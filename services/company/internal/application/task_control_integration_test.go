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
	migration, e = os.ReadFile(filepath.Join(filepath.Dir(file), "../../migrations/000034_task_control_runtime_limits.up.sql"))
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
	crmActor := "7"
	assertion := func(purpose string, request uuid.UUID, admin bool) string {
		a := tc.Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: company, InstallationID: install, IntegrationID: integration, AccountID: "123", ActorID: crmActor, Product: tc.Product, Generation: core.status.Generation, Admin: admin, Purpose: purpose, RequestID: request}
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
	// A browser's stale owner role must not elevate an employee. Read overrides
	// expose draft/history without granting validate, publish or mode changes.
	if _, e = pool.Exec(ctx, `INSERT INTO task_control_permissions(company_id,user_id,can_read,can_manage) VALUES($1,$2,true,true)`, company, employeeID); e != nil {
		t.Fatal(e)
	}
	spoofed := employee
	spoofed.Role = "owner"
	caps, e := svc.GetTaskControlCapabilities(ctx, spoofed)
	if e != nil || !caps.Read || caps.Manage || !caps.Operations["history"] || caps.Operations["rollback"] {
		t.Fatal("capabilities", caps, e)
	}
	if _, e = svc.GetTaskControlDraft(ctx, spoofed, binding); e != nil {
		t.Fatal("read draft", e)
	}
	if _, e = svc.TaskControlPublications(ctx, spoofed, binding, publication.ID); e != nil {
		t.Fatal("read publication", e)
	}
	if _, e = svc.ValidateTaskControlDraft(ctx, spoofed, binding); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("read-only validate", e)
	}
	if _, e = svc.PublishTaskControlDraft(ctx, spoofed, binding, publish); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("read-only publish", e)
	}
	for _, op := range []string{"history", "references", "application"} {
		if e = svc.AuthorizeTaskControlOperation(ctx, spoofed, binding, op); e != nil {
			t.Fatal(op, e)
		}
	}
	for _, op := range []string{"rollback", "imports", "sourceRequests", "mode", "forged"} {
		if e = svc.AuthorizeTaskControlOperation(ctx, spoofed, binding, op); !isCompanyError(e, ErrorForbidden) {
			t.Fatal(op, e)
		}
	}
	if e = svc.AuthorizeTaskControlOperation(ctx, spoofed, uuid.New(), "history"); !isCompanyError(e, ErrorNotFound) {
		t.Fatal("foreign binding", e)
	}
	if _, e = pool.Exec(ctx, `UPDATE users SET role='admin' WHERE company_id=$1 AND id=$2`, company, employeeID); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.PublishTaskControlDraft(ctx, spoofed, binding, publish); !isCompanyError(e, ErrorConflict) {
		t.Fatal("another actor replay", e)
	}
	if _, e = svc.GetTaskControlIntent(ctx, spoofed, intentID); !isCompanyError(e, ErrorNotFound) {
		t.Fatal("another actor intent", e)
	}
	if _, e = pool.Exec(ctx, `UPDATE users SET role='employee' WHERE company_id=$1 AND id=$2`, company, employeeID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `DELETE FROM task_control_permissions WHERE company_id=$1 AND user_id=$2`, company, employeeID); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.TaskControlPublications(ctx, spoofed, binding, publication.ID); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("revoked read", e)
	}
	caps, e = svc.GetTaskControlCapabilities(ctx, spoofed)
	if e != nil || caps.Read || caps.Manage {
		t.Fatal("no access capabilities", caps, e)
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
	for _, badProof := range []string{assertion("link", modeID, true), assertion("mode", modeID, false), assertion("mode", uuid.New(), true)} {
		bad := mode
		bad.CoreAssertion = badProof
		if _, e = svc.SetTaskControlMode(ctx, owner, binding, bad); !isCompanyError(e, ErrorForbidden) {
			t.Fatal("invalid mode proof", e)
		}
	}
	proofTime := now
	now = now.Add(61 * time.Second)
	if _, e = svc.SetTaskControlMode(ctx, owner, binding, mode); !isCompanyError(e, ErrorUnauthenticated) {
		t.Fatal("expired mode proof", e)
	}
	now = proofTime
	switched, e := svc.SetTaskControlMode(ctx, owner, binding, mode)
	if e != nil || switched.Scope.ModeEpoch != 2 {
		t.Fatal("managed", e)
	}
	modeAgain, e := svc.SetTaskControlMode(ctx, owner, binding, mode)
	if e != nil || modeAgain.Scope.ModeEpoch != 2 {
		t.Fatal("mode lost response", e)
	}
	changedMode := mode
	changedMode.TargetMode = "standalone"
	if _, e = svc.SetTaskControlMode(ctx, owner, binding, changedMode); !isCompanyError(e, ErrorConflict) {
		t.Fatal("mode key conflict", e)
	}
	if _, e = pool.Exec(ctx, `UPDATE task_control_permissions SET can_manage=false WHERE company_id=$1 AND user_id=$2`, company, ownerID); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.SetTaskControlMode(ctx, owner, binding, mode); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("mode replay after revoke", e)
	}
	if _, e = pool.Exec(ctx, `UPDATE task_control_permissions SET can_manage=true WHERE company_id=$1 AND user_id=$2`, company, ownerID); e != nil {
		t.Fatal(e)
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
		Mode        string    `json:"mode"`
		Schema      int       `json:"runtimeSchemaVersion"`
		Actor       string    `json:"actorId"`
		IssuedAt    time.Time `json:"issuedAt"`
		ValidUntil  time.Time `json:"validUntil"`
		Publication struct {
			Checksum     string           `json:"checksum"`
			Scope        TaskControlScope `json:"scope"`
			RuntimeRules []tc.LegacyRule  `json:"runtimeRules"`
		} `json:"publication"`
	}
	_ = json.Unmarshal(encoded, &envelope)
	if envelope.Schema != 1 || envelope.Actor != "7" || !envelope.IssuedAt.Equal(now) || envelope.ValidUntil.Sub(now) != taskControlSessionTTL || envelope.Mode != "managed" || envelope.Publication.Checksum != fx.Checksum || envelope.Publication.Scope.ModeEpoch != 2 || len(envelope.Publication.RuntimeRules) != 2 {
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
	// Multiple independent tabs coexist; admission is serialized and bounded per actor.
	for i := 2; i < taskControlMaxActorSessions-1; i++ {
		if _, e = svc.ExchangeTaskControlSession(ctx, assertion("runtime", uuid.New(), false)); e != nil {
			t.Fatal("session admission", i, e)
		}
	}
	admitted := make(chan error, 2)
	for i := 0; i < 2; i++ {
		proof := assertion("runtime", uuid.New(), false)
		wg.Add(1)
		go func() { defer wg.Done(); _, err := svc.ExchangeTaskControlSession(ctx, proof); admitted <- err }()
	}
	wg.Wait()
	close(admitted)
	admissionSuccess := 0
	for err := range admitted {
		if err == nil {
			admissionSuccess++
		} else if !isCompanyError(err, ErrorUpstream) {
			t.Fatal("concurrent admission", err)
		}
	}
	if admissionSuccess != 1 {
		t.Fatal("concurrent admission exceeded cap", admissionSuccess)
	}
	crmActor = "8"
	if _, e = svc.ExchangeTaskControlSession(ctx, assertion("runtime", uuid.New(), false)); e != nil {
		t.Fatal("session limit leaked between actors", e)
	}
	crmActor = "7"
	if _, e = svc.ExchangeTaskControlSession(ctx, assertion("runtime", uuid.New(), false)); !isCompanyError(e, ErrorUpstream) {
		t.Fatal("session limit", e)
	}
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); e != nil {
		t.Fatal("another tab revoked existing session", e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO task_control_assertion_uses(key_id,jti,company_id,expires_at) SELECT 'expired-batch',gen_random_uuid(),$1,$2 FROM generate_series(1,1005)`, company, now.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if e = svc.CleanupTaskControlRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	var expiredLeft int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM task_control_assertion_uses WHERE key_id='expired-batch'`).Scan(&expiredLeft); e != nil || expiredLeft != 5 {
		t.Fatal("cleanup exceeded batch", expiredLeft, e)
	}
	var retained int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM task_control_runtime_sessions`).Scan(&retained); e != nil || retained != taskControlMaxActorSessions+1 {
		t.Fatal("live sessions pruned", retained, e)
	}
	canceled, cancelRequest := context.WithCancel(ctx)
	cancelRequest()
	if _, _, e = svc.AuthorizeTaskControlRuntime(canceled, session.AccessToken); !isCompanyError(e, ErrorUpstream) {
		t.Fatal("database outage became denial", e)
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
	if _, e = svc.TaskControlPublications(ctx, owner, binding, publication.ID); e != nil {
		t.Fatal("history during Core outage", e)
	}
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
	connections, e := svc.ListTaskControlConnections(ctx, owner)
	if e != nil || len(connections) != 1 || connections[0].State != "active" {
		t.Fatal("unknown retired binding", connections, e)
	}
	core.fail = false
	core.status.State = "notInstalled"
	core.status.Revision = 2
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, session.AccessToken); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("uninstall: %v", e)
	}
	connections, e = svc.ListTaskControlConnections(ctx, owner)
	if e != nil || len(connections) != 1 || connections[0].State != "revoked" {
		t.Fatal("confirmed uninstall did not retire binding", connections, e)
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
	if e = svc.CleanupTaskControlRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	var sessionsLeft, proofsLeft int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_control_runtime_sessions), (SELECT count(*) FROM task_control_assertion_uses)`).Scan(&sessionsLeft, &proofsLeft); e != nil || sessionsLeft != 0 || proofsLeft != 0 {
		t.Fatal("expiry cleanup", sessionsLeft, proofsLeft, e)
	}
	if _, e = svc.ExchangeTaskControlSession(ctx, raw); e == nil {
		t.Fatal("expired proof resurrected after cleanup")
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
	if _, e = svc.TaskControlPublications(ctx, owner, binding, publication.ID); e != nil {
		t.Fatal("history retained after revoke/reinstall", e)
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
