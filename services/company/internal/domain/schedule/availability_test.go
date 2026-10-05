package schedule

import (
	"testing"
	"time"
)

func TestAvailabilityCalendar(t *testing.T) {
	cases := []struct {
		name, zone, at, start, end string
		days                       []int
		exceptions                 map[string]Exception
		want                       bool
		reason                     string
	}{
		{"start", "Europe/Moscow", "2026-10-05T06:00:00Z", "09:00", "18:00", []int{0}, nil, true, "available"},
		{"end", "Europe/Moscow", "2026-10-05T15:00:00Z", "09:00", "18:00", []int{0}, nil, false, "outside_shift"},
		{"tail baseoff", "Europe/Moscow", "2026-10-06T02:00:00Z", "22:00", "06:00", []int{0}, nil, true, "available"},
		{"tail sick", "Europe/Moscow", "2026-10-06T02:00:00Z", "22:00", "06:00", []int{0}, map[string]Exception{"2026-10-06": {Type: ShiftSick}}, false, "sick"},
		{"zero", "Europe/Moscow", "2026-10-05T09:00:00Z", "09:00", "09:00", []int{0}, nil, false, "schedule_invalid"},
		{"gap", "America/New_York", "2026-03-08T07:15:00Z", "02:00", "04:00", []int{6}, nil, false, "schedule_invalid"},
		{"fold", "America/New_York", "2026-11-01T06:15:00Z", "01:00", "03:00", []int{6}, nil, false, "schedule_invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, c.at)
			tpl := Template{Type: "week", Days: c.days, Start: c.start, End: c.end}
			got := AvailableAt(&tpl, c.exceptions, c.zone, now)
			if got.Available != c.want || got.Reason != c.reason {
				t.Fatalf("got%+v", got)
			}
		})
	}
}
func TestAvailabilityMissingAndCycle(t *testing.T) {
	now := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC)
	if got := AvailableAt(nil, nil, "UTC", now); got.Reason != "schedule_missing" {
		t.Fatal(got)
	}
	tpl := Template{Type: "cycle", On: 1, Off: 1, CycleStart: "2026-10-05", Start: "22:00", End: "06:00"}
	if !AvailableAt(&tpl, nil, "UTC", now).Available {
		t.Fatal("cycle overnight")
	}
}

func instant(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAvailabilityExceptionAndCalendarBoundaries(t *testing.T) {
	tpl := Template{Type: "week", Days: []int{0}, Start: "22:00", End: "06:00"}
	cases := []struct {
		name, at, until string
		ex              map[string]Exception
		available       bool
		reason          string
	}{
		{"base off tail exclusive end", "2026-10-06T03:00:00Z", "", nil, false, "outside_shift"},
		{"tail last minute", "2026-10-06T02:59:59Z", "2026-10-06T03:00:00Z", nil, true, "available"},
		{"explicit off cuts midnight", "2026-10-05T21:00:00Z", "", map[string]Exception{"2026-10-06": {Type: ShiftOff}}, false, "off"},
		{"tail cutoff announced on previous day", "2026-10-05T20:59:59Z", "2026-10-05T21:00:00Z", map[string]Exception{"2026-10-06": {Type: ShiftVacation}}, true, "available"},
		{"work override base off", "2026-10-06T08:00:00Z", "2026-10-06T09:00:00Z", map[string]Exception{"2026-10-06": {Type: ShiftWork, Start: "10:00", End: "12:00"}}, true, "available"},
		{"malformed work", "2026-10-06T02:00:00Z", "", map[string]Exception{"2026-10-06": {Type: ShiftWork, Start: "oops", End: "12:00"}}, false, "schedule_invalid"},
		{"unknown exception", "2026-10-06T02:00:00Z", "", map[string]Exception{"2026-10-06": {Type: "unknown"}}, false, "schedule_invalid"},
		{"zero work", "2026-10-06T02:00:00Z", "", map[string]Exception{"2026-10-06": {Type: ShiftWork, Start: "12:00", End: "12:00"}}, false, "schedule_invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AvailableAt(&tpl, c.ex, "Europe/Moscow", instant(t, c.at))
			if got.Available != c.available || got.Reason != c.reason {
				t.Fatalf("got %+v", got)
			}
			if c.until != "" && (got.Until == nil || !got.Until.Equal(instant(t, c.until))) {
				t.Fatalf("wrong until: %+v", got)
			}
		})
	}
}

func TestAvailabilityDSTAndLocalCycle(t *testing.T) {
	for _, c := range []struct {
		name, zone, at, start, end string
		days                       []int
		available                  bool
		until                      string
	}{
		{"half hour gap", "Australia/Lord_Howe", "2026-10-03T15:40:00Z", "02:15", "04:00", []int{6}, false, ""},
		{"half hour fold", "Australia/Lord_Howe", "2026-04-04T15:10:00Z", "01:45", "03:00", []int{6}, false, ""},
		{"valid interval spanning spring change", "America/New_York", "2026-03-08T07:30:00Z", "01:00", "04:00", []int{6}, true, "2026-03-08T08:00:00Z"},
		{"overnight ambiguous end", "America/New_York", "2026-11-01T04:30:00Z", "22:00", "01:30", []int{5}, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			tpl := Template{Type: "week", Days: c.days, Start: c.start, End: c.end}
			got := AvailableAt(&tpl, nil, c.zone, instant(t, c.at))
			if got.Available != c.available {
				t.Fatalf("got %+v", got)
			}
			if !c.available && got.Reason != "schedule_invalid" {
				t.Fatalf("got %+v", got)
			}
			if c.until != "" && (got.Until == nil || !got.Until.Equal(instant(t, c.until))) {
				t.Fatalf("got %+v", got)
			}
		})
	}
	// DST Sunday has 23 hours; Monday must still be the next civil day.
	tpl := Template{Type: "cycle", On: 1, Off: 1, CycleStart: "2026-03-08", Start: "09:00", End: "18:00"}
	got := AvailableAt(&tpl, nil, "America/New_York", instant(t, "2026-03-09T14:00:00Z"))
	if got.Available || got.Next == nil || !got.Next.Equal(instant(t, "2026-03-10T13:00:00Z")) {
		t.Fatalf("civil cycle: %+v", got)
	}
	// Gregorian range is wider than time.Duration: year 0001 must not saturate.
	tpl.CycleStart = "0001-01-01"
	got = AvailableAt(&tpl, nil, "UTC", instant(t, "2026-03-09T14:00:00Z"))
	start, _ := time.Parse(time.DateOnly, tpl.CycleStart)
	day := instant(t, "2026-03-09T00:00:00Z")
	want := (day.Unix()/86400-start.Unix()/86400)%2 == 0
	if got.Available != want {
		t.Fatalf("wide calendar cycle got %+v want %v", got, want)
	}
}

func TestAvailabilityInvalidAndBoundedSearch(t *testing.T) {
	now := instant(t, "2026-10-05T08:00:00Z")
	tpl := Template{Type: "week", Days: []int{0}, Start: "09:00", End: "18:00"}
	for _, zone := range []string{"", "Local", "Mars/Unknown"} {
		if got := AvailableAt(&tpl, nil, zone, now); got.Available || got.Reason != "timezone_required" {
			t.Fatalf("zone %q: %+v", zone, got)
		}
	}
	overflow := Template{Type: "cycle", On: int(^uint(0) >> 1), Off: 1, CycleStart: "2026-10-05", Start: "09:00", End: "18:00"}
	if got := AvailableAt(&overflow, nil, "UTC", now); got.Reason != "schedule_invalid" {
		t.Fatal(got)
	}
	ex := map[string]Exception{}
	for i := 0; i <= 35; i++ {
		ex[now.AddDate(0, 0, i).Format(time.DateOnly)] = Exception{Type: ShiftOff}
	}
	// An explicit working override outside the bounded window must not be returned.
	ex[now.AddDate(0, 0, 36).Format(time.DateOnly)] = Exception{Type: ShiftWork, Start: "09:00", End: "18:00"}
	got := AvailableAt(&tpl, ex, "UTC", now)
	if got.Available || got.Next != nil {
		t.Fatalf("unbounded next shift: %+v", got)
	}
	delete(ex, "2026-10-05") // today's shift is valid and starts one hour later.
	got = AvailableAt(&tpl, ex, "UTC", now)
	if got.Next == nil || !got.Next.Equal(instant(t, "2026-10-05T09:00:00Z")) {
		t.Fatalf("today: %+v", got)
	}
	// Future work overrides participate even on base-off days.
	now = instant(t, "2026-10-05T18:00:00Z")
	ex["2026-10-06"] = Exception{Type: ShiftWork, Start: "11:00", End: "12:00"}
	got = AvailableAt(&tpl, ex, "UTC", now)
	if got.Next == nil || !got.Next.Equal(instant(t, "2026-10-06T11:00:00Z")) {
		t.Fatalf("override next: %+v", got)
	}
}

func TestAvailabilityExplicitNonworkKindsAndHorizon(t *testing.T) {
	tpl := Template{Type: "week", Days: []int{0}, Start: "22:00", End: "06:00"}
	now := instant(t, "2026-10-06T02:00:00Z")
	for _, kind := range []ShiftType{ShiftOff, ShiftVacation, ShiftSick, ShiftTrip} {
		got := AvailableAt(&tpl, map[string]Exception{"2026-10-06": {Type: kind}}, "UTC", now)
		if got.Available || got.Reason != string(kind) {
			t.Fatalf("%s: %+v", kind, got)
		}
	}
	tpl = Template{Type: "cycle", On: 1, Off: 80, CycleStart: "2026-10-04", Start: "09:00", End: "18:00"}
	now = instant(t, "2026-10-05T08:00:00Z")
	got := AvailableAt(&tpl, nil, "UTC", now)
	if got.Next != nil || got.Reason != "shift_not_found_within_horizon" {
		t.Fatalf("empty horizon: %+v", got)
	}
	limit := now.AddDate(0, 0, 35).Format(time.DateOnly)
	got = AvailableAt(&tpl, map[string]Exception{limit: {Type: ShiftWork, Start: "11:00", End: "12:00"}}, "UTC", now)
	want := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)
	if got.Next == nil || !got.Next.Equal(want) {
		t.Fatalf("inclusive horizon boundary: %+v", got)
	}
}
