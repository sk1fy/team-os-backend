//go:build integration

package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// Called after the normal dynamic mode/restore scenario. Only the disposable
// testcontainers PostgreSQL is eligible; no connection to an external DB is accepted.
func verifyTaskControlReleaseBackupRestore(t *testing.T, ctx context.Context, pool *pgxpool.Pool, svc *Service, actor Actor, binding uuid.UUID, original TaskControlPublicationV2) {
	t.Helper()
	if pool.Config().ConnConfig.Database != "company" {
		t.Fatal("release drill requires the isolated company test database")
	}
	ids, err := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", "label=org.testcontainers=true").Output()
	if err != nil {
		t.Fatal(err)
	}
	container := ""
	for _, id := range strings.Fields(string(ids)) {
		payload, e := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .NetworkSettings.Ports}}", id).Output()
		if e != nil {
			t.Fatal(e)
		}
		var ports map[string][]struct{ HostPort string }
		if e = json.Unmarshal(payload, &ports); e != nil {
			t.Fatal(e)
		}
		for _, endpoint := range ports["5432/tcp"] {
			if endpoint.HostPort == strconv.Itoa(int(pool.Config().ConnConfig.Port)) {
				if container != "" && container != id {
					t.Fatal("ambiguous test container")
				}
				container = id
			}
		}
	}
	if container == "" {
		t.Fatal("could not identify disposable testcontainers PostgreSQL")
	}
	command := func(input []byte, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, "docker", append([]string{"exec", "-i", container}, args...)...)
		cmd.Stdin = bytes.NewReader(input)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, e := cmd.Output()
		if e != nil {
			t.Fatalf("backup/restore command failed: %v: %s", e, stderr.String())
		}
		return out
	}
	snapshot := func(p *pgxpool.Pool) map[string]string {
		t.Helper()
		rows, e := p.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename LIKE 'task\_control\_%' ORDER BY tablename`)
		if e != nil {
			t.Fatal(e)
		}
		tables := []string{}
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				t.Fatal(e)
			}
			tables = append(tables, name)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
		if len(tables) < 10 {
			t.Fatal("incomplete task-control schema", tables)
		}
		hashes := map[string]string{}
		for _, table := range tables {
			var payload string
			if e = p.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM `+pgx.Identifier{table}.Sanitize()+` r`).Scan(&payload); e != nil {
				t.Fatal(e)
			}
			digest := sha256.Sum256([]byte(payload))
			hashes[table] = hex.EncodeToString(digest[:])
		}
		return hashes
	}
	before := snapshot(pool)
	_, file, _, _ := runtime.Caller(0)
	down, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../migrations/000040_task_control_runtime_diagnostics.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, downgradeErr := tx.Exec(ctx, string(down))
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if downgradeErr == nil || !strings.Contains(downgradeErr.Error(), "Retained runtime evidence") {
		t.Fatal("downgrade with retained ACK evidence was not rejected", downgradeErr)
	}
	backup := command(nil, "pg_dump", "-U", "company", "-d", "company", "--format=custom", "--no-owner", "--no-privileges")
	backupHash := sha256.Sum256(backup)
	command(nil, "createdb", "-U", "company", "r10_restore")
	command(backup, "pg_restore", "-U", "company", "-d", "r10_restore", "--exit-on-error", "--no-owner", "--no-privileges")
	config := pool.Config().Copy()
	config.ConnConfig.Database = "r10_restore"
	restoredPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredPool.Close()
	after := snapshot(restoredPool)
	if !bytes.Equal(mustR05JSON(t, before), mustR05JSON(t, after)) {
		t.Fatal("task-control data changed after pg_dump/pg_restore")
	}
	// Reconstruct the application with a new pool, as after a service restart.
	restored := &Service{pool: restoredPool, now: svc.now, taskControlCore: svc.taskControlCore, taskControlKeys: svc.taskControlKeys}
	draft, err := restored.GetTaskControlDraftV2(ctx, actor, binding)
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.New(restoredPool).GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: actor.CompanyID, ID: binding})
	if err != nil {
		t.Fatal(err)
	}
	var current TaskControlPublicationV2
	var payload []byte
	if err = restoredPool.QueryRow(ctx, `SELECT payload FROM task_control_publications WHERE company_id=$1 AND id=$2`, actor.CompanyID, c.PublicationID.UUID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(payload, &current); err != nil {
		t.Fatal(err)
	}
	// Publish a different behavior after restart, then restore the pre-change
	// intent. Copying the currently active publication would not prove rollback.
	var changed TaskControlDraftV2
	if err = json.Unmarshal(mustR05JSON(t, draft), &changed); err != nil {
		t.Fatal(err)
	}
	if len(changed.IntentRules) == 0 || changed.IntentRules[0].Dynamic == nil {
		t.Fatal("dynamic rollback fixture missing")
	}
	changed.IntentRules[0].Dynamic.CheckResponsible = !changed.IntentRules[0].Dynamic.CheckResponsible
	changed.IntentRules[0].Dynamic.Name = "Проверка изменения перед откатом R10"
	updated, err := restored.UpdateTaskControlDraftV2(ctx, actor, binding, TaskControlDraftInputV2{draft.Revision, c.BindingRevision, c.ModeEpoch, changed.IntentRules})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := restored.PublishTaskControlDraftV2(ctx, actor, binding, TaskControlPublishInputV2{RequestID: uuid.New(), ExpectedBindingRevision: c.BindingRevision, ExpectedModeEpoch: c.ModeEpoch, ExpectedDraftRevision: updated.Revision, ExpectedChecksum: updated.Checksum})
	if err != nil || replacement.IntentChecksum == current.IntentChecksum {
		t.Fatal("replacement did not change intent", err)
	}
	rolled, err := restored.RollbackTaskControlV2(ctx, actor, binding, TaskControlRollbackInputV2{RequestID: uuid.New(), SourcePublicationID: current.ID, ExpectedPublishedVersion: replacement.Version, ExpectedBindingRevision: c.BindingRevision, ExpectedModeEpoch: c.ModeEpoch})
	if err != nil || rolled.ID == current.ID || rolled.IntentChecksum != current.IntentChecksum || rolled.Version != replacement.Version+1 || rolled.RestoredFromID == nil || *rolled.RestoredFromID != current.ID {
		t.Fatal("explicit rollback after restore", rolled, err)
	}
	retained, err := restored.GetTaskControlDraftV2(ctx, actor, binding)
	if err != nil || updated.Revision != retained.Revision || updated.Checksum != retained.Checksum {
		t.Fatal("rollback changed draft", err)
	}
	var count int
	if err = restoredPool.QueryRow(ctx, `SELECT count(*) FROM task_control_publications WHERE company_id=$1 AND id=$2`, actor.CompanyID, original.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("original intent lost", count, err)
	}
	t.Logf("release drill: pg_dump/pg_restore SHA-256=%x, %d task-control tables unchanged; restart and explicit rollback preserve draft/history", backupHash, len(before))
}

func taskControlReleaseDrillEnabled() bool { return os.Getenv("TASK_CONTROL_RELEASE_DRILL") == "1" }
