//go:build integration

package application

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"reflect"
	"testing"
	"time"
)

func TestDistributionWidgetSharedRulesReceiptsAndAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, employee := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}, {employee, "employee", "active"}})
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	core := &interfaceCore{fakeDistributionCore: &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}}
	s := &Service{pool: pool, now: time.Now, distributionCore: core}
	binding, e := s.LinkDistributionConnection(ctx, owner, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,'42','verified',now())", uuid.New(), company, binding.BindingID, ownerID)
	exec("UPDATE distribution_bindings SET mapping_revision=1,mapping_ack_revision=1 WHERE id=$1", binding.BindingID)
	group, e := s.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "Shared widget", MemberIDs: []uuid.UUID{ownerID}})
	if e != nil {
		t.Fatal(e)
	}
	rule := uuid.New()
	exec("INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id) VALUES($1,$2,$3,1,'123','20','30',$4)", rule, company, binding.BindingID, group.ID)
	in := DistributionWidgetRuntimeInput{Scope: corebridge.Scope{CompanyID: company, BindingID: binding.BindingID, BindingRevision: 1, InstallationID: binding.InstallationID, IntegrationID: binding.IntegrationID, AccountID: "123"}, UserID: "42", PrincipalExpiresAt: time.Now().Add(time.Minute), Kind: "rules"}
	raw, e := s.DistributionWidgetRuntime(ctx, in)
	if e != nil || !bytes.Contains(raw, []byte(rule.String())) {
		t.Fatalf("shared rules %s %v", raw, e)
	}
	in.Write = true
	in.Kind = "rule"
	in.ID = rule
	in.RequestID = uuid.New()
	in.Payload = json.RawMessage(`{"expectedRevision":1,"active":false,"keepCurrentResponsible":false}`)
	first, e := s.DistributionWidgetRuntime(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	in.PrincipalExpiresAt = time.Now().Add(2 * time.Minute)
	again, e := s.DistributionWidgetRuntime(ctx, in)
	var firstJSON, againJSON any
	_ = json.Unmarshal(first, &firstJSON)
	_ = json.Unmarshal(again, &againJSON)
	if e != nil || !reflect.DeepEqual(firstJSON, againJSON) {
		t.Fatalf("newJWT same request %s %s %v", first, again, e)
	}
	in.Payload = json.RawMessage(`{"expectedRevision":1,"active":false,"keepCurrentResponsible":true}`)
	if _, e = s.DistributionWidgetRuntime(ctx, in); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("body conflict %v", e)
	}
	in.RequestID = uuid.New()
	if _, e = s.DistributionWidgetRuntime(ctx, in); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("revision conflict %v", e)
	}
	in.RequestID = uuid.New()
	in.Payload = json.RawMessage(`{"expectedRevision":2,"active":false,"keepCurrentResponsible":true}`)
	exec("UPDATE users SET role='employee' WHERE id=$1", ownerID)
	if _, e = s.DistributionWidgetRuntime(ctx, in); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("live role denial %v", e)
	}
	exec("UPDATE users SET role='owner' WHERE id=$1", ownerID)
	in.PrincipalExpiresAt = time.Now().Add(-time.Second)
	if _, e = s.DistributionWidgetRuntime(ctx, in); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("expired auth %v", e)
	}
	in.PrincipalExpiresAt = time.Now().Add(time.Minute)
	in.BindingRevision = 2
	if _, e = s.DistributionWidgetRuntime(ctx, in); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("foreign scope %v", e)
	}
	in.BindingRevision = 1
	// Expiry while waiting on an authoritative lock cancels the mutation.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "SELECT company_id FROM distribution_availability_versions WHERE company_id=$1 FOR UPDATE", company); e != nil {
		t.Fatal(e)
	}
	in.PrincipalExpiresAt = time.Now().Add(150 * time.Millisecond)
	in.RequestID = uuid.New()
	in.Payload = json.RawMessage(`{"expectedRevision":2,"active":false,"keepCurrentResponsible":true}`)
	_, e = s.DistributionWidgetRuntime(ctx, in)
	if e == nil {
		t.Fatal("expired blocked mutation succeeded")
	}
	_ = tx.Rollback(ctx)
	in.PrincipalExpiresAt = time.Now().Add(time.Minute)
	pending, e := s.DistributionWidgetRuntime(ctx, in)
	if e != nil || !bytes.Contains(pending, []byte(`"outcome_unknown"`)) {
		t.Fatalf("pending never reexecutes %s %v", pending, e)
	}
	// A durable pending request from a process lost after dispatch never executes again.
	in.Write = false
	in.Kind = "rules"
	in.ID = uuid.Nil
	in.RequestID = uuid.Nil
	in.Payload = nil
	raw, e = s.DistributionWidgetRuntime(ctx, in)
	if e != nil || !bytes.Contains(raw, []byte(`"revision":2`)) {
		t.Fatalf("TeamOS sees widget revision %s %v", raw, e)
	}
}
