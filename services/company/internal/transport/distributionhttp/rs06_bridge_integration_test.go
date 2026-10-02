//go:build integration

package distributionhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Paired with Core TestDistributionTeamBridgeServer. Both databases, signed
// transports, frozen commands, callbacks and operation results are real. The
// opposite test controls amoCRM alone; no synthetic allow response is supplied.
func TestRS06ActualCoreTeamSignedBridge(t *testing.T) {
	dir := os.Getenv("DISTRIBUTION_BRIDGE_DIR")
	if dir == "" {
		t.Skip("opt-in Core/Team PostgreSQL bridge")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	var core struct {
		URL         string           `json:"url"`
		Certificate string           `json:"certificatePEM"`
		Scope       corebridge.Scope `json:"scope"`
		Employee    uuid.UUID        `json:"employeeId"`
		Employee3   uuid.UUID        `json:"employee3Id"`
		Secret      string           `json:"secret"`
		KeyID       string           `json:"keyId"`
	}
	rs06ReadBridge(t, ctx, dir, "core.json", &core)
	pool := rs06BridgePool(t, ctx)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	company := core.Scope.CompanyID
	exec(`INSERT INTO companies(id,name) VALUES($1,'RS06 signed bridge')`, company)
	for i, id := range []uuid.UUID{core.Employee, core.Employee3} {
		role := "employee"
		if i == 0 {
			role = "owner"
		}
		exec(`INSERT INTO users(id,company_id,email,first_name,role,status) VALUES($1,$2,$3,'Bridge',$4,'active')`, id, company, fmt.Sprintf("rs06-%d@example.invalid", i), role)
		exec(`INSERT INTO employee_section_access(company_id,user_id,section) VALUES($1,$2,'distribution') ON CONFLICT DO NOTHING`, company, id)
		exec(`INSERT INTO user_schedules(company_id,user_id,template) VALUES($1,$2,'{"type":"week","days":[0,1,2,3,4,5,6],"start":"00:00","end":"23:59"}')`, company, id)
	}
	exec(`INSERT INTO distribution_bindings(id,company_id,installation_id,integration_id,account_id,state,intent_id,initiated_by,initiator_snapshot,expires_at,mapping_revision,mapping_ack_revision) VALUES($1,$2,$3,$4,$5,'active',$6,$7,$7,clock_timestamp()+interval '15 minutes',1,1)`, core.Scope.BindingID, company, core.Scope.InstallationID, core.Scope.IntegrationID, core.Scope.AccountID, uuid.New(), core.Employee)
	exec(`INSERT INTO distribution_binding_versions(company_id,binding_id,revision,installation_id,integration_id,account_id,state) VALUES($1,$2,1,$3,$4,$5,'active')`, company, core.Scope.BindingID, core.Scope.InstallationID, core.Scope.IntegrationID, core.Scope.AccountID)
	for i, id := range []uuid.UUID{core.Employee, core.Employee3} {
		exec(`INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,$5,'verified',clock_timestamp())`, uuid.New(), company, core.Scope.BindingID, id, strconv.Itoa(i+2))
	}
	for _, capability := range []string{"decision-validation", "event-delivery", "result-delivery"} {
		exec(`INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES($1,$2,$3,$4)`, core.KeyID, company, core.Scope.InstallationID, capability)
	}
	client, err := corebridge.New(core.URL, core.KeyID, core.Secret)
	if err != nil {
		t.Fatal(err)
	}
	newService := func() *application.Service {
		t.Helper()
		svc, err := application.NewService(pool, nil, application.WithDistributionCore(client), application.WithDistributionDeliveryCore(client))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := newService()
	team := httptest.NewUnstartedServer(&Handler{Keys: map[string]string{core.KeyID: core.Secret}, Store: db.New(pool), Service: svc})
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = team.Listener.Close()
	team.Listener = listener
	team.StartTLS()
	defer team.Close()
	teamURL := fmt.Sprintf("https://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	rs06WriteBridge(t, dir, "team.json", map[string]any{"url": teamURL, "certificatePEM": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: team.Certificate().Raw}))})
	var ready map[string]any
	rs06ReadBridge(t, ctx, dir, "connected.json", &ready)
	sequence := 0
	control := func(in any) rs06BridgeReply {
		t.Helper()
		sequence++
		rs06WriteBridge(t, dir, fmt.Sprintf("ctl-%d.json", sequence), in)
		var reply rs06BridgeReply
		rs06ReadBridge(t, ctx, dir, fmt.Sprintf("reply-%d.json", sequence), &reply)
		return reply
	}
	defer func() {
		if ctx.Err() == nil {
			control(map[string]any{"action": "stop"})
		}
	}()
	owner := application.Actor{CompanyID: company, UserID: core.Employee, Role: "owner"}
	if _, err := svc.SaveDistributionTimezone(ctx, owner, "UTC"); err != nil {
		t.Fatal(err)
	}
	group := uuid.New()
	exec(`INSERT INTO distribution_groups(id,company_id,name,member_ids) VALUES($1,$2,'RS06 actual bridge',$3)`, group, company, []uuid.UUID{core.Employee, core.Employee3})
	rule, err := svc.CreateDistributionRuntimeRule(ctx, owner, db.CreateDistributionRuleParams{CompanyID: company, BindingID: core.Scope.BindingID, BindingRevision: 1, AccountID: core.Scope.AccountID, PipelineID: "20", StatusID: "30", GroupID: group, Active: true, KeepCurrent: true})
	if err != nil {
		t.Fatal(err)
	}
	// No timestamp spoofing: the real entry is observed after actual activation.
	eventRevision := int64(1000)
	created := func(id, ownerID int64, mode string) {
		t.Helper()
		control(map[string]any{"action": "crm", "leadId": id, "pipelineId": 20, "statusId": 30, "responsibleUserId": ownerID, "updatedAt": 100, "mode": mode})
		eventRevision++
		now := time.Now().UTC()
		p, st := "20", "30"
		event := application.DistributionEventEnvelope{SchemaVersion: 1, MessageID: uuid.New(), Scope: core.Scope, EventID: uuid.New(), SourceOccurredAt: &now, ReceivedAt: now, EmittedAt: now, CorrelationID: uuid.New(), Event: application.DistributionCRMEvent{Kind: "lead.created", LeadID: strconv.FormatInt(id, 10), ObservationRevision: eventRevision, SourceEvidence: &application.DistributionSourceEvidence{PipelineID: &p, StatusID: &st}}}
		raw, _ := json.Marshal(event)
		request := httptest.NewRequestWithContext(ctx, "POST", teamURL+"/internal/v1/distribution/events", bytes.NewReader(raw))
		corebridge.Sign(request, core.KeyID, core.Secret, core.Scope, raw, time.Now())
		out := httptest.NewRecorder()
		team.Config.Handler.ServeHTTP(out, request)
		if out.Code != 202 {
			t.Fatalf("signed event ACK %d %s", out.Code, out.Body.String())
		}
		if found, err := svc.ProcessDistributionDelivery(ctx); !found || err != nil {
			t.Fatalf("real event processing %v %v", found, err)
		}
	}
	exited := func(id, ownerID int64) {
		t.Helper()
		control(map[string]any{"action": "crm", "leadId": id, "pipelineId": 20, "statusId": 31, "responsibleUserId": ownerID, "updatedAt": 102})
		eventRevision++
		now := time.Now().UTC()
		p, old, st := "20", "30", "31"
		event := application.DistributionEventEnvelope{SchemaVersion: 1, MessageID: uuid.New(), Scope: core.Scope, EventID: uuid.New(), SourceOccurredAt: &now, ReceivedAt: now, EmittedAt: now, CorrelationID: uuid.New(), Event: application.DistributionCRMEvent{Kind: "lead.status_changed", LeadID: strconv.FormatInt(id, 10), ObservationRevision: eventRevision, SourceEvidence: &application.DistributionSourceEvidence{PipelineID: &p, StatusID: &st, OldPipelineID: &p, OldStatusID: &old}}}
		raw, _ := json.Marshal(event)
		request := httptest.NewRequestWithContext(ctx, "POST", teamURL+"/internal/v1/distribution/events", bytes.NewReader(raw))
		corebridge.Sign(request, core.KeyID, core.Secret, core.Scope, raw, time.Now())
		out := httptest.NewRecorder()
		team.Config.Handler.ServeHTTP(out, request)
		if out.Code != 202 {
			t.Fatalf("signed exit ACK %d %s", out.Code, out.Body.String())
		}
		if found, err := svc.ProcessDistributionDelivery(ctx); !found || err != nil {
			t.Fatalf("signed exit processing %v %v", found, err)
		}
	}
	process := func() {
		t.Helper()
		if _, err := svc.ProcessDistributionQueue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	due := func() { exec(`UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE NOT settled`) }
	settle := func() { t.Helper(); due(); svc = newService(); process() }
	state := func(id int64, want string) {
		t.Helper()
		var got string
		if err := pool.QueryRow(ctx, `SELECT state FROM distribution_queue WHERE lead_id=$1`, strconv.FormatInt(id, 10)).Scan(&got); err != nil || got != want {
			t.Fatalf("lead%d queue %s want%s: %v", id, got, want, err)
		}
	}
	cursor := func(want int32) {
		t.Helper()
		var got int32
		if err := pool.QueryRow(ctx, `SELECT cursor_next FROM distribution_group_claims WHERE company_id=$1 AND group_id=$2`, company, group).Scan(&got); err != nil || got != want {
			t.Fatalf("cursor %d want%d %v", got, want, err)
		}
	}
	tick := func(wantState string, wantCalls int) {
		t.Helper()
		reply := control(map[string]any{"action": "tick"})
		if reply.Error != "" || reply.Operation == nil || reply.Operation.State != wantState || reply.PatchCalls != wantCalls {
			t.Fatalf("actual Core tick %+v want%s/calls%d", reply, wantState, wantCalls)
		}
	}
	t.Run("actual_assignment_and_durable_restart", func(t *testing.T) {
		created(10, 1, "")
		process()
		tick("succeeded", 1)
		settle()
		state(10, "confirmed")
		cursor(1)
		settle()
		cursor(1)
	})
	t.Run("keep_has_no_patch_and_no_turn", func(t *testing.T) {
		created(11, 3, "")
		process()
		tick("no_change", 1)
		settle()
		state(11, "kept")
		cursor(1)
	})
	exec(`UPDATE distribution_rules SET keep_current=false,revision=revision+1 WHERE id=$1`, rule.ID)
	t.Run("same_owner_assign_confirms_exactly_one_turn", func(t *testing.T) {
		created(12, 3, "")
		process()
		tick("no_change", 1)
		settle()
		state(12, "confirmed")
		cursor(0)
	})
	t.Run("concurrent_events_keep_one_group_turn", func(t *testing.T) {
		created(13, 1, "")
		created(14, 1, "")
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := svc.ProcessDistributionQueue(ctx); errs <- err }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var commands int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM distribution_queue WHERE lead_id IN('13','14') AND operation_id IS NOT NULL`).Scan(&commands); err != nil || commands != 1 {
			t.Fatal("two simultaneous business commands", commands, err)
		}
		tick("succeeded", 2)
		settle()
		state(13, "confirmed")
		cursor(1)
		due()
		process()
		tick("succeeded", 3)
		settle()
		state(14, "confirmed")
		cursor(0)
	})
	t.Run("actual_grant_expires_while_core_guard_waits", func(t *testing.T) {
		created(15, 1, "")
		process()
		var op uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT operation_id FROM distribution_queue WHERE lead_id='15'`).Scan(&op); err != nil {
			t.Fatal(err)
		}
		control(map[string]any{"action": "guard_hold", "operationId": op, "milliseconds": 5500})
		reply := control(map[string]any{"action": "tick"})
		if reply.Error != "decision expired or unavailable" || reply.PatchCalls != 3 {
			t.Fatalf("expired real grant dispatched %+v", reply)
		}
		operation, err := client.Operation(ctx, core.Scope, op)
		if err != nil || operation.State != "queued" || operation.ResolutionEvidence.GuardReleasable {
			t.Fatal("expired grant advanced result", operation, err)
		}
		control(map[string]any{"action": "job_ready"})
		tick("succeeded", 4)
		settle()
		state(15, "confirmed")
		cursor(1)
	})
	t.Run("crm_recipient_deactivated_after_decision_never_dispatches", func(t *testing.T) {
		created(17, 1, "")
		process()
		control(map[string]any{"action": "recipient", "userId": 3, "active": false})
		tick("rejected", 4)
		cursor(1)
		control(map[string]any{"action": "recipient", "userId": 3, "active": true})
		exited(17, 1)
		settle()
		cursor(1)
		var claims int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM distribution_lead_claims WHERE lead_id='17'`).Scan(&claims); err != nil || claims != 0 {
			t.Fatal("safe recipient rejection retained claim", claims, err)
		}
	})
	t.Run("manual_crm_owner_change_before_patch_is_not_overwritten", func(t *testing.T) {
		created(18, 1, "")
		process()
		control(map[string]any{"action": "crm", "leadId": 18, "pipelineId": 20, "statusId": 30, "responsibleUserId": 2, "updatedAt": 101})
		tick("conflict", 4)
		cursor(1)
		exited(18, 2)
		settle()
		cursor(1)
		var claims int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM distribution_lead_claims WHERE lead_id='18'`).Scan(&claims); err != nil || claims != 0 {
			t.Fatal("safe source conflict retained claim", claims, err)
		}
	})
	t.Run("durable_ack_reconciles_without_second_patch", func(t *testing.T) {
		created(16, 1, "observe_failure")
		process()
		tick("confirming", 5)
		control(map[string]any{"action": "crm", "leadId": 16, "pipelineId": 20, "statusId": 30, "responsibleUserId": 3, "updatedAt": 101, "mode": ""})
		settle()
		state(16, "confirmed")
		cursor(0)
		if reply := control(map[string]any{"action": "crm", "leadId": 16, "pipelineId": 20, "statusId": 30, "responsibleUserId": 3, "updatedAt": 101}); reply.PatchCalls != 5 {
			t.Fatal("reconcile repeatedPATCH", reply)
		}
	})
	t.Run("unknown_outcome_holds_group_and_lead_after_restart", func(t *testing.T) {
		created(20, 1, "timeout_applied")
		process()
		tick("outcome_unknown", 6)
		settle()
		state(20, "uncertain")
		cursor(0)
		created(21, 1, "")
		due()
		process()
		var pending, leadClaims, groupClaims int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM distribution_queue WHERE lead_id='21' AND operation_id IS NOT NULL),(SELECT count(*) FROM distribution_lead_claims WHERE account_id='123' AND lead_id='20'),(SELECT count(*) FROM distribution_group_claims WHERE company_id=$1 AND queue_id IS NOT NULL)`, company).Scan(&pending, &leadClaims, &groupClaims); err != nil || pending != 0 || leadClaims != 1 || groupClaims != 1 {
			t.Fatal("unknown guard bypass", pending, leadClaims, groupClaims, err)
		}
		if reply := control(map[string]any{"action": "tick"}); reply.PatchCalls != 6 {
			t.Fatal("unknown outcome repeatedPATCH", reply)
		}
	})
}

type rs06BridgeReply struct {
	Error      string                `json:"error"`
	Operation  *corebridge.Operation `json:"operation"`
	PatchCalls int                   `json:"patchCalls"`
	NoJob      bool                  `json:"noJob"`
}

func rs06WriteBridge(t *testing.T, dir, name string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path+".tmp", raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}
func rs06ReadBridge(t *testing.T, ctx context.Context, dir, name string, value any) {
	t.Helper()
	for {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			if err := json.Unmarshal(raw, value); err != nil {
				t.Fatal(err)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("paired bridge timed out", name, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func rs06BridgePool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	sources, err := filepath.Glob(filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	scripts := []string{}
	for _, source := range sources {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(temporary, filepath.Base(source))
		if err := os.WriteFile(target, append(append([]byte("BEGIN;\n"), raw...), []byte("\nCOMMIT;\n")...), 0600); err != nil {
			t.Fatal(err)
		}
		scripts = append(scripts, target)
	}
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("company"), postgres.WithUsername("company"), postgres.WithPassword("company"), postgres.WithInitScripts(scripts...), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Error(err)
		}
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
