//go:build integration

package distributionhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
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
	for _, capability := range []string{"decision-validation", "event-delivery", "result-delivery", "widget-access", "widget-runtime"} {
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
	var handler atomic.Pointer[Handler]
	handler.Store(&Handler{Keys: map[string]string{core.KeyID: core.Secret}, Store: db.New(pool), Service: svc})
	team := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.Load().ServeHTTP(w, r) }))
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "0.0.0.0:0")
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
	t.Run("rs08_actual_sdk_widget_shared_rules", func(t *testing.T) {
		widgetGroup, widgetRule := uuid.New(), uuid.New()
		exec(`INSERT INTO distribution_groups(id,company_id,name,member_ids) VALUES($1,$2,'RS08 widget shared',$3)`, widgetGroup, company, []uuid.UUID{core.Employee})
		exec(`INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id) VALUES($1,$2,$3,1,$4,'20','31',$5)`, widgetRule, company, core.Scope.BindingID, core.Scope.AccountID, widgetGroup)
		callWidget := func(method, path string, body any, want int) []byte {
			t.Helper()
			time.Sleep(220 * time.Millisecond)
			token := control(map[string]any{"action": "widget_token"}).WidgetToken
			if token == "" {
				t.Fatal("no synthetic SDK token")
			}
			raw, _ := json.Marshal(body)
			req, e := http.NewRequestWithContext(ctx, method, core.URL+path, bytes.NewReader(raw))
			if e != nil {
				t.Fatal(e)
			}
			req.Header.Set("Origin", "https://test.amocrm.ru")
			req.Header.Set("X-Auth-Token", token)
			req.Header.Set("Content-Type", "application/json")
			response, e := (&http.Client{Timeout: 3 * time.Second}).Do(req)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = response.Body.Close() }()
			result, e := io.ReadAll(response.Body)
			if e != nil || response.StatusCode != want {
				t.Fatalf("widget %s status%d want%d body%s err%v", path, response.StatusCode, want, result, e)
			}
			return result
		}
		base := "/api/v1/widget/distribution/"
		bootstrap := callWidget("GET", base+"bootstrap", nil, 200)
		if !bytes.Contains(bootstrap, []byte(`"canManage":true`)) {
			t.Fatal("both server roles not verified", string(bootstrap))
		}
		read := callWidget("POST", base+"runtime", map[string]any{"kind": "rules"}, 200)
		if !bytes.Contains(read, []byte(widgetRule.String())) {
			t.Fatal("shared rule absent")
		}
		write := map[string]any{"kind": "rule", "id": widgetRule, "write": true, "requestId": uuid.New(), "payload": map[string]any{"expectedRevision": 1, "active": false, "keepCurrentResponsible": false}}
		callWidget("POST", base+"runtime", write, 200)
		callWidget("POST", base+"runtime", write, 200)
		var revision int64
		if e := pool.QueryRow(ctx, "SELECT revision FROM distribution_rules WHERE id=$1", widgetRule).Scan(&revision); e != nil || revision != 2 {
			t.Fatalf("single shared revision %d %v", revision, e)
		}
		callWidget("POST", base+"runtime", map[string]any{"kind": "rules", "companyId": uuid.New()}, 400)
		exec("UPDATE users SET role='employee' WHERE id=$1", core.Employee)
		callWidget("POST", base+"runtime", write, 403)
		exec("UPDATE users SET role='owner' WHERE id=$1", core.Employee)
	})
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
		request, err := http.NewRequestWithContext(ctx, "POST", teamURL+"/internal/v1/distribution/events", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		corebridge.Sign(request, core.KeyID, core.Secret, core.Scope, raw, time.Now())
		response, err := team.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 202 {
			t.Fatalf("signed event ACK %d %s", response.StatusCode, body)
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
		request, err := http.NewRequestWithContext(ctx, "POST", teamURL+"/internal/v1/distribution/events", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		corebridge.Sign(request, core.KeyID, core.Secret, core.Scope, raw, time.Now())
		response, err := team.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 202 {
			t.Fatalf("signed exit ACK %d %s", response.StatusCode, body)
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
	tick := func(t *testing.T, wantState string, wantCalls int) {
		t.Helper()
		reply := control(map[string]any{"action": "tick"})
		if reply.Error != "" || reply.Operation == nil || reply.Operation.State != wantState || reply.PatchCalls != wantCalls {
			t.Fatalf("actual Core tick %+v want%s/calls%d", reply, wantState, wantCalls)
		}
	}
	t.Run("expired_missing_admission_is_fenced_before_claim_release", func(t *testing.T) {
		before := control(map[string]any{"action": "stats"})
		now := time.Now().UTC()
		entry, queue, key := uuid.New(), uuid.New(), uuid.New()
		a := corebridge.Assignment{
			SchemaVersion: 1, MessageID: uuid.New(), Scope: core.Scope, EventID: uuid.New(),
			SourceOccurredAt: now.Add(-2 * time.Minute), ReceivedAt: now.Add(-2 * time.Minute), EmittedAt: now.Add(-2 * time.Minute),
			CorrelationID: uuid.New(), CausationID: uuid.New(),
			Command: corebridge.AssignmentCommand{
				OperationID: uuid.New(), EpisodeID: entry, DecisionID: uuid.New(), RuleID: rule.ID, GroupID: group,
				RuleRevision: rule.Revision, AvailabilityRevision: 1, ClaimRevision: 1, DecisionKind: "assign", TargetResponsibleUserID: "2",
				ExpectedSnapshot: corebridge.LeadSnapshot{LeadID: "9", PipelineID: "20", StatusID: "30", ResponsibleUserID: "1", ObservedAt: now.Add(-2 * time.Minute)},
				Actor:            corebridge.AssignmentActor{Kind: "system"}, ValidUntil: now.Add(-time.Minute),
			},
		}
		raw, e := json.Marshal(a)
		if e != nil {
			t.Fatal(e)
		}
		// Simulate a process restored with a committed frozen command whose POST
		// never reached Core. The active episode must be recalculated after proof.
		exec(`INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES($1,'9',$2,$3,1,$4)`, core.Scope.AccountID, company, core.Scope.BindingID, entry)
		exec(`INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at) VALUES($1,$2,$3,'9',$4,1,1,'20','30',$5,'created_in_stage','checking',$6,$6)`, entry, company, core.Scope.AccountID, core.Scope.BindingID, a.EventID, a.ReceivedAt)
		exec(`INSERT INTO distribution_queue(id,company_id,entry_id,rule_id,group_id,account_id,lead_id,state,operation_id,decision_id,command,idempotency_key,cancel_key,reconcile_key,availability_hash,planned_employee_id,selection_order) VALUES($1,$2,$3,$4,$5,$6,'9','uncertain',$7,$8,$9,$10,$11,$12,decode(repeat('00',32),'hex'),$13,$14)`, queue, company, entry, rule.ID, group, core.Scope.AccountID, a.Command.OperationID, a.Command.DecisionID, raw, key, uuid.New(), uuid.New(), core.Employee, []uuid.UUID{core.Employee, core.Employee3})
		exec(`INSERT INTO distribution_group_claims(company_id,group_id,queue_id) VALUES($1,$2,$3)`, company, group, queue)
		exec(`INSERT INTO distribution_lead_claims(account_id,lead_id,queue_id) VALUES($1,'9',$2)`, core.Scope.AccountID, queue)
		process()
		state(9, "waiting")
		recovered, e := db.New(pool).LockDistributionQueue(ctx, queue)
		if e != nil || recovered.Reason != "decision_recalculation" || recovered.OperationID.Valid || recovered.Command != nil || recovered.Settled {
			t.Fatal("terminal expiry did not restore an active episode", recovered, e)
		}
		cursor(0)
		var held int
		if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM distribution_lead_claims WHERE queue_id=$1)+(SELECT count(*) FROM distribution_group_claims WHERE queue_id=$1)`, queue).Scan(&held); e != nil || held != 0 {
			t.Fatal("terminal no-attempt retained claims", held, e)
		}
		op, e := client.Operation(ctx, core.Scope, a.Command.OperationID)
		if e != nil || op.State != "rejected" || op.ExternalEffectState != "no_attempt" || !op.ResolutionEvidence.GuardReleasable {
			t.Fatal("missing remote expiry fence", op, e)
		}
		late, e := client.Assign(ctx, a, key)
		if e != nil || late.OperationID != op.OperationID || late.State != "rejected" {
			t.Fatal("late admission escaped expiry fence", late, e)
		}
		replay, e := client.ExpireAssignment(ctx, a, key)
		if e != nil || replay.OperationID != op.OperationID || replay.ResultVersion != op.ResultVersion {
			t.Fatal("expiry replay changed durable outcome", replay, e)
		}
		after := control(map[string]any{"action": "stats"})
		// Core keeps the required job identity as a cancelled audit row.
		if after.Operations != before.Operations+1 || after.Guards != before.Guards || after.AssignmentJobs != before.AssignmentJobs+1 || after.PatchCalls != before.PatchCalls {
			t.Fatal("expiry or late replay changed durable identities or sent PATCH", before, after)
		}
		// Retire the now-undispatched fixture waiting row so the following cases
		// can use the same group. This cannot discard an unresolved operation.
		exec(`UPDATE distribution_queue SET state='cancelled',reason='user_cancelled',settled=true,cancel_requested=true WHERE id=$1 AND operation_id IS NULL`, queue)
	})
	t.Run("actual_assignment_and_durable_restart", func(t *testing.T) {
		created(10, 1, "")
		process()
		tick(t, "succeeded", 1)
		settle()
		state(10, "confirmed")
		cursor(1)
		settle()
		cursor(1)
	})
	t.Run("keep_has_no_patch_and_no_turn", func(t *testing.T) {
		created(11, 3, "")
		process()
		tick(t, "no_change", 1)
		settle()
		state(11, "kept")
		cursor(1)
	})
	exec(`UPDATE distribution_rules SET keep_current=false,revision=revision+1 WHERE id=$1`, rule.ID)
	t.Run("same_owner_assign_confirms_exactly_one_turn", func(t *testing.T) {
		created(12, 3, "")
		process()
		tick(t, "no_change", 1)
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
		tick(t, "succeeded", 2)
		settle()
		state(13, "confirmed")
		cursor(1)
		due()
		process()
		tick(t, "succeeded", 3)
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
		tick(t, "succeeded", 4)
		settle()
		state(15, "confirmed")
		cursor(1)
	})
	t.Run("crm_recipient_deactivated_after_decision_never_dispatches", func(t *testing.T) {
		created(17, 1, "")
		process()
		control(map[string]any{"action": "recipient", "userId": 3, "active": false})
		tick(t, "rejected", 4)
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
		tick(t, "conflict", 4)
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
		tick(t, "confirming", 5)
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
		tick(t, "outcome_unknown", 6)
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

	t.Run("rs10_observation_scope_has_zero_effects_and_fresh_enable_boundary", func(t *testing.T) {
		exec(`UPDATE distribution_queue SET next_attempt_at=clock_timestamp()+interval '1 hour' WHERE group_id=$1`, group)
		observeGroup, e := svc.CreateDistributionGroup(ctx, owner, application.CreateDistributionGroupInput{Name: "RS10 read-only observation", MemberIDs: []uuid.UUID{core.Employee}})
		if e != nil {
			t.Fatal(e)
		}
		observedRule, e := svc.CreateDistributionRuntimeRule(ctx, owner, db.CreateDistributionRuleParams{CompanyID: company, BindingID: core.Scope.BindingID, BindingRevision: 1, AccountID: core.Scope.AccountID, PipelineID: "20", StatusID: "31", GroupID: observeGroup.ID, Active: true, KeepCurrent: true, ExecutionMode: "observe"})
		if e != nil {
			t.Fatal(e)
		}
		control(map[string]any{"action": "pause"})
		baseline := control(map[string]any{"action": "stats"})
		event31 := func(id int64) application.DistributionEventEnvelope {
			t.Helper()
			control(map[string]any{"action": "crm", "leadId": id, "pipelineId": 20, "statusId": 31, "responsibleUserId": 1, "updatedAt": time.Now().Unix()})
			now := time.Now().UTC()
			p, st := "20", "31"
			eventRevision++
			event := application.DistributionEventEnvelope{SchemaVersion: 1, MessageID: uuid.New(), Scope: core.Scope, EventID: uuid.New(), SourceOccurredAt: &now, ReceivedAt: now, EmittedAt: now, CorrelationID: uuid.New(), Event: application.DistributionCRMEvent{Kind: "lead.created", LeadID: strconv.FormatInt(id, 10), ObservationRevision: eventRevision, SourceEvidence: &application.DistributionSourceEvidence{PipelineID: &p, StatusID: &st}}}
			raw, _ := json.Marshal(event)
			request, e := http.NewRequestWithContext(ctx, "POST", teamURL+"/internal/v1/distribution/events", bytes.NewReader(raw))
			if e != nil {
				t.Fatal(e)
			}
			corebridge.Sign(request, core.KeyID, core.Secret, core.Scope, raw, time.Now())
			response, e := team.Client().Do(request)
			if e != nil {
				t.Fatal(e)
			}
			_ = response.Body.Close()
			if response.StatusCode != 202 {
				t.Fatal("event ingress rejected", response.StatusCode)
			}
			if found, e := svc.ProcessDistributionDelivery(ctx); !found || e != nil {
				t.Fatal(found, e)
			}
			return event
		}
		event := event31(90)
		if found, e := svc.ProcessDistributionObservation(ctx); !found || e != nil {
			t.Fatal(found, e)
		}
		if _, e := svc.ReceiveDistributionEvent(ctx, event); e != nil {
			t.Fatal(e)
		}
		if found, e := svc.ProcessDistributionObservation(ctx); found || e != nil {
			t.Fatal("replayed observation executed", found, e)
		}
		process()
		after := control(map[string]any{"action": "stats"})
		if after.PatchCalls != baseline.PatchCalls || after.Operations != baseline.Operations || after.Guards != baseline.Guards || after.AssignmentJobs != baseline.AssignmentJobs {
			t.Fatal("observation mutated Core", baseline, after)
		}
		var plans, queues, claims int
		if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM distribution_observations WHERE rule_id=$1),(SELECT count(*) FROM distribution_queue WHERE rule_id=$1),(SELECT count(*) FROM distribution_group_claims WHERE group_id=$2)`, observedRule.ID, observeGroup.ID).Scan(&plans, &queues, &claims); e != nil || plans != 1 || queues != 0 || claims != 0 {
			t.Fatal("observation mutated business queue", plans, queues, claims, e)
		}
		raw, e := svc.DistributionObservations(ctx, owner, observedRule.ID, 25, 0)
		if e != nil {
			t.Fatal(e)
		}
		var page struct {
			Items []application.DistributionObservation
		}
		if e = json.Unmarshal(raw, &page); e != nil || len(page.Items) != 1 || page.Items[0].DecisionKind != "assign" {
			t.Fatal(string(raw), e)
		}
		token := control(map[string]any{"action": "widget_token"}).WidgetToken
		body, _ := json.Marshal(map[string]any{"kind": "lead", "leadId": "90"})
		request, e := http.NewRequestWithContext(ctx, "POST", core.URL+"/api/v1/widget/distribution/runtime", bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("Origin", "https://test.amocrm.ru")
		request.Header.Set("X-Auth-Token", token)
		request.Header.Set("Content-Type", "application/json")
		response, e := (&http.Client{Timeout: 3 * time.Second}).Do(request)
		if e != nil {
			t.Fatal(e)
		}
		wire, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("actual widget observation status%d %s", response.StatusCode, wire)
		}
		var card struct {
			Latest *application.DistributionObservation `json:"latestObservation"`
			Items  []any
		}
		if e = json.Unmarshal(wire, &card); e != nil || card.Latest == nil || card.Latest.ID != page.Items[0].ID || len(card.Items) != 0 {
			t.Fatal("widget/Team observation disagree", string(wire), e)
		}
		// Offline operator coordinator fixture: actual legacy deployment remains
		// unobserved. The wrapper may call the real owner API only after stop/drain.
		writers := &rs10WriterFixture{legacyRunning: true}
		enable := func() error {
			var err error
			raw, err = svc.DistributionRuntimeWrite(ctx, owner, "rule", observedRule.ID, []byte(`{"expectedRevision":1,"active":true,"keepCurrentResponsible":true,"executionMode":"live"}`))
			return err
		}
		if err := writers.enableNew(enable); err == nil {
			t.Fatal("enabled new writer while legacy was running")
		}
		writers.legacyRunning = false
		writers.oldPending = 1
		writers.oldUnknown = true
		if err := writers.enableNew(enable); err == nil {
			t.Fatal("enabled while legacy effect remained unresolved")
		}
		writers.oldPending = 0
		writers.oldUnknown = false
		if err := writers.enableNew(enable); err != nil {
			t.Fatal(err)
		}
		if err := writers.returnLegacy(); err == nil {
			t.Fatal("returned legacy while new writer remained enabled")
		}
		writers.newRunning = false
		writers.newUnknown = true
		if err := writers.returnLegacy(); err == nil {
			t.Fatal("returned legacy before unknown new effects were settled")
		}

		if n, e := db.New(pool).AdmitDistributionEntries(ctx); e != nil || n != 0 {
			t.Fatal("old observation drained after enable", n, e)
		}
		control(map[string]any{"action": "resume"})
		// CRM source precision is seconds; use the actual next wall second after
		// the live boundary rather than inventing a future source timestamp.
		time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)) + 10*time.Millisecond)
		event31(91)
		admitted := false
		for n := 0; n < 10; n++ {
			process()
			if e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM distribution_queue WHERE rule_id=$1 AND lead_id='91' AND operation_id IS NOT NULL)`, observedRule.ID).Scan(&admitted); e != nil {
				t.Fatal(e)
			}
			if admitted {
				break
			}
		}
		if !admitted {
			t.Fatal("fresh live scope never received its own scheduler turn")
		}
		tick(t, "succeeded", baseline.PatchCalls+1)
		exec(`UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE rule_id=$1 AND lead_id='91'`, observedRule.ID)
		var confirmed string
		for n := 0; n < 10; n++ {
			process()
			if e = pool.QueryRow(ctx, `SELECT state FROM distribution_queue WHERE rule_id=$1 AND lead_id='91'`, observedRule.ID).Scan(&confirmed); e != nil {
				t.Fatal(e)
			}
			if confirmed == "confirmed" {
				break
			}
		}
		if e = pool.QueryRow(ctx, `SELECT state FROM distribution_queue WHERE rule_id=$1 AND lead_id='91'`, observedRule.ID).Scan(&confirmed); e != nil || confirmed != "confirmed" {
			t.Fatal(confirmed, e)
		}
	})
	t.Run("rs10_actual_two_owner_backup_restore_reconnect_preserves_unknown", func(t *testing.T) {
		control(map[string]any{"action": "pause"})
		var primary db.DistributionRule
		primary, e := db.New(pool).GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: company, ID: rule.ID})
		if e != nil {
			t.Fatal(e)
		}
		payload, _ := json.Marshal(map[string]any{"expectedRevision": primary.Revision, "active": false, "keepCurrentResponsible": primary.KeepCurrent})
		if _, e = svc.DistributionRuntimeWrite(ctx, owner, "rule", rule.ID, payload); e != nil {
			t.Fatal(e)
		}
		var identity struct {
			ContainerID string `json:"containerID"`
		}
		rs06ReadBridge(t, ctx, dir, "team-owner-container.json", &identity)
		backup := func(number int, action string) {
			t.Helper()
			rs06WriteBridge(t, dir, fmt.Sprintf("backup-request-%d.json", number), map[string]any{"action": action, "teamContainerID": identity.ContainerID})
			var result struct {
				OK     bool   `json:"ok"`
				Owners int    `json:"owners"`
				Error  string `json:"error"`
			}
			rs06ReadBridge(t, ctx, dir, fmt.Sprintf("backup-reply-%d.json", number), &result)
			if !result.OK || result.Owners != 2 {
				t.Fatal("two-owner backup coordinator failed", action, result.Error)
			}
		}
		before := control(map[string]any{"action": "stats"})
		var unknownID uuid.UUID
		var cursorBefore int32
		if e = pool.QueryRow(ctx, `SELECT operation_id FROM distribution_queue WHERE lead_id='20'`).Scan(&unknownID); e != nil {
			t.Fatal(e)
		}
		if e = pool.QueryRow(ctx, `SELECT cursor_next FROM distribution_group_claims WHERE group_id=$1`, group).Scan(&cursorBefore); e != nil {
			t.Fatal(e)
		}
		backup(1, "backup")
		backup(2, "restore")
		pool.Reset()
		svc = newService()
		handler.Store(&Handler{Keys: map[string]string{core.KeyID: core.Secret}, Store: db.New(pool), Service: svc})
		control(map[string]any{"action": "reconnect"})
		after := control(map[string]any{"action": "stats"})
		op, oe := client.Operation(ctx, core.Scope, unknownID)
		if oe != nil || op.State != "outcome_unknown" || op.ResolutionEvidence.GuardReleasable {
			t.Fatal("restore retired unknown proof", op.State, oe)
		}
		if before.Operations != after.Operations || before.Guards != after.Guards || before.AssignmentJobs != after.AssignmentJobs || before.PatchCalls != after.PatchCalls {
			t.Fatal("restore changed Core durable identities", before, after)
		}
		var restoredID uuid.UUID
		var cursorAfter int32
		if e = pool.QueryRow(ctx, `SELECT operation_id FROM distribution_queue WHERE lead_id='20'`).Scan(&restoredID); e != nil || restoredID != unknownID {
			t.Fatal("restore lost frozen operation", e)
		}
		if e = pool.QueryRow(ctx, `SELECT cursor_next FROM distribution_group_claims WHERE group_id=$1`, group).Scan(&cursorAfter); e != nil || cursorAfter != cursorBefore {
			t.Fatal("restore changed RR", e)
		}
		due()
		process()
		state(20, "uncertain")
		if reply := control(map[string]any{"action": "tick"}); reply.PatchCalls != before.PatchCalls {
			t.Fatal("restore/reconnect repeated PATCH", reply)
		}
	})

}

type rs06BridgeReply struct {
	WidgetToken    string                `json:"widgetToken"`
	Error          string                `json:"error"`
	Operation      *corebridge.Operation `json:"operation"`
	PatchCalls     int                   `json:"patchCalls"`
	Operations     int                   `json:"operations"`
	Guards         int                   `json:"guards"`
	AssignmentJobs int                   `json:"assignmentJobs"`
	NoJob          bool                  `json:"noJob"`
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
	container, err := postgres.Run(ctx, "postgres:16-alpine", testcontainers.WithLabels(map[string]string{"rkrs.distribution.bridge_run": os.Getenv("DISTRIBUTION_BRIDGE_RUN_ID")}), postgres.WithDatabase("company"), postgres.WithUsername("company"), postgres.WithPassword("company"), postgres.WithInitScripts(scripts...), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Error(err)
		}
	})
	if dir := os.Getenv("DISTRIBUTION_BRIDGE_DIR"); dir != "" {
		rs06WriteBridge(t, dir, "team-owner-container.json", map[string]any{"containerID": container.GetContainerID()})
	}
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

// This fixture exercises the offline stop/drain procedure. It neither queries
// nor controls rakurs-ssd, and is never an authorization bypass in runtime.
type rs10WriterFixture struct {
	legacyRunning, newRunning bool
	oldPending                int
	oldUnknown, newUnknown    bool
}

func (w *rs10WriterFixture) enableNew(enable func() error) error {
	if w.legacyRunning || w.oldPending > 0 || w.oldUnknown {
		return errors.New("legacy writer is not stopped and drained")
	}
	if err := enable(); err != nil {
		return err
	}
	w.newRunning = true
	return nil
}
func (w *rs10WriterFixture) returnLegacy() error {
	if w.newRunning || w.newUnknown {
		return errors.New("new writer or unknown effects prevent return")
	}
	w.legacyRunning = true
	return nil
}
