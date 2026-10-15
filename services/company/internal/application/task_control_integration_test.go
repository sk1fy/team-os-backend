//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type tcTestCore struct {
	status tc.Installation
	fail   bool
	calls  int
}

func (f *tcTestCore) Installation(_ context.Context, c uuid.UUID, a string) (tc.Installation, error) {
	f.calls++
	if f.fail {
		return tc.Installation{}, errors.New("offline")
	}
	return f.status, nil
}
func TestTaskControlLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	_, file, _, _ := runtime.Caller(0)
	migration, e := os.ReadFile(filepath.Join(filepath.Dir(file), "../../migrations/000032_task_control_connections.up.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	migration, e = os.ReadFile(filepath.Join(filepath.Dir(file), "../../migrations/000033_task_control_rules.up.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	company, ownerID, employeeID := uuid.New(), uuid.New(), uuid.New()
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	employee := Actor{CompanyID: company, UserID: employeeID, Role: "employee"}
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}, {employeeID, "employee", "active"}})
	if _, e = pool.Exec(ctx, `INSERT INTO company_integrations(id,company_id,provider,external_account_id,status) VALUES($1,$2,'rakurs','123','active')`, uuid.New(), company); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Second)
	install, integration := uuid.New(), uuid.New()
	core := &tcTestCore{status: tc.Installation{CompanyID: company, AccountID: "123", InstallationID: &install, IntegrationID: &integration, Product: tc.Product, State: "installed", Revision: 1, Generation: 1, CheckedAt: now, FreshUntil: now.Add(5 * time.Minute)}}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	svc := &Service{pool: pool, now: func() time.Time { return now }, taskControlCore: core, taskControlKeys: map[string]ed25519.PublicKey{"key": pub}}
	assertion := func(purpose string, request uuid.UUID, admin bool) string {
		a := tc.Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: company, InstallationID: install, IntegrationID: integration, AccountID: "123", ActorID: "7", Product: tc.Product, Generation: core.status.Generation, Admin: admin, Purpose: purpose, RequestID: request}
		raw, _ := json.Marshal(a)
		prefix := "key." + base64.RawURLEncoding.EncodeToString(raw)
		return prefix + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte("teamos-task-control/v1\n"+prefix)))
	}
	if _, e = pool.Exec(ctx, `INSERT INTO employee_section_access(company_id,user_id,section) VALUES($1,$2,'distribution')`, company, employeeID); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.GetTaskControlInstallation(ctx, employee, "123"); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("employee rights: %v", e)
	}
	if _, e = svc.GetTaskControlInstallation(ctx, owner, "999"); !isCompanyError(e, ErrorNotFound) {
		t.Fatalf("foreign account: %v", e)
	}
	intentID := uuid.New()
	in := TaskControlLinkInput{IntentID: intentID, InstallationID: install, IntegrationID: integration, AccountID: "123", CoreAssertion: assertion("link", intentID, true)}
	if _, e = pool.Exec(ctx, `INSERT INTO task_control_permissions(company_id,user_id,can_read,can_manage) VALUES($1,$2,true,false)`, company, ownerID); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.LinkTaskControl(ctx, owner, in); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("manage override ignored", e)
	}
	if _, e = pool.Exec(ctx, `UPDATE task_control_permissions SET can_manage=true WHERE company_id=$1 AND user_id=$2`, company, ownerID); e != nil {
		t.Fatal(e)
	}
	intent, e := svc.LinkTaskControl(ctx, owner, in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := svc.LinkTaskControl(ctx, owner, in)
	if e != nil || again.Scope.BindingID != intent.Scope.BindingID {
		t.Fatalf("repeat link: %v", e)
	}
	wrong := in
	wrong.InstallationID = uuid.New()
	if _, e = svc.LinkTaskControl(ctx, owner, wrong); e == nil {
		t.Fatal("changed link accepted")
	}
	binding := intent.Scope.BindingID
	draft, e := svc.GetTaskControlDraft(ctx, owner, binding)
	if e != nil || draft.Revision != 1 || len(draft.Rules) != 0 {
		t.Fatal("empty draft", e)
	}
	if _, e = svc.GetTaskControlDraft(ctx, employee, binding); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("employee draft", e)
	}
	outsider := owner
	outsider.CompanyID = uuid.New()
	if _, e = svc.GetTaskControlDraft(ctx, outsider, binding); e == nil {
		t.Fatal("cross tenant")
	}
	fixture, e := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../contracts/fixtures/task-control-v1.json"))
	if e != nil {
		t.Fatal(e)
	}
	var fx struct {
		Rules    []tc.Rule `json:"rules"`
		Checksum string    `json:"checksum"`
	}
	if e = json.Unmarshal(fixture, &fx); e != nil {
		t.Fatal(e)
	}
	update := TaskControlDraftInput{1, 1, 1, fx.Rules}
	draft, e = svc.UpdateTaskControlDraft(ctx, owner, binding, update)
	if e != nil || draft.Checksum != fx.Checksum {
		t.Fatal("save/checksum", e, draft.Checksum)
	}
	if _, e = svc.UpdateTaskControlDraft(ctx, owner, binding, update); !isCompanyError(e, ErrorConflict) {
		t.Fatal("stale draft", e)
	}
	publish := TaskControlPublishInput{1, 1, uuid.New(), draft.Revision, draft.Checksum, "MVP"}
	publication, e := svc.PublishTaskControlDraft(ctx, owner, binding, publish)
	if e != nil {
		t.Fatal("publish", e)
	}
	repeated, e := svc.PublishTaskControlDraft(ctx, owner, binding, publish)
	if e != nil || repeated.ID != publication.ID {
		t.Fatal("publish retry", e)
	}
	changed := publish
	changed.Reason = "changed"
	if _, e = svc.PublishTaskControlDraft(ctx, owner, binding, changed); !isCompanyError(e, ErrorConflict) {
		t.Fatal("publish changed retry", e)
	}
	beforeMode, e := svc.ExchangeTaskControlSession(ctx, assertion("runtime", uuid.New(), false))
	if e != nil {
		t.Fatal(e)
	}
	modeID := uuid.New()
	mode := TaskControlModeInput{1, 1, modeID, "managed", publication.ID, assertion("mode", modeID, true)}
	switched, e := svc.SetTaskControlMode(ctx, owner, binding, mode)
	if e != nil || switched.Scope.ModeEpoch != 2 {
		t.Fatal("managed", e)
	}
	if _, e = svc.TaskControlBootstrap(ctx, beforeMode.AccessToken); e == nil {
		t.Fatal("old session after mode")
	}
	runtimeSession, e := svc.ExchangeTaskControlSession(ctx, assertion("runtime", uuid.New(), false))
	if e != nil {
		t.Fatal(e)
	}
	boot, e := svc.TaskControlBootstrap(ctx, runtimeSession.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(boot)
	var envelope struct {
		Mode        string `json:"mode"`
		Publication struct {
			Checksum     string           `json:"checksum"`
			Scope        TaskControlScope `json:"scope"`
			RuntimeRules []tc.LegacyRule  `json:"runtimeRules"`
		} `json:"publication"`
	}
	_ = json.Unmarshal(encoded, &envelope)
	if envelope.Mode != "managed" || envelope.Publication.Checksum != fx.Checksum || envelope.Publication.Scope.ModeEpoch != 2 || len(envelope.Publication.RuntimeRules) != 2 {
		t.Fatalf("bootstrap %s", encoded)
	}
	// Existing lifecycle below exercises a standalone connection; return explicitly.
	backID := uuid.New()
	_, e = svc.SetTaskControlMode(ctx, owner, binding, TaskControlModeInput{1, 2, backID, "standalone", publication.ID, assertion("mode", backID, true)})
	if e != nil {
		t.Fatal(e)
	}
	intent.Scope.ModeEpoch = 3
	raw := assertion("runtime", uuid.New(), false)
	session, e := svc.ExchangeTaskControlSession(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ExchangeTaskControlSession(ctx, raw); e == nil {
		t.Fatal("replay")
	}
	concurrent := assertion("runtime", uuid.New(), false)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := svc.ExchangeTaskControlSession(ctx, concurrent); results <- e }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent successes %d", success)
	}
	before := core.calls
	if _, actor, e := svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); e != nil || actor != "7" {
		t.Fatalf("runtime %s %v", actor, e)
	}
	if core.calls != before {
		t.Fatal("Core called per read")
	}
	svc = &Service{pool: pool, now: func() time.Time { return now }, taskControlCore: core, taskControlKeys: map[string]ed25519.PublicKey{"key": pub}}
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); e != nil {
		t.Fatal(e)
	}
	core.fail = true
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); e != nil {
		t.Fatal("fresh snapshot outage", e)
	}
	if _, e = pool.Exec(ctx, `DELETE FROM task_control_installation_snapshots WHERE company_id=$1`, company); e != nil {
		t.Fatal(e)
	}
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); !isCompanyError(e, ErrorUpstream) {
		t.Fatalf("outage: %v", e)
	}
	st, e := svc.GetTaskControlInstallation(ctx, owner, "123")
	if e != nil || st.State != "unknown" {
		t.Fatal("outage became uninstall", st, e)
	}
	core.fail = false
	core.status.State = "notInstalled"
	core.status.Revision = 2
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("uninstall: %v", e)
	}
	core.status.State = "installed"
	core.status.Generation = 2
	core.status.Revision = 3
	in.IntentID = uuid.New()
	in.CoreAssertion = assertion("link", in.IntentID, true)
	linked, e := svc.LinkTaskControl(ctx, owner, in)
	if e != nil {
		t.Fatal(e)
	}
	if linked.Scope.BindingID == intent.Scope.BindingID {
		t.Fatal("reinstall reused binding")
	}
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); e == nil {
		t.Fatal("old generation survived")
	}
	session, e = svc.ExchangeTaskControlSession(ctx, assertion("runtime", uuid.New(), false))
	if e != nil {
		t.Fatal(e)
	}
	previousNow := now
	now = now.Add(5*time.Minute + time.Second)
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); !isCompanyError(e, ErrorUnauthenticated) {
		t.Fatal("expired runtime session accepted", e)
	}
	now = previousNow
	requestID := uuid.New()
	rev := TaskControlRevokeInput{RequestID: requestID, ExpectedBindingRevision: 1, ExpectedModeEpoch: 1, CoreAssertion: assertion("revoke", requestID, true)}
	revoked, e := svc.RevokeTaskControl(ctx, owner, linked.Scope.BindingID, rev)
	if e != nil || revoked.State != "revoked" {
		t.Fatal(revoked, e)
	}
	if _, e = svc.RevokeTaskControl(ctx, owner, linked.Scope.BindingID, rev); e != nil {
		t.Fatal("revoke repeat", e)
	}
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); e == nil {
		t.Fatal("revoked runtime active")
	}
	var companies, users int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM companies`).Scan(&companies); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE company_id=$1`, company).Scan(&users); e != nil {
		t.Fatal(e)
	}
	if companies != 1 || users != 2 {
		t.Fatalf("provisioning %d/%d", companies, users)
	}
}
