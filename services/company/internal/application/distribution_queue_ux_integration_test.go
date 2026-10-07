//go:build integration

package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestDistributionQueueExpiryPersistsAndConcurrentSweepIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC)
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "51")
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row := fx.queueRow(t)
	if row.WaitingDeadlineAt.Sub(row.CreatedAt) != 72*time.Hour || !row.NextShiftAt.Valid {
		t.Fatalf("persisted deadline/shift %+v", row)
	}
	fx.exec("UPDATE distribution_queue SET next_attempt_at=clock_timestamp()+interval '7 days' WHERE id=$1", row.ID)
	restarted := &Service{pool: fx.svc.pool, now: fx.svc.now}
	if _, e := restarted.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	if got := fx.queueRow(t); got.WaitingDeadlineAt != row.WaitingDeadlineAt || got.Settled {
		t.Fatal("restart extended deadline or expired too early")
	}
	fx.exec("UPDATE distribution_queue SET waiting_deadline_at=clock_timestamp()-interval '1 second',lease_token=$2,lease_until=clock_timestamp()+interval '1 minute' WHERE id=$1", row.ID, uuid.New())
	if _, e := restarted.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	if fx.queueRow(t).Settled {
		t.Fatal("sweep overwrote live worker lease")
	}
	fx.exec("UPDATE distribution_queue SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", row.ID)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := db.New(fx.svc.pool).ExpireUndispatchedDistributionQueue(ctx)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	got := fx.queueRow(t)
	if got.State != "cancelled" || got.Reason != "waiting_expired" || !got.Settled || got.OperationID.Valid {
		t.Fatalf("expiry %+v", got)
	}
	var n int
	if e := fx.svc.pool.QueryRow(ctx, "SELECT count(*) FROM distribution_queue_history WHERE queue_id=$1 AND reason='waiting_expired'", row.ID).Scan(&n); e != nil || n != 1 {
		t.Fatalf("duplicate history %d %v", n, e)
	}
	if fx.core.calls != 0 {
		t.Fatal("expired waiting lead assigned")
	}
}

func TestDistributionQueueExpiredInflightKeepsGuardAndConfirmedOutcomeWins(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "52")
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row := fx.queueRow(t)
	if fx.core.calls != 1 || !row.OperationID.Valid {
		t.Fatal("assignment not frozen")
	}
	fx.exec("UPDATE distribution_queue SET waiting_deadline_at=$2,next_attempt_at=clock_timestamp() WHERE id=$1", row.ID, clock.Add(-time.Second))
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row = fx.queueRow(t)
	if row.Settled || !row.CancelRequested || !row.OperationID.Valid {
		t.Fatalf("inflight guard released %+v", row)
	}
	var claims int
	if e := fx.svc.pool.QueryRow(ctx, "SELECT count(*) FROM distribution_lead_claims WHERE queue_id=$1", row.ID).Scan(&claims); e != nil || claims != 1 {
		t.Fatalf("lead claim lost %d %v", claims, e)
	}
	snapshot := fx.core.assignment.Command.ExpectedSnapshot
	snapshot.ResponsibleUserID = "2"
	outcome := "assigned"
	fx.core.operation.State = "succeeded"
	fx.core.operation.ExternalEffectState = "settled"
	fx.core.operation.ResultVersion = 2
	fx.core.operation.Outcome = &outcome
	fx.core.operation.ConfirmedSnapshot = &snapshot
	fx.core.operation.ResolutionEvidence = corebridge.OperationEvidence{Kind: "response_and_observation", ObservedAt: clock, GuardReleasable: true}
	fx.exec("UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1", row.ID)
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row = fx.queueRow(t)
	if row.State != "confirmed" || !row.Settled {
		t.Fatalf("confirmed result falsely cancelled %+v", row)
	}
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	if fx.core.calls != 1 {
		t.Fatal("duplicate assignment after expiry")
	}
}

func TestDistributionQueuePagesClamp15AndStableOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC)
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "53")
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 30; i++ {
		entry, id := uuid.New(), uuid.New()
		fx.exec("INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123',$1,$2,$3,1,$4)", fmt.Sprint(100+i), fx.company, fx.binding, entry)
		fx.exec("INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state) VALUES($1,$2,'123',$3,$4,1,1,'20','30',$5,'created_in_stage','checking')", entry, fx.company, fmt.Sprint(100+i), fx.binding, uuid.New())
		state := "waiting"
		if i == 1 {
			state = "dispatching"
		}
		if i == 2 {
			state = "uncertain"
		}
		fx.exec("INSERT INTO distribution_queue(id,company_id,entry_id,rule_id,group_id,account_id,lead_id,state,created_at) VALUES($1,$2,$3,$4,$5,'123',$6,$7,'2026-10-06T01:00:00Z')", id, fx.company, entry, fx.rule, fx.group, fmt.Sprint(100+i), state)
	}
	seen := map[uuid.UUID]bool{}
	var prev uuid.UUID
	for page := 0; page < 3; page++ {
		raw, e := fx.svc.DistributionQueuePage(ctx, fx.owner, DistributionQueueFilter{Tab: "waiting", GroupID: fx.group}, 100, int32(page*15))
		if e != nil {
			t.Fatal(e)
		}
		var v struct {
			Items   []struct{ ID uuid.UUID }
			Limit   int
			HasMore bool
		}
		if json.Unmarshal(raw, &v) != nil {
			t.Fatal(string(raw))
		}
		want := 15
		if page == 2 {
			want = 1
		}
		if len(v.Items) != want || v.Limit != 15 || v.HasMore != (page < 2) {
			t.Fatalf("page %d %s", page, raw)
		}
		for _, r := range v.Items {
			if seen[r.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[r.ID] = true
			prev = r.ID
		}
	}
	if len(seen) != 31 || prev == uuid.Nil {
		t.Fatal("records lost")
	}
}

func TestDistributionTimezoneCachedScopedAndManualSettingCannotOverride(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC)
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "54")
	q := db.New(fx.svc.pool)
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: fx.company, ID: fx.binding})
	if e != nil {
		t.Fatal(e)
	}
	if b.AccountTimezone.String != "UTC" {
		t.Fatalf("timezone not imported %+v", b)
	}
	if _, e = fx.svc.SaveDistributionTimezone(ctx, fx.owner, "Asia/Tokyo"); e != nil {
		t.Fatal(e)
	}
	settings, e := fx.svc.distributionBindingSettings(ctx, q, b)
	if e != nil || settings.Timezone != "UTC" {
		t.Fatal("manual legacy override changed account timezone")
	}
	scope := bindingScope(b)
	refs := fx.core.refs
	refs.Timezone = "Asia/Tokyo"
	refs.TimezoneFetchedAt = clock.Add(time.Minute)
	clock = clock.Add(time.Minute)
	if e = fx.svc.cacheDistributionTimezone(ctx, scope, refs); e != nil {
		t.Fatal(e)
	}
	refs.Timezone = "Europe/Moscow"
	refs.TimezoneFetchedAt = clock.Add(-time.Minute)
	if e = fx.svc.cacheDistributionTimezone(ctx, scope, refs); e != nil {
		t.Fatal(e)
	}
	b, e = q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: fx.company, ID: fx.binding})
	if e != nil || b.AccountTimezone.String != "Asia/Tokyo" {
		t.Fatal("stale reference replaced confirmed timezone")
	}
	foreign := scope
	foreign.CompanyID = uuid.New()
	if e = fx.svc.cacheDistributionTimezone(ctx, foreign, refs); e == nil {
		t.Fatal("foreign tenant timezone accepted")
	}
	fx.svc.distributionCore = nil
	clock = clock.Add(6 * time.Minute)
	raw, e := fx.svc.distributionAccountSettings(ctx, fx.owner)
	if e != nil {
		t.Fatal(e)
	}
	var state struct{ Timezone, TimezoneStatus string }
	json.Unmarshal(raw, &state)
	if state.Timezone != "Asia/Tokyo" || state.TimezoneStatus != "cached" {
		t.Fatalf("outage cache %s", raw)
	}
}

func TestDistributionQueueExpiredUnknownAdmissionRetainsIntentUntilCoreTombstone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "56")
	fx.core.assignError = errors.New("lost assignment acknowledgement")
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row := fx.queueRow(t)
	fx.core.operation = corebridge.Operation{} // lost admission acknowledgement, 404 is not proof of no external effect
	fx.exec("UPDATE distribution_queue SET waiting_deadline_at=$2,next_attempt_at=clock_timestamp() WHERE id=$1", row.ID, clock)
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	held := fx.queueRow(t)
	if held.Settled || held.OperationID != row.OperationID || held.IdempotencyKey != row.IdempotencyKey || fx.core.calls != 1 {
		t.Fatal("deadline discarded ambiguous intent or admitted a new assignment")
	}
	clock = clock.Add(11 * time.Minute)
	fx.exec("UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1", row.ID)
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	expired := fx.queueRow(t)
	if !expired.Settled || expired.State != "cancelled" || expired.Reason != "waiting_expired" || fx.core.expireCalls != 1 || fx.core.calls != 1 {
		t.Fatalf("Core tombstone not safely settled: state%s reason%s expire%d calls%d", expired.State, expired.Reason, fx.core.expireCalls, fx.core.calls)
	}
}

func TestDistributionQueueExpiryRunsWithoutCRMOrPageRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	clock := time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC)
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "58")
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row := fx.queueRow(t)
	fx.exec("UPDATE distribution_queue SET waiting_deadline_at=clock_timestamp(),next_attempt_at=clock_timestamp()+interval '7 days' WHERE id=$1", row.ID)
	background := &Service{pool: fx.svc.pool, now: time.Now}
	go background.RunDistributionQueueExpiry(ctx)
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-timeout.C:
			t.Fatal("background expiry required a CRM call or page read")
		case <-poll.C:
			current, e := db.New(fx.svc.pool).LockDistributionQueue(ctx, row.ID)
			if e != nil {
				t.Fatal(e)
			}
			if current.Settled {
				if current.State != "cancelled" || current.Reason != "waiting_expired" {
					t.Fatalf("background result %+v", current)
				}
				return
			}
		}
	}
}

func TestDistributionQueueLeadURLUsesConfirmedDomainAndCurrentPermission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC)
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "59")
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	b, e := db.New(fx.svc.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: fx.company, ID: fx.binding})
	if e != nil {
		t.Fatal(e)
	}
	refs := fx.core.refs
	refs.AccountDomain = "fixture.amocrm.ru"
	if e = fx.svc.cacheDistributionTimezone(ctx, bindingScope(b), refs); e != nil {
		t.Fatal(e)
	}
	fx.exec("INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,'42','verified',now())", uuid.New(), fx.company, fx.binding, fx.owner.UserID)
	permission := &interfaceCore{fakeDistributionCore: fx.core.fakeDistributionCore}
	svc := &Service{pool: fx.svc.pool, now: time.Now, distributionCore: permission}
	page := func() map[string]any {
		t.Helper()
		raw, e := svc.DistributionQueuePage(ctx, fx.owner, DistributionQueueFilter{GroupID: fx.group}, 15, 0)
		if e != nil {
			t.Fatal(e)
		}
		var out struct{ Items []map[string]any }
		if json.Unmarshal(raw, &out) != nil || len(out.Items) != 1 {
			t.Fatal(string(raw))
		}
		return out.Items[0]
	}
	if got := page()["leadUrl"]; got != "https://fixture.amocrm.ru/leads/detail/59" {
		t.Fatalf("confirmed link %v", got)
	}
	permission.deny = true
	hidden := page()
	if hidden["leadUrl"] != nil || hidden["leadId"] != nil {
		t.Fatal("CRM metadata leaked after permission revoked")
	}
	permission.deny = false
	fx.exec("UPDATE distribution_bindings SET account_domain='evil.invalid' WHERE id=$1", fx.binding)
	if page()["leadUrl"] != nil {
		t.Fatal("untrusted host exposed")
	}
	fx.exec("UPDATE distribution_bindings SET account_domain='fixture.amocrm.ru',revision=revision+1 WHERE id=$1", fx.binding)
	if page()["leadUrl"] != nil {
		t.Fatal("stale binding exposed link")
	}
}
