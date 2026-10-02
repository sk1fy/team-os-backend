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
	exec("INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,'42','verified',now())", uuid.New(), company, binding, employee)
	exec("INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES('core',$1,$2,'widget-access')", company, installation)
	service, e := application.NewService(pool, nil)
	if e != nil {
		t.Fatal(e)
	}
	secret := strings.Repeat("s", 32)
	handler := &Handler{Keys: map[string]string{"core": secret}, Store: db.New(pool), Service: service}
	scope := corebridge.Scope{CompanyID: company, BindingID: binding, BindingRevision: 1, InstallationID: installation, IntegrationID: integration, AccountID: "123"}
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
