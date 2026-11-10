//go:build integration

package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"github.com/sk1fy/team-os-backend/services/company/internal/externalusers"
)

type importHTTPResponse struct {
	body   string
	status int
	length int
}

type importHTTPFixture struct {
	mu       sync.Mutex
	response importHTTPResponse
	accounts []string
}

func (f *importHTTPFixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodPost || body["app_name"] != "rkrs_activity" || body["amo_account_id"] == "" {
		t.Errorf("unexpected employee source request: method=%s body=%v", r.Method, body)
	}
	f.mu.Lock()
	f.accounts = append(f.accounts, body["amo_account_id"])
	response := f.response
	f.mu.Unlock()
	if response.length > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(response.length))
	}
	if response.status != 0 {
		w.WriteHeader(response.status)
	}
	_, _ = w.Write([]byte(response.body))
}

func (f *importHTTPFixture) set(response importHTTPResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.response = response
}

func (f *importHTTPFixture) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.accounts...)
}

func TestAmoImportHTTPToPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	pool := application.EmployeeImportTestPool(t, ctx)
	fixture := &importHTTPFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fixture.serve(t, w, r) }))
	defer server.Close()
	actor := seedImportCompany(t, ctx, pool, "31355990")
	other := seedImportCompany(t, ctx, pool, "31355991")
	body := `{"result":true,"data":[{"id":"42","name":"Сотрудник Один","email":"employee@example.com","group":{"id":"g1","name":"Продажи"}},{"id":"43","name":"Сотрудник Два","group":{"id":"g2","name":"Администратор"}}]}`
	fixture.set(importHTTPResponse{body: body, status: http.StatusOK})
	users := runHTTPImport(t, ctx, pool, server.URL, actor)
	assertRequestedAccount(t, fixture, "31355990", 1)
	if len(users) != 3 {
		t.Fatalf("returned users=%d, want owner and two employees", len(users))
	}
	var employeeID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE company_id=$1 AND external_id='42'`, actor.CompanyID).Scan(&employeeID); err != nil {
		t.Fatal(err)
	}
	var mappings, departments, unwantedAccess int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM task_control_employee_mappings m JOIN users u ON u.company_id=m.company_id AND u.id=m.employee_id
		 WHERE m.company_id=$1 AND m.account_id='31355990' AND m.crm_user_id=u.external_id AND u.external_id IN ('42','43')),
		(SELECT count(*) FROM departments WHERE company_id=$1 AND source='amo'),
		(SELECT count(*) FROM users u WHERE company_id=$1 AND source='amo' AND (role<>'employee' OR show_in_schedule OR
		 EXISTS(SELECT 1 FROM credentials c WHERE c.user_id=u.id) OR EXISTS(SELECT 1 FROM access_links l WHERE l.user_id=u.id) OR
		 EXISTS(SELECT 1 FROM sessions s WHERE s.user_id=u.id) OR EXISTS(SELECT 1 FROM user_external_identities i WHERE i.user_id=u.id)))`,
		actor.CompanyID).Scan(&mappings, &departments, &unwantedAccess); err != nil {
		t.Fatal(err)
	}
	if mappings != 2 || departments != 2 || unwantedAccess != 0 {
		t.Fatalf("mappings=%d departments=%d unwanted access=%d", mappings, departments, unwantedAccess)
	}
	var created int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox o JOIN users u
		ON u.company_id=o.company_id AND u.id::text=o.payload->'payload'->'user'->>'userId'
		WHERE o.company_id=$1 AND o.subject='teamos.org.user.created.v1' AND u.external_id IN ('42','43')
		AND o.payload->>'companyId'=$1::text`, actor.CompanyID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("created employee events=%d, want 2", created)
	}
	before := importState(t, ctx, pool, actor.CompanyID)
	departmentIDs := importDepartmentIDs(t, ctx, pool, actor.CompanyID)
	delete(before, "departments")
	delete(before, "task_control_reference_revisions")
	runHTTPImport(t, ctx, pool, server.URL, actor)
	assertRequestedAccount(t, fixture, "31355990", 2)
	afterRepeat := importState(t, ctx, pool, actor.CompanyID)
	delete(afterRepeat, "departments")
	delete(afterRepeat, "task_control_reference_revisions")
	if !reflect.DeepEqual(before, afterRepeat) {
		t.Fatalf("identical snapshot changed employee/access/event state\nbefore=%v\nafter=%v", before, afterRepeat)
	}
	if ids := importDepartmentIDs(t, ctx, pool, actor.CompanyID); !reflect.DeepEqual(departmentIDs, ids) {
		t.Fatalf("repeated import changed department IDs: before=%v after=%v", departmentIDs, ids)
	}

	runHTTPImport(t, ctx, pool, server.URL, other)
	assertRequestedAccount(t, fixture, "31355991", 3)
	var otherEmployee uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT u.id FROM users u JOIN task_control_employee_mappings m ON m.company_id=u.company_id AND m.employee_id=u.id
		WHERE u.company_id=$1 AND u.external_id='42' AND m.account_id='31355991' AND m.crm_user_id='42'`, other.CompanyID).Scan(&otherEmployee); err != nil {
		t.Fatal(err)
	}
	if otherEmployee == employeeID {
		t.Fatal("same external ID shared an employee across companies")
	}
	otherBefore := importState(t, ctx, pool, other.CompanyID)
	execImportSQL(t, ctx, pool, `UPDATE users SET status='deactivated',first_name='Локальное имя',role='admin' WHERE company_id=$1 AND id=$2`, actor.CompanyID, employeeID)
	fixture.set(importHTTPResponse{body: `{"result":true,"data":[{"id":"42","name":"Источник изменил имя","email":"employee@example.com","group":{"id":"g2","name":"Поддержка"}},{"id":"43","name":"Сотрудник Два","group":{"id":"g2","name":"Поддержка"}}]}`, status: http.StatusOK})
	runHTTPImport(t, ctx, pool, server.URL, actor)
	assertRequestedAccount(t, fixture, "31355990", 4)
	var id uuid.UUID
	var name, status, role, department string
	if err := pool.QueryRow(ctx, `SELECT u.id,u.first_name,u.status,u.role,d.name FROM users u
		JOIN user_departments ud ON ud.company_id=u.company_id AND ud.user_id=u.id
		JOIN departments d ON d.company_id=ud.company_id AND d.id=ud.department_id
		WHERE u.company_id=$1 AND u.external_id='42'`, actor.CompanyID).Scan(&id, &name, &status, &role, &department); err != nil {
		t.Fatal(err)
	}
	if id != employeeID || name != "Локальное имя" || status != "deactivated" || role != "admin" || department != "Поддержка" {
		t.Fatalf("existing profile or department wrong: %s %s %s %s %s", id, name, status, role, department)
	}
	var updated int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE company_id=$1 AND subject='teamos.org.user.updated.v1'
		AND payload->'payload'->'user'->>'userId'=$2 AND payload->'payload'->'changedFields' ? 'departmentIds'`,
		actor.CompanyID, employeeID.String()).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("department change events=%d, want 1", updated)
	}
	if after := importState(t, ctx, pool, other.CompanyID); !reflect.DeepEqual(otherBefore, after) {
		t.Fatal("refresh changed another company's state")
	}
}

func TestAmoImportHTTPFailuresPreservePostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	pool := application.EmployeeImportTestPool(t, ctx)
	actor := seedImportCompany(t, ctx, pool, "31355990")
	fixture := &importHTTPFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fixture.serve(t, w, r) }))
	defer server.Close()
	fixture.set(importHTTPResponse{body: `{"result":true,"data":[{"id":"42","name":"Сотрудник","email":"employee@example.com","group":{"id":"g1","name":"Продажи"}}]}`, status: http.StatusOK})
	runHTTPImport(t, ctx, pool, server.URL, actor)
	var employee uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE company_id=$1 AND external_id='42'`, actor.CompanyID).Scan(&employee); err != nil {
		t.Fatal(err)
	}
	execImportSQL(t, ctx, pool, `INSERT INTO credentials(company_id,user_id,password_hash) VALUES($1,$2,'fixture-hash')`, actor.CompanyID, employee)
	execImportSQL(t, ctx, pool, `INSERT INTO access_links(company_id,user_id,token) VALUES($1,$2,'fixture-link')`, actor.CompanyID, employee)
	session := uuid.New()
	execImportSQL(t, ctx, pool, `INSERT INTO sessions(id,company_id,user_id,refresh_hash,expires_at) VALUES($1,$2,$3,$4,now()+interval '1 day')`, session, actor.CompanyID, employee, session[:])
	execImportSQL(t, ctx, pool, `INSERT INTO distribution_groups(id,company_id,name,member_ids) VALUES($1,$2,'fixture',$3)`, uuid.New(), actor.CompanyID, []uuid.UUID{employee})
	baseline := importState(t, ctx, pool, actor.CompanyID)
	valid := `{"result":true,"data":[]}`
	tests := []struct {
		name   string
		body   string
		code   int
		length int
	}{
		{name: "trailing garbage", body: valid + "broken"},
		{name: "second JSON", body: valid + valid},
		{name: "oversized whitespace", body: valid + strings.Repeat(" ", 4<<20)},
		{name: "truncated", body: `{"result":true,"data":[`},
		{name: "read failure after valid JSON", body: valid, length: 100},
		{name: "missing array", body: `{"result":true}`},
		{name: "business error", body: `{"result":false,"data":[]}`},
		{name: "duplicate IDs", body: `{"result":true,"data":[{"id":"50"},{"id":"50"}]}`},
		{name: "duplicate email", body: `{"result":true,"data":[{"id":"50","email":"A@example.com"},{"id":"51","email":"a@example.com"}]}`},
		{name: "invalid email", body: `{"result":true,"data":[{"id":"50","email":"invalid"}]}`},
		{name: "incomplete group", body: `{"result":true,"data":[{"id":"50","group":{"id":"g2"}}]}`},
		{name: "source unavailable", body: valid, code: http.StatusServiceUnavailable},
		{name: "source rate limit", body: valid, code: http.StatusTooManyRequests},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture.set(importHTTPResponse{body: test.body, status: test.code, length: test.length})
			users := runHTTPImport(t, ctx, pool, server.URL, actor)
			assertRequestedAccount(t, fixture, "31355990", index+2)
			if len(users) != 2 {
				t.Fatalf("source failure hid existing users: count=%d", len(users))
			}
			if after := importState(t, ctx, pool, actor.CompanyID); !reflect.DeepEqual(baseline, after) {
				t.Fatalf("failed snapshot mutated persisted state\nbefore=%v\nafter=%v", baseline, after)
			}
		})
	}
	t.Run("connection refused", func(t *testing.T) {
		server.Close()
		users := runHTTPImport(t, ctx, pool, server.URL, actor)
		if len(users) != 2 {
			t.Fatalf("transport failure hid existing users: count=%d", len(users))
		}
		if after := importState(t, ctx, pool, actor.CompanyID); !reflect.DeepEqual(baseline, after) {
			t.Fatal("transport failure changed persisted state")
		}
	})
}

func seedImportCompany(t *testing.T, ctx context.Context, pool *pgxpool.Pool, account string) application.Actor {
	t.Helper()
	actor := application.Actor{CompanyID: uuid.New(), UserID: uuid.New(), Role: "owner"}
	execImportSQL(t, ctx, pool, `INSERT INTO companies(id,name,amo_account_id,status,onboarding_completed_at) VALUES($1,'Компания',$2,'active',now())`, actor.CompanyID, account)
	execImportSQL(t, ctx, pool, `INSERT INTO users(id,company_id,email,first_name,last_name,role,status) VALUES($1,$2,$3,'Владелец','','owner','active')`, actor.UserID, actor.CompanyID, actor.UserID.String()+"@example.com")
	execImportSQL(t, ctx, pool, `UPDATE companies SET owner_id=$2 WHERE id=$1`, actor.CompanyID, actor.UserID)
	return actor
}

func runHTTPImport(t *testing.T, ctx context.Context, pool *pgxpool.Pool, endpoint string, actor application.Actor) []application.User {
	t.Helper()
	client, err := externalusers.NewClient(externalusers.Config{APIURL: endpoint, AppName: "rkrs_activity", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	service, err := application.NewService(pool, nil, application.WithExternalEmployeeProvider(client))
	if err != nil {
		t.Fatal(err)
	}
	users, err := service.ListUsers(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	return users
}

func assertRequestedAccount(t *testing.T, fixture *importHTTPFixture, account string, count int) {
	t.Helper()
	requests := fixture.requests()
	if len(requests) != count || requests[len(requests)-1] != account {
		t.Fatalf("source requests=%v, want %d requests ending with %s", requests, count, account)
	}
}

func execImportSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}

func importState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, company uuid.UUID) map[string]string {
	t.Helper()
	state := make(map[string]string)
	for _, table := range []string{"users", "departments", "user_departments", "task_control_employee_mappings", "task_control_reference_revisions", "credentials", "access_links", "sessions", "employee_section_access", "employee_access_audit", "distribution_groups", "outbox", "user_admin_audit", "user_external_identities", "amo_company_entries"} {
		var rows string
		sql := fmt.Sprintf(`SELECT COALESCE(jsonb_agg(to_jsonb(record) ORDER BY to_jsonb(record)::text),'[]'::jsonb)::text FROM %s AS record WHERE company_id=$1`, table)
		if err := pool.QueryRow(ctx, sql, company).Scan(&rows); err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		state[table] = rows
	}
	return state
}

func importDepartmentIDs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, company uuid.UUID) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT array_agg(id ORDER BY id) FROM departments WHERE company_id=$1`, company).Scan(&ids); err != nil {
		t.Fatal(err)
	}
	return ids
}
