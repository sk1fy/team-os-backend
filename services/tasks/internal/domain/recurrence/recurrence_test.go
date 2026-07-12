package recurrence

import (
	"testing"
	"time"
)

func TestNextDueDateDaily(t *testing.T) {
	from := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	next, err := NextDueDate(Rule{Frequency: FrequencyDaily, Interval: 2}, from)
	if err != nil {
		t.Fatal(err)
	}
	want := from.AddDate(0, 0, 2)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

func TestNextDueDateWeeklyWithoutWeekdays(t *testing.T) {
	from := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	next, err := NextDueDate(Rule{Frequency: FrequencyWeekly, Interval: 1}, from)
	if err != nil {
		t.Fatal(err)
	}
	want := from.AddDate(0, 0, 7)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

func TestNextDueDateWeeklyWithWeekdays(t *testing.T) {
	from := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC) // Monday
	next, err := NextDueDate(Rule{Frequency: FrequencyWeekly, Interval: 1, Weekdays: []int{3}}, from)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

func TestNextDueDateMonthly(t *testing.T) {
	from := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	next, err := NextDueDate(Rule{Frequency: FrequencyMonthly, Interval: 1}, from)
	if err != nil {
		t.Fatal(err)
	}
	want := from.AddDate(0, 1, 0)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}
