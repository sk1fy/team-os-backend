package application

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestDistributionPagePermissionsParallelScopedAndFailClosed(t *testing.T) {
	rule, other := uuid.New(), uuid.New()
	rows := []db.DistributionQueue{
		{RuleID: rule, AccountID: "1", LeadID: "10"},
		{RuleID: rule, AccountID: "1", LeadID: "10"},
		{RuleID: other, AccountID: "1", LeadID: "10"},
		{RuleID: rule, AccountID: "2", LeadID: "10"},
		{RuleID: rule, AccountID: "1", LeadID: "11"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var calls atomic.Int32
	check := func(ctx context.Context, row db.DistributionQueue) (bool, error) {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		if row.RuleID == other {
			return false, nil
		}
		if row.AccountID == "2" {
			return true, errors.New("permission source unavailable")
		}
		return true, nil
	}
	done := make(chan []distributionPermissionResult, 1)
	go func() { done <- distributionQueuePermissions(ctx, rows, check) }()
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("four checks did not start concurrently")
		}
	}
	close(release)
	results := <-done
	if calls.Load() != 4 || len(results) != len(rows) || !results[0].visible || !results[1].visible || results[2].visible || results[3].visible || results[3].err == nil || !results[4].visible {
		t.Fatalf("calls=%d results=%+v", calls.Load(), results)
	}
	// A later request must ask again, even for an identical lead.
	results = distributionQueuePermissions(ctx, rows[:1], func(context.Context, db.DistributionQueue) (bool, error) { return false, nil })
	if results[0].visible {
		t.Fatal("permission reused across requests")
	}
}

func TestDistributionPagePermissionsBoundWorkersAndCancellation(t *testing.T) {
	rows := make([]db.DistributionQueue, 20)
	for i := range rows {
		rows[i].RuleID = uuid.New()
	}
	ctx, cancel := context.WithCancel(context.Background())
	var active, peak atomic.Int32
	entered := make(chan struct{}, 4)
	done := make(chan []distributionPermissionResult, 1)
	go func() {
		done <- distributionQueuePermissions(ctx, rows, func(ctx context.Context, _ db.DistributionQueue) (bool, error) {
			n := active.Add(1)
			defer active.Add(-1)
			for previous := peak.Load(); n > previous; previous = peak.Load() {
				if peak.CompareAndSwap(previous, n) {
					break
				}
			}
			entered <- struct{}{}
			<-ctx.Done()
			return false, ctx.Err()
		})
	}()
	for i := 0; i < 4; i++ {
		<-entered
	}
	cancel()
	for _, result := range <-done {
		if result.visible || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("cancelled result %+v", result)
		}
	}
	if peak.Load() != 4 || active.Load() != 0 {
		t.Fatalf("peak=%d active=%d", peak.Load(), active.Load())
	}
}

func TestDistributionLeadURLRejectsHostAndPathInjection(t *testing.T) {
	for _, raw := range []string{"http://x.amocrm.ru/leads/detail/10", "https://x.amocrm.ru.evil.test/leads/detail/10", "https://evil.test@x.amocrm.ru/leads/detail/10", "https://x.amocrm.ru:443/leads/detail/10", "https://x.amocrm.ru/leads/detail/11", "https://x.amocrm.ru/leads/detail/10?next=evil", "https://x.amocrm.ru/leads/detail/10#token", "javascript:alert(1)"} {
		if safeDistributionLeadURL(&raw, "10") != nil {
			t.Fatal(raw)
		}
	}
	good := "https://x.amocrm.ru/leads/detail/10"
	if safeDistributionLeadURL(&good, "10") == nil {
		t.Fatal("safeURLrejected")
	}
}
func TestDistributionQueueFiltersAreHalfOpenAndStrict(t *testing.T) {
	now := time.Now()
	next := now.Add(time.Hour)
	if !(DistributionQueueFilter{Tab: "errors", From: &now, To: &next}).valid() {
		t.Fatal("validfilter")
	}
	for _, f := range []DistributionQueueFilter{{Tab: "unknown"}, {From: &now, To: &now}, {From: &next, To: &now}} {
		if f.valid() {
			t.Fatal(f)
		}
	}
}
