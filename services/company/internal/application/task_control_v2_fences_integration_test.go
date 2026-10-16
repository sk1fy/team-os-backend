//go:build integration

package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestTaskControlDynamicLateWorkerCannotReplaceNewPublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, owner, binding, install, integration := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, owner, []accessTestUser{{owner, "owner", "active"}})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO company_integrations(id,company_id,provider,external_account_id,status) VALUES($1,$2,'rakurs','123','active')`, uuid.New(), company)
	exec(`INSERT INTO task_control_connections(company_id,id,installation_id,integration_id,account_id,generation,intent_id,linked_by,crm_admin_id,mode,runtime_schema_version) VALUES($1,$2,$3,$4,'123',1,$5,$6,'7','managed',2)`, company, binding, install, integration, uuid.New(), owner)
	q := db.New(pool)
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: company, ID: binding})
	if e != nil {
		t.Fatal(e)
	}
	oldID, newID, snapshotID := uuid.New(), uuid.New(), uuid.New()
	rules := []tc.IntentRule{{Kind: "dynamic", Dynamic: &tc.RuleV2{SchemaVersion: 2, RuleUID: uuid.New(), Name: "Published", Audience: tc.AudienceSelection{Kind: "allLinked"}, Leads: tc.LeadSelection{Kind: "allOpen"}, Customers: tc.CustomerSelection{Kind: "disabled"}, Enabled: true}}}
	sum, e := tc.IntentChecksum(rules)
	if e != nil {
		t.Fatal(e)
	}
	for i, id := range []uuid.UUID{oldID, newID} {
		pub := TaskControlPublicationV2{ID: id, Version: int64(i + 1), SchemaVersion: 2, ScopeAtPublication: taskControlScope(c), DraftRevision: 1, IntentRules: rules, IntentChecksum: sum, CreatedAt: time.Now().UTC(), CreatedBy: owner, InitialSnapshotID: snapshotID, RuntimeRevision: 2}
		raw, _ := json.Marshal(pub)
		if _, e = q.CreateTaskControlPublication(ctx, db.CreateTaskControlPublicationParams{CompanyID: company, BindingID: binding, ID: id, RequestID: uuid.New(), BodyHash: sum, Payload: raw}); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE task_control_connections SET publication_id=$3 WHERE company_id=$1 AND id=$2`, company, binding, oldID)
	// A prior committed snapshot can only be advanced by the matching publication.
	exec(`INSERT INTO task_control_v2_state(company_id,binding_id,current_snapshot_id,runtime_revision,verified_source_fresh_until) VALUES($1,$2,$3,2,now()+interval '5 minutes')`, company, binding, snapshotID)
	exec(`INSERT INTO task_control_v2_jobs(company_id,binding_id,mode_epoch,publication_id) VALUES($1,$2,1,$3)`, company, binding, oldID)
	lock, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = lock.Rollback(ctx) }()
	if e = db.New(lock).LockTaskControlAccount(ctx, "123"); e != nil {
		t.Fatal(e)
	}
	svc := &Service{pool: pool, now: time.Now}
	done := make(chan error, 1)
	go func() {
		// The NOWAIT probe below briefly locks the row itself. SKIP LOCKED can
		// legitimately return no job during that interval, so mirror the real
		// worker poll instead of treating this observer race as a fence failure.
		for {
			worked, err := svc.resolveTaskControlV2Job(ctx)
			if worked || err != nil {
				done <- err
				return
			}
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	// Wait until the worker owns its old job and is blocked on the account lock.
	claimed := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		probe, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = probe.Exec(ctx, `SELECT 1 FROM task_control_v2_jobs WHERE company_id=$1 AND binding_id=$2 FOR UPDATE NOWAIT`, company, binding)
		_ = probe.Rollback(ctx)
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "55P03" {
			claimed = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !claimed {
		t.Fatal("worker did not reach account fence")
	}
	// This is the pointer commit performed by publish under the same account lock.
	if _, e = lock.Exec(ctx, `UPDATE task_control_connections SET publication_id=$3 WHERE company_id=$1 AND id=$2`, company, binding, newID); e != nil {
		t.Fatal(e)
	}
	if e = lock.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	state, e := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: company, BindingID: binding})
	if e != nil || state.RuntimeRevision != 2 || state.CurrentSnapshotID.UUID != snapshotID {
		t.Fatal("late worker replaced publication snapshot", state, e)
	}
	current, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: company, ID: binding})
	if e != nil || current.PublicationID.UUID != newID {
		t.Fatal("late worker changed active publication", current, e)
	}
	// Actor authorization is checked before exposing intent, even for a valid binding UUID.
	foreign := Actor{CompanyID: uuid.New(), UserID: owner}
	if _, e = svc.GetTaskControlDraftV2(ctx, foreign, binding); e == nil {
		t.Fatal("cross-tenant intent disclosed")
	}
	// A late job from another epoch also cannot promote any runtime revision.
	exec(`INSERT INTO task_control_v2_jobs(company_id,binding_id,mode_epoch,publication_id) VALUES($1,$2,999,$3)`, company, binding, newID)
	if _, e = svc.resolveTaskControlV2Job(ctx); e != nil {
		t.Fatal(e)
	}
	state, e = q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: company, BindingID: binding})
	if e != nil || state.RuntimeRevision != 2 {
		t.Fatal("foreign epoch promoted", state, e)
	}
}
