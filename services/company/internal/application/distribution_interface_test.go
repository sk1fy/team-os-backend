package application

import (
	"testing"
	"time"
)

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
