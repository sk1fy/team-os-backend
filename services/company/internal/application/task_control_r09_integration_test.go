//go:build integration

package application

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// This cross-repository acceptance deliberately requires the actual widget checkout.
// Regular company CI has no access to that repository; the R09 runner sets WIDGET_DIR.
func TestTaskControlR09ActualWidgetRoundTrip(t *testing.T) {
	if os.Getenv("WIDGET_DIR") == "" {
		t.Skip("cross-repository acceptance requires WIDGET_DIR and Node.js")
	}
	f := newR05Fixture(t)
	q := db.New(f.pool)
	c, err := q.GetTaskControlConnection(f.ctx, db.GetTaskControlConnectionParams{CompanyID: f.actor.CompanyID, ID: f.binding})
	if err != nil {
		t.Fatal(err)
	}
	now := f.svc.now()
	core := f.svc.taskControlCore.(*referenceTestCore)
	core.status.RuntimeVersion = "1.2.3"
	core.snapshot = tc.ReferenceSnapshot{SchemaVersion: 1, Scope: referenceScope(c), Complete: true,
		Provenance: tc.ReferenceProvenance{Source: "coreAmocrm", FetchedAt: now, FetchStartedAt: now}, FreshUntil: now.Add(5 * time.Minute),
		Users: []tc.ReferenceUser{{ID: "7", Name: "Тест", IsActive: true}}, Groups: []tc.ReferenceGroup{},
		Pipelines: []tc.ReferencePipeline{{ID: "10", Name: "Тест", Statuses: []tc.ReferenceStatus{{ID: "11", Name: "Открыт", Kind: "open"}, {ID: "142", Name: "Закрыт", Kind: "won"}}}},
		Customers: tc.ReferenceCustomers{Availability: "unsupported", Statuses: []tc.ReferenceCustomerStatus{}, CheckedAt: now}}
	core.snapshot.Revision, err = tc.ReferenceRevision(core.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE task_control_installation_snapshots SET payload=$1 WHERE company_id=$2`, mustR05JSON(t, core.status), f.actor.CompanyID)
	f.exec(t, `INSERT INTO task_control_reference_cache(company_id,binding_id,scope_key,payload)VALUES($1,$2,$3,$4)`, f.actor.CompanyID, f.binding, referenceScopeKey(c), mustR05JSON(t, core.snapshot))
	f.exec(t, `INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id)VALUES($1,'123',$2,'7')`, f.actor.CompanyID, f.reader.UserID)
	if _, err = f.svc.ActivateTaskControlV2(f.ctx, f.actor, f.binding, TaskControlActivateInputV2{uuid.New(), 1, 1}); err != nil {
		t.Fatal(err)
	}
	rules := []tc.IntentRule{{Kind: "dynamic", Dynamic: &tc.RuleV2{SchemaVersion: 2, RuleUID: uuid.New(), Name: "Приёмка R09", Audience: tc.AudienceSelection{Kind: "allLinked"}, Leads: tc.LeadSelection{Kind: "allOpen"}, Customers: tc.CustomerSelection{Kind: "disabled"}, Enabled: true, ShowModal: true}}}
	draft, err := f.svc.UpdateTaskControlDraftV2(f.ctx, f.actor, f.binding, TaskControlDraftInputV2{1, 1, 2, rules})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := f.svc.PublishTaskControlDraftV2(f.ctx, f.actor, f.binding, TaskControlPublishInputV2{RequestID: uuid.New(), ExpectedBindingRevision: 1, ExpectedModeEpoch: 2, ExpectedDraftRevision: draft.Revision, ExpectedChecksum: draft.Checksum})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE task_control_connections SET mode='managed' WHERE company_id=$1 AND id=$2`, f.actor.CompanyID, f.binding)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.taskControlKeys = map[string]ed25519.PublicKey{"r09": public}
	// A loopback adapter exercises real application methods, not gateway/Core HTTP.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out any
		var callErr error
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/widget/task-control/sessions":
			var in struct {
				CoreAssertion  string   `json:"coreAssertion"`
				RuntimeVersion string   `json:"runtimeVersion"`
				Capabilities   []string `json:"capabilities"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			out, callErr = f.svc.ExchangeTaskControlSessionNegotiated(r.Context(), in.CoreAssertion, in.RuntimeVersion, in.Capabilities)
		case "GET /api/v2/task-control/runtime/bootstrap":
			out, callErr = f.svc.TaskControlBootstrapV2(r.Context(), token)
		case "POST /api/v1/task-control/runtime/telemetry":
			var in TaskControlTelemetryInput
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			callErr = f.svc.RecordTaskControlTelemetry(r.Context(), token, in)
		default:
			http.NotFound(w, r)
			return
		}
		if callErr != nil {
			http.Error(w, "application rejected request", http.StatusConflict)
			return
		}
		if out == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer server.Close()
	_, file, _, _ := runtime.Caller(0)
	runWidget := func(expectedRules int) {
		t.Helper()
		a := tc.Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: c.CompanyID, InstallationID: c.InstallationID, IntegrationID: c.IntegrationID, AccountID: "123", ActorID: "7", Product: tc.Product, Generation: 1, Purpose: "runtime", RequestID: uuid.New()}
		prefix := "r09." + base64.RawURLEncoding.EncodeToString(mustR05JSON(t, a))
		assertion := prefix + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, []byte("teamos-task-control/v1\n"+prefix)))
		input := map[string]any{"origin": server.URL, "now": now, "assertion": assertion, "expectedRules": expectedRules}
		cmd := exec.CommandContext(f.ctx, "node", filepath.Join(filepath.Dir(file), "../../../../scripts/task-control-r09-widget-roundtrip.cjs"))
		cmd.Stdin = bytes.NewReader(mustR05JSON(t, input))
		output, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("actual widget roundtrip: %v: %s", e, output)
		}
		t.Logf("actual widget: %s", output)
		report, e := f.svc.TaskControlApplicationV2(f.ctx, f.reader, f.binding, pub.ID)
		if e != nil {
			t.Fatal(e)
		}
		var status struct {
			RuntimeState     string `json:"runtimeState"`
			AppliedInstances int    `json:"appliedInstances"`
		}
		if e = json.Unmarshal(mustR05JSON(t, report), &status); e != nil || status.RuntimeState != "appliedByRuntime" || status.AppliedInstances != 1 {
			t.Fatalf("real receipts not reflected in application: %+v, %v", status, e)
		}
	}
	runWidget(1)
	// Changing the account-scoped mapping recomputes the same publication to an
	// empty actor projection; the actual widget must apply it without standalone fallback.
	f.exec(t, `DELETE FROM task_control_employee_mappings WHERE company_id=$1 AND account_id='123'`, f.actor.CompanyID)
	if err = q.DrainTaskControlV2SourceChanges(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = q.QueueTaskControlV2Jobs(f.ctx); err != nil {
		t.Fatal(err)
	}
	if worked, e := f.svc.resolveTaskControlV2Job(f.ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	runWidget(0)
}
