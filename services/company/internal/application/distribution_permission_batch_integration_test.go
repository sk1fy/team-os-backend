//go:build integration

package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"sync/atomic"
	"testing"
	"time"
)

type pageBatchCore struct {
	*fakeDistributionCore
	calls, singleCalls atomic.Int32
	delay              time.Duration
	hook               func()
	deny               bool
	ids                []string
	now                func() time.Time
}

func (f *pageBatchCore) Permission(context.Context, corebridge.Scope, corebridge.PermissionInput) (corebridge.Permission, error) {
	f.singleCalls.Add(1)
	return corebridge.Permission{}, errors.New("unexpected single lead call")
}
func (f *pageBatchCore) Permissions(ctx context.Context, _ corebridge.Scope, in corebridge.PermissionInput, ids []string) (map[string]corebridge.Permission, error) {
	f.calls.Add(1)
	f.ids = append([]string(nil), ids...)
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.hook != nil {
		f.hook()
	}
	result := map[string]corebridge.Permission{}
	for _, id := range ids {
		responsible := "2"
		name := "Fixture " + id
		url := "https://test.amocrm.ru/leads/detail/" + id
		result[id] = corebridge.Permission{UserID: in.UserID, CheckedAt: f.now(), CanViewLead: !f.deny, ResponsibleUserID: &responsible, LeadName: &name, LeadURL: &url}
	}
	return result, nil
}
func TestDistributionReadBatch100SlowLeadsAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Now().UTC()
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "00:00", "23:59"}}, "9", "10")
	fx.exec("INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,'42','verified',now())", uuid.New(), fx.company, fx.binding, fx.owner.UserID)
	core := &pageBatchCore{fakeDistributionCore: fx.core.fakeDistributionCore, delay: 2300 * time.Millisecond, now: fx.svc.now}
	fx.svc.distributionCore = core
	var firstQueue uuid.UUID
	for i := 0; i < 100; i++ {
		lead := fmt.Sprint(10 + i)
		entry := fx.entry
		if i > 0 {
			entry = uuid.New()
			fx.exec("INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123',$1,$2,$3,1,$4)", lead, fx.company, fx.binding, entry)
			fx.exec("INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state) VALUES($1,$2,'123',$3,$4,1,1,'20','30',$5,'created_in_stage','checking')", entry, fx.company, lead, fx.binding, uuid.New())
		}
		queue := uuid.New()
		if i == 0 {
			firstQueue = queue
		}
		fx.exec("INSERT INTO distribution_queue(id,company_id,entry_id,rule_id,group_id,account_id,lead_id,state,settled) VALUES($1,$2,$3,$4,$5,'123',$6,'confirmed',true)", queue, fx.company, entry, fx.rule, fx.group, lead)
		job, event := uuid.New(), uuid.New()
		fx.exec("INSERT INTO distribution_observation_jobs(id,company_id,rule_id,execution_epoch,entry_id,event_id,binding_id,binding_revision,account_id,lead_id,state) VALUES($1,$2,$3,1,$4,$5,$6,1,'123',$7,'completed')", job, fx.company, fx.rule, entry, event, fx.binding, lead)
		payload, _ := json.Marshal(DistributionObservation{ID: job, RuleID: fx.rule, EntryID: entry, LeadID: lead})
		fx.exec("INSERT INTO distribution_observations(id,company_id,rule_id,group_id,execution_epoch,entry_id,event_id,binding_id,binding_revision,account_id,lead_id,payload) VALUES($1,$2,$3,$4,1,$5,$6,$7,1,'123',$8,$9)", job, fx.company, fx.rule, fx.group, entry, event, fx.binding, lead, payload)
	}
	raw, err := fx.svc.DistributionSummary(ctx, fx.owner, fx.group)
	var summary struct {
		Available bool `json:"metricsAvailable"`
		Confirmed int  `json:"confirmedToday"`
	}
	if err != nil || json.Unmarshal(raw, &summary) != nil || !summary.Available || summary.Confirmed != 100 || core.calls.Load() != 1 || core.singleCalls.Load() != 0 || len(core.ids) != 100 {
		t.Fatalf("summary %s err=%v calls=%d", raw, err, core.calls.Load())
	}
	raw, err = fx.svc.DistributionObservations(ctx, fx.owner, fx.rule, 100, 0)
	var page struct{ Items []DistributionObservation }
	if err != nil || json.Unmarshal(raw, &page) != nil || len(page.Items) != 100 || core.calls.Load() != 2 {
		t.Fatalf("observations count=%d err=%v calls=%d", len(page.Items), err, core.calls.Load())
	}
	raw, err = fx.svc.DistributionQueueDetail(ctx, fx.owner, firstQueue)
	var detail struct {
		LeadName          *string
		CurrentEmployeeID *uuid.UUID
	}
	if err != nil || json.Unmarshal(raw, &detail) != nil || detail.LeadName == nil || detail.CurrentEmployeeID == nil || core.calls.Load() != 3 || core.singleCalls.Load() != 0 {
		t.Fatalf("detail %s err=%v", raw, err)
	}
	core.delay = 3500 * time.Millisecond
	raw, err = fx.svc.DistributionSummary(ctx, fx.owner, fx.group)
	var timeoutStats struct {
		Available bool   `json:"metricsAvailable"`
		Reason    string `json:"metricsReason"`
		Confirmed *int   `json:"confirmedToday"`
	}
	if err != nil || json.Unmarshal(raw, &timeoutStats) != nil || timeoutStats.Available || timeoutStats.Confirmed != nil || timeoutStats.Reason != "permission_budget_exceeded" {
		t.Fatal("batch timeout disclosed authoritative count", err, string(raw))
	}
	core.delay = 0
	checked := fx.svc.distributionBindingPermissions(ctx, fx.owner, []distributionPermissionTarget{{fx.binding, 1, "123", "10"}, {fx.binding, 1, "123", "10"}, {fx.binding, 1, "123", "11"}})
	if len(core.ids) != 2 || len(checked) != 3 || !checked[0].visible || !checked[1].visible || !checked[2].visible {
		t.Fatal("same binding lead dedup failed", core.ids, checked)
	}
	calls := core.calls.Load()
	checked = fx.svc.distributionBindingPermissions(ctx, fx.owner, []distributionPermissionTarget{{fx.binding, 2, "123", "10"}, {fx.binding, 1, "456", "10"}})
	if checked[0].visible || checked[0].err == nil || checked[1].visible || checked[1].err == nil || core.calls.Load() != calls {
		t.Fatal("account/revision fence crossed", checked)
	}
	core.delay = 0
	core.deny = true
	raw, err = fx.svc.DistributionObservations(ctx, fx.owner, fx.rule, 100, 0)
	page.Items = nil
	if err != nil || json.Unmarshal(raw, &page) != nil || len(page.Items) != 0 {
		t.Fatal("later denial reused permissions", err, string(raw))
	}
	core.deny = false
	core.hook = func() {
		fx.exec("UPDATE distribution_bindings SET mapping_revision=mapping_revision+1 WHERE id=$1", fx.binding)
	}
	if _, err = fx.svc.DistributionObservations(ctx, fx.owner, fx.rule, 100, 0); !isCompanyError(err, ErrorForbidden) {
		t.Fatal("mapping revision changed during batch disclosed", err)
	}
	core.hook = nil
	foreign := fx.owner
	foreign.CompanyID = uuid.New()
	before := core.calls.Load()
	if _, err = fx.svc.DistributionObservations(ctx, foreign, fx.rule, 100, 0); err == nil || core.calls.Load() != before {
		t.Fatal("foreign tenant called CRM", err)
	}
}
