package application

import (
	"testing"

	"github.com/google/uuid"
	domainrecurrence "github.com/sk1fy/team-os-backend/services/tasks/internal/domain/recurrence"
)

func TestNormalizeRecurrence(t *testing.T) {
	tests := []struct {
		name string
		rule *RecurrenceRule
		ok   bool
	}{
		{name: "nil", rule: nil, ok: true},
		{name: "daily", rule: &RecurrenceRule{Frequency: domainrecurrence.FrequencyDaily, Interval: 1}, ok: true},
		{name: "deduplicates weekdays", rule: &RecurrenceRule{Frequency: domainrecurrence.FrequencyWeekly, Interval: 1, Weekdays: []int{1, 1, 5}}, ok: true},
		{name: "zero interval", rule: &RecurrenceRule{Frequency: domainrecurrence.FrequencyDaily}, ok: false},
		{name: "unknown frequency", rule: &RecurrenceRule{Frequency: "yearly", Interval: 1}, ok: false},
		{name: "invalid weekday", rule: &RecurrenceRule{Frequency: domainrecurrence.FrequencyWeekly, Interval: 1, Weekdays: []int{7}}, ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := normalizeRecurrence(test.rule)
			if (err == nil) != test.ok {
				t.Fatalf("normalizeRecurrence() error = %v, ok=%v", err, test.ok)
			}
			if test.name == "deduplicates weekdays" && len(test.rule.Weekdays) != 2 {
				t.Fatalf("weekdays не дедуплицированы: %v", test.rule.Weekdays)
			}
		})
	}
}

func TestNormalizeUUIDsAndAddedUUIDs(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	got := normalizeUUIDs([]uuid.UUID{first, first, second})
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("normalizeUUIDs() = %v", got)
	}
	added := addedUUIDs([]uuid.UUID{first}, []uuid.UUID{first, second})
	if len(added) != 1 || added[0] != second {
		t.Fatalf("addedUUIDs() = %v", added)
	}
}
