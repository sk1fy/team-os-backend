//go:build integration

package distributionhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestSignedDecisionValidationPostgresScopeAndLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, filename, _, _ := runtime.Caller(0)
	directory := filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations")
	sources, e := filepath.Glob(filepath.Join(directory, "*.up.sql"))
	if e != nil {
		t.Fatal(e)
	}
	temporary := t.TempDir()
	scripts := []string{}
	for _, source := range sources {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(temporary, filepath.Base(source))
		if err = os.WriteFile(target, append(append([]byte("BEGIN;\n"), raw...), []byte("\nCOMMIT;\n")...), 0600); err != nil {
			t.Fatal(err)
		}
		scripts = append(scripts, target)
	}
	container, e := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("company"), postgres.WithUsername("company"), postgres.WithPassword("company"), postgres.WithInitScripts(scripts...), postgres.BasicWaitStrategies())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Error(err)
		}
	})
	dsn, e := container.ConnectionString(ctx, "sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	company, employee, installation, binding, integration := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO companies(id,name) VALUES($1,'Fixture')", company)
	exec("INSERT INTO users(id,company_id,email,first_name,role,status) VALUES($1,$2,'fixture@example.invalid','Fixture','employee','active')", employee, company)
	exec("INSERT INTO employee_section_access(company_id,user_id,section) VALUES($1,$2,'distribution') ON CONFLICT DO NOTHING", company, employee)
	exec("INSERT INTO distribution_bindings(id,company_id,installation_id,integration_id,account_id,state,intent_id,initiated_by,initiator_snapshot,expires_at,mapping_revision,mapping_ack_revision) VALUES($1,$2,$3,$4,'123','active',$5,$6,$6,now()+interval '15 minutes',1,1)", binding, company, installation, integration, uuid.New(), employee)
	exec("INSERT INTO distribution_binding_versions(company_id,binding_id,revision,installation_id,integration_id,account_id,state) VALUES($1,$2,1,$3,$4,'123','active')", company, binding, installation, integration)
	exec("INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,'42','verified',now())", uuid.New(), company, binding, employee)
	exec("INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES('core',$1,$2,'widget-access')", company, installation)
	service, e := application.NewService(pool, nil)
	if e != nil {
		t.Fatal(e)
	}
	secret := strings.Repeat("s", 32)
	handler := &Handler{Keys: map[string]string{"core": secret}, Store: db.New(pool), Service: service}
	scope := corebridge.Scope{CompanyID: company, BindingID: binding, BindingRevision: 1, InstallationID: installation, IntegrationID: integration, AccountID: "123"}
	widgetIn := application.DistributionWidgetRuntimeInput{Scope: scope, UserID: "42", PrincipalExpiresAt: time.Now().Add(time.Minute), Kind: "groups"}
	widgetCall := func(in application.DistributionWidgetRuntimeInput, headers http.Header) (int, []byte, http.Header) {
		t.Helper()
		raw, _ := json.Marshal(in)
		req := httptest.NewRequestWithContext(ctx, "POST", "https://company.internal.example/internal/v1/distribution/widget-runtime", bytes.NewReader(raw))
		corebridge.Sign(req, "core", secret, in.Scope, raw, time.Now())
		if headers != nil {
			req.Header = headers.Clone()
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code, w.Body.Bytes(), req.Header.Clone()
	}
	if status, _, _ := widgetCall(widgetIn, nil); status != 403 {
		t.Fatalf("widget runtime grant %d", status)
	}
	exec("INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES('core',$1,$2,'widget-runtime')", company, installation)
	status, _, widgetHeaders := widgetCall(widgetIn, nil)
	if status != 200 {
		t.Fatalf("actual signed widget read %d", status)
	}
	if status, _, _ := widgetCall(widgetIn, widgetHeaders); status != 401 {
		t.Fatalf("widget nonce replay %d", status)
	}
	widgetIn.Scope.AccountID = "124"
	if status, _, _ := widgetCall(widgetIn, nil); status != 403 {
		t.Fatalf("widget wrong binding %d", status)
	}
	widgetIn.Scope.AccountID = "123"
	widgetGroup, widgetRule := uuid.New(), uuid.New()
	exec("INSERT INTO distribution_groups(id,company_id,name,algorithm,member_ids) VALUES($1,$2,'Shared signed','round_robin',$3)", widgetGroup, company, []uuid.UUID{employee})
	exec("INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id) VALUES($1,$2,$3,1,'123','20','30',$4)", widgetRule, company, binding, widgetGroup)
	widgetIn.Write = true
	widgetIn.Kind = "rule"
	widgetIn.ID = widgetRule
	widgetIn.RequestID = uuid.New()
	widgetIn.Payload = json.RawMessage(`{"expectedRevision":1,"active":false,"keepCurrentResponsible":false}`)
	if status, _, _ := widgetCall(widgetIn, nil); status != 403 {
		t.Fatalf("employee write %d", status)
	}
	exec("UPDATE users SET role='owner' WHERE id=$1", employee)
	if status, body, _ := widgetCall(widgetIn, nil); status != 200 || !bytes.Contains(body, []byte(`"revision":2`)) {
		t.Fatalf("signed shared mutation %d %s", status, body)
	}
	widgetIn.PrincipalExpiresAt = time.Now().Add(2 * time.Minute)
	if status, body, _ := widgetCall(widgetIn, nil); status != 200 || !bytes.Contains(body, []byte(`"revision": 2`)) {
		t.Fatalf("signed receipt replay with new JWT %d %s", status, body)
	}
	exec("UPDATE users SET role='employee' WHERE id=$1", employee)
	now := time.Now().UTC()
	envelope := application.DistributionEventEnvelope{SchemaVersion: 1, MessageID: uuid.New(), Scope: scope, EventID: uuid.New(), CorrelationID: uuid.New(), ReceivedAt: now, EmittedAt: now, Event: application.DistributionCRMEvent{Kind: "lead.snapshot_reconciled", LeadID: "7", ObservationRevision: 1}}
	delivery := func(in application.DistributionEventEnvelope, headers http.Header) (int, application.DistributionDeliveryReceipt, http.Header) {
		t.Helper()
		raw, _ := json.Marshal(in)
		r := httptest.NewRequestWithContext(ctx, "POST", "https://company.internal.example/internal/v1/distribution/events", bytes.NewReader(raw))
		corebridge.Sign(r, "core", secret, in.Scope, raw, time.Now())
		if headers != nil {
			r.Header = headers.Clone()
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var receipt application.DistributionDeliveryReceipt
		if w.Code == 202 {
			if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, receipt, r.Header.Clone()
	}
	if status, _, _ := delivery(envelope, nil); status != 403 {
		t.Fatalf("delivery capability %d", status)
	}
	exec("INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES('core',$1,$2,'event-delivery')", company, installation)
	statusDelivery, receipt, deliveryHeaders := delivery(envelope, nil)
	if statusDelivery != 202 || receipt.MessageID != envelope.MessageID || receipt.ReceiptID == uuid.Nil {
		t.Fatalf("durable ACK %d %+v", statusDelivery, receipt)
	}
	if status, _, _ := delivery(envelope, deliveryHeaders); status != 401 {
		t.Fatalf("nonce replay %d", status)
	}
	if status, duplicate, _ := delivery(envelope, nil); status != 202 || duplicate.ReceiptID != receipt.ReceiptID || duplicate.Disposition != "duplicate" {
		t.Fatalf("lost ACK %d %+v", status, duplicate)
	}
	envelope.Event.Kind = "lead.deleted"
	if status, _, _ := delivery(envelope, nil); status != 409 {
		t.Fatalf("message conflict %d", status)
	}
	envelope.MessageID = uuid.New()
	envelope.Scope.AccountID = "124"
	if status, _, _ := delivery(envelope, nil); status != 403 {
		t.Fatalf("historical scope mismatch %d", status)
	}
	exec("INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES('core',$1,$2,'result-delivery')", company, installation)
	outcome := "assigned"
	malformed := application.DistributionResultEnvelope{SchemaVersion: 1, MessageID: uuid.New(), Scope: scope, EventID: uuid.New(), CorrelationID: uuid.New(), ReceivedAt: now, EmittedAt: now, Result: corebridge.Operation{LeadID: "7", Scope: scope, OperationID: uuid.New(), EpisodeID: uuid.New(), DecisionID: uuid.New(), RuleID: uuid.New(), GroupID: uuid.New(), RuleRevision: 1, AvailabilityRevision: 1, ClaimRevision: 1, TargetResponsibleUserID: "42", State: "succeeded", ExternalEffectState: "unknown", Outcome: &outcome, ResultVersion: 1, ResolutionEvidence: corebridge.OperationEvidence{Kind: "observed_state_only", ObservedAt: now, GuardReleasable: true}, AcceptedAt: now, UpdatedAt: now}}
	malformed.Result.EventID = malformed.EventID
	malformed.Result.CorrelationID = malformed.CorrelationID
	malformedRaw, _ := json.Marshal(malformed)
	malformedRequest := httptest.NewRequestWithContext(ctx, "POST", "https://company.internal.example/internal/v1/distribution/results", bytes.NewReader(malformedRaw))
	corebridge.Sign(malformedRequest, "core", secret, scope, malformedRaw, time.Now())
	malformedResponse := httptest.NewRecorder()
	handler.ServeHTTP(malformedResponse, malformedRequest)
	if malformedResponse.Code != 400 {
		t.Fatalf("false completion ACK %d", malformedResponse.Code)
	}
	input := application.DistributionDecisionValidationInput{Actor: application.DistributionDecisionActor{Kind: "system"}, Scope: scope, OperationID: uuid.New(), DecisionID: uuid.New(), EpisodeID: uuid.New(), RuleID: uuid.New(), GroupID: uuid.New(), RuleRevision: 1, AvailabilityRevision: 1, ClaimRevision: 1, WorkerFence: 1, TargetEmployeeID: employee, TargetResponsibleUserID: "42", LeadID: "7", DecisionKind: "assign", ValidUntil: time.Now().Add(10 * time.Minute)}
	request := func(in application.DistributionDecisionValidationInput, headers *http.Header) (int, application.DistributionDecisionValidation, http.Header) {
		t.Helper()
		raw, _ := json.Marshal(in)
		r := httptest.NewRequestWithContext(ctx, "POST", "https://company.internal.example/internal/v1/distribution/validate-decision", bytes.NewReader(raw))
		corebridge.Sign(r, "core", secret, in.Scope, raw, time.Now())
		if headers != nil {
			r.Header = headers.Clone()
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var out application.DistributionDecisionValidation
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, out, r.Header.Clone()
	}
	if status, _, _ := request(input, nil); status != 403 {
		t.Fatalf("widget grant crossed capability %d", status)
	}
	exec("INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES('core',$1,$2,'decision-validation')", company, installation)
	status, out, headers := request(input, nil)
	if status != 200 || out.Allowed || out.Reason != "decision_not_ready" || out.OperationID != input.OperationID || out.DecisionID != input.DecisionID || out.WorkerFence != input.WorkerFence {
		t.Fatalf("unexpected response %d %+v", status, out)
	}
	if status, _, _ := request(input, &headers); status != 401 {
		t.Fatalf("durable replay %d", status)
	}
	wrong := input
	wrong.CompanyID = uuid.New()
	if status, _, _ := request(wrong, nil); status != 403 {
		t.Fatalf("foreign company %d", status)
	}
	wrong = input
	wrong.InstallationID = uuid.New()
	if status, _, _ := request(wrong, nil); status != 403 {
		t.Fatalf("foreign installation %d", status)
	}
	check := func(reason string) {
		t.Helper()
		status, out, _ := request(input, nil)
		if status != 200 || out.Allowed || out.Reason != reason {
			t.Fatalf("%s: %d %+v", reason, status, out)
		}
	}
	exec("UPDATE distribution_bindings SET mapping_ack_revision=0 WHERE id=$1", binding)
	check("binding_unavailable")
	exec("UPDATE distribution_bindings SET mapping_ack_revision=1 WHERE id=$1", binding)
	exec("UPDATE companies SET status='frozen' WHERE id=$1", company)
	check("company_unavailable")
	exec("UPDATE companies SET status='active' WHERE id=$1", company)
	exec("UPDATE distribution_bindings SET state='revoked',revision=2 WHERE id=$1", binding)
	check("binding_unavailable")
	exec("UPDATE distribution_bindings SET state='active',revision=1 WHERE id=$1", binding)
	expired := input
	expired.ValidUntil = time.Now().Add(-time.Second)
	if status, out, _ := request(expired, nil); status != 200 || out.Allowed || out.Reason != "decision_expired" {
		t.Fatalf("expired %d %+v", status, out)
	}

	actorCRM := "42"
	userInput := input
	userInput.Actor = application.DistributionDecisionActor{Kind: "user", TeamOSUserID: &employee, CRMUserID: &actorCRM}
	if status, out, _ := request(userInput, nil); status != 200 || out.Allowed || out.Reason != "decision_not_ready" {
		t.Fatalf("ordinary actor denied incorrectly %d %+v", status, out)
	}
	exec("DELETE FROM employee_section_access WHERE company_id=$1 AND user_id=$2 AND section='distribution'", company, employee)
	if status, out, _ := request(userInput, nil); status != 200 || out.Allowed || out.Reason != "actor_unavailable" {
		t.Fatalf("actor section ignored %d %+v", status, out)
	}
	exec("INSERT INTO employee_section_access(company_id,user_id,section) VALUES($1,$2,'distribution')", company, employee)
	exec("UPDATE users SET status='deactivated' WHERE id=$1", employee)
	check("mapping_unavailable")
	exec("DELETE FROM users WHERE id=$1", employee)
	check("mapping_unavailable")
}
