package activitybridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	dto "github.com/sk1fy/team-os-backend/services/company/internal/activitycontract"
)

func TestClientScopeMappingAndErrors(t *testing.T) {
	scope := Scope{CompanyId: uuid.New(), InstallationId: uuid.New(), IntegrationId: uuid.New(), AccountId: "42"}
	status := 200
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Activity-Account-Id") != "42" || r.Header.Get("X-Activity-Installation-Id") != scope.InstallationId.String() || r.Header.Get("X-Activity-Integration-Id") != scope.IntegrationId.String() || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Error("missing server scope")
		}
		if status != 200 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(status)
			return
		}
		if r.URL.Path == "/api/v1/activity/connection" {
			if err := json.NewEncoder(w).Encode(map[string]any{"installation_id": scope.InstallationId, "integration_id": scope.IntegrationId, "account_id": "42", "state": "active"}); err != nil {
				t.Errorf("encode connection: %v", err)
			}
			return
		}
		if r.URL.Path == "/api/v1/activity/task-panel" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode task panel: %v", err)
				return
			}
			if body["consider_future_tasks"] != true || body["revision"] != float64(4) {
				t.Errorf("mapping=%v", body)
			}
			if err := json.NewEncoder(w).Encode(body); err != nil {
				t.Errorf("encode task panel: %v", err)
			}
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
	defer server.Close()
	c, err := New(server.URL, strings.Repeat("x", 32))
	if err != nil {
		t.Fatal(err)
	}
	c.http = server.Client()
	if err = c.Verify(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	input := dto.ActivityTaskPanel{Enabled: true, EmployeeIds: []int64{7}, ConsiderFutureTasks: true, IncludeRelatedDeals: true, PriorityKeys: []string{"task_type"}, Revision: 4}
	out, err := c.SaveTaskPanel(context.Background(), scope, input)
	if err != nil || out.Revision != 4 || !out.ConsiderFutureTasks {
		t.Fatalf("round trip=%+v %v", out, err)
	}
	for _, code := range []int{401, 403, 404, 409, 429, 503} {
		status = code
		before := calls
		err = c.Verify(context.Background(), scope)
		var e *Error
		if !errors.As(err, &e) || e.Status != code || e.RetryAfter != "7" || calls != before+1 {
			t.Fatalf("status %d error=%v calls=%d", code, err, calls-before)
		}
	}
}
func TestBindingRejectsAmbiguity(t *testing.T) {
	b := Scope{CompanyId: uuid.New(), InstallationId: uuid.New(), IntegrationId: uuid.New(), AccountId: "42"}
	raw, _ := json.Marshal([]Scope{b, b})
	if _, err := ParseBindings(string(raw)); err == nil {
		t.Fatal("ambiguous binding accepted")
	}
	raw, _ = json.Marshal([]Scope{b})
	if _, err := ParseBindings(string(raw)); err != nil {
		t.Fatal(err)
	}
}
func TestClientRejectsMismatchedAuthority(t *testing.T) {
	s := Scope{CompanyId: uuid.New(), InstallationId: uuid.New(), IntegrationId: uuid.New(), AccountId: "42"}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{"installation_id": s.InstallationId, "integration_id": s.IntegrationId, "account_id": "43", "state": "active"}); err != nil {
			t.Errorf("encode connection: %v", err)
		}
	}))
	defer server.Close()
	c, _ := New(server.URL, strings.Repeat("x", 32))
	c.http = server.Client()
	var e *Error
	if err := c.Verify(context.Background(), s); !errors.As(err, &e) || e.Status != 403 {
		t.Fatalf("mismatch=%v", err)
	}
}

func TestPanelMappingPreservesCASAndIssuedLink(t *testing.T) {
	scope := Scope{CompanyId: uuid.New(), InstallationId: uuid.New(), IntegrationId: uuid.New(), AccountId: "42"}
	panelID := uuid.New()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode panel: %v", err)
			return
		}
		if string(body["revision"]) != "9" || string(body["employee_ids"]) != "[7]" || body["employeeIds"] != nil {
			t.Errorf("wrong panel mapping %v", body)
		}
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()
	c, _ := New(server.URL, strings.Repeat("x", 32))
	c.http = server.Client()
	_, err := c.Update(context.Background(), scope, panelID, dto.ActivityPanelPatch{Revision: 9, EmployeeIds: []int64{7}})
	var upstream *Error
	if !errors.As(err, &upstream) || upstream.Status != 409 {
		t.Fatalf("CAS error=%v", err)
	}
	public := panel{ID: panelID, Revision: 9, EmployeeIds: []int64{7}, Options: options{Operators: []operator{{EmployeeID: 7, Plan: 100, Thresholds: thresholds{DialingSeconds: 10, ConversationSeconds: 30, ExceedingSeconds: 180}}}}}.public()
	if public.Options.Operators[0].Thresholds.ConversationSeconds != 30 || public.Options.PipelineIds == nil || public.Options.Departments == nil || public.ShareUrl != nil {
		t.Fatalf("lossy panel mapping %+v", public)
	}
}

func TestMalformedSuccessIsUnavailableNotEmptyData(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{}`)); err != nil {
			t.Errorf("write malformed response: %v", err)
		}
	}))
	defer server.Close()
	c, _ := New(server.URL, strings.Repeat("x", 32))
	c.http = server.Client()
	_, err := c.Panels(context.Background(), Scope{})
	var upstream *Error
	if !errors.As(err, &upstream) || upstream.Status != 502 {
		t.Fatalf("malformed success became empty panels: %v", err)
	}
}
