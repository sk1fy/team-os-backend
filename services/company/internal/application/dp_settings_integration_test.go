//go:build integration

package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
)

type dpCoreStub struct {
	key string
}

func (f *dpCoreStub) ReadLead(context.Context, corebridge.Scope, string) (corebridge.LeadObservation, error) {
	return corebridge.LeadObservation{}, errors.New("unused")
}
func (f *dpCoreStub) Operation(context.Context, corebridge.Scope, uuid.UUID) (corebridge.Operation, error) {
	return corebridge.Operation{}, errors.New("unused")
}
func (f *dpCoreStub) DPCredential(_ context.Context, _ corebridge.Scope, g uuid.UUID) (corebridge.DPCredential, error) {
	return corebridge.DPCredential{Key: f.key, GroupID: g.String(), BindingRevision: 1}, nil
}

// Regression for the widget "choose a group only" Digital Pipeline setup:
// dp_settings issues a dp_ credential, never persists the secret in the
// idempotency result, replays idempotently, denies non-admins and foreign
// groups, and stays configurable for a paused group.
func TestDistributionWidgetDPSettingsCredential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID := uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}})
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	core := &interfaceCore{fakeDistributionCore: &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}}
	const dpKey = "dp_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	s := &Service{pool: pool, now: time.Now, distributionCore: core, deliveryCore: &dpCoreStub{key: dpKey}}
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
	group, e := s.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "DP group", MemberIDs: []uuid.UUID{ownerID}})
	if e != nil {
		t.Fatal(e)
	}
	exec("UPDATE distribution_groups SET active=false WHERE id=$1", group.ID) // paused group stays configurable

	scope := corebridge.Scope{CompanyID: company, BindingID: binding.BindingID, BindingRevision: 1, InstallationID: binding.InstallationID, IntegrationID: binding.IntegrationID, AccountID: "123"}
	requestID := uuid.New()
	in := DistributionWidgetRuntimeInput{Scope: scope, UserID: "42", PrincipalExpiresAt: time.Now().Add(time.Minute), Kind: "dp_settings", GroupID: group.ID, Write: true, RequestID: requestID}
	raw, e := s.DistributionWidgetRuntime(ctx, in)
	if e != nil {
		t.Fatalf("dp_settings %v", e)
	}
	var out struct {
		State     string `json:"state"`
		GroupID   string `json:"groupId"`
		GroupName string `json:"groupName"`
		Key       string `json:"key"`
	}
	if e := json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	if out.State != "connected" || out.GroupID != group.ID.String() || out.GroupName != "DP group" || !strings.HasPrefix(out.Key, "dp_") {
		t.Fatalf("unexpected response %s", raw)
	}
	// The secret must never be persisted in the idempotency result.
	var persisted string
	if e := pool.QueryRow(ctx, "SELECT response FROM distribution_widget_requests WHERE request_id=$1", requestID).Scan(&persisted); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(persisted, dpKey) {
		t.Fatalf("secret persisted in idempotency result: %s", persisted)
	}
	// Replay is idempotent and re-issues the same key without leaking it to disk.
	again, e := s.DistributionWidgetRuntime(ctx, in)
	if e != nil || !bytes.Contains(again, []byte(dpKey)) {
		t.Fatalf("replay must return the key %s %v", again, e)
	}
	if e := pool.QueryRow(ctx, "SELECT response FROM distribution_widget_requests WHERE request_id=$1", requestID).Scan(&persisted); e != nil || strings.Contains(persisted, dpKey) {
		t.Fatalf("replay persisted secret %s %v", persisted, e)
	}
	// Non-admin cannot mint a credential.
	exec("UPDATE users SET role='employee' WHERE id=$1", ownerID)
	in.RequestID = uuid.New()
	if _, e = s.DistributionWidgetRuntime(ctx, in); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("non-admin must be denied: %v", e)
	}
	exec("UPDATE users SET role='owner' WHERE id=$1", ownerID)
	// Foreign group (other company) cannot be bound.
	in.RequestID = uuid.New()
	in.GroupID = uuid.New()
	if _, e = s.DistributionWidgetRuntime(ctx, in); !isCompanyError(e, ErrorNotFound) {
		t.Fatalf("foreign group must 404: %v", e)
	}
}
