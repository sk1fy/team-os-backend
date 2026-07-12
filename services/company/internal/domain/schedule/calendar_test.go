package schedule

import (
	"testing"
	"time"
)

var week = Template{Type: "week", Days: []int{0, 1, 2, 3, 4}, Start: "09:00", End: "18:00"}
var cycle = Template{Type: "cycle", On: 2, Off: 2, Start: "09:00", End: "21:00", CycleStart: "2026-07-01"}

func date(value string) time.Time { parsed, _ := time.Parse(time.DateOnly, value); return parsed }

func TestCalendarMath(t *testing.T) {
	if WeekdayIndex(date("2026-07-01")) != 2 {
		t.Fatal("1 июля 2026 должно быть средой")
	}
	if !IsWeekend(date("2026-07-04")) || IsWeekend(date("2026-07-06")) {
		t.Fatal("неверно определён выходной")
	}
	if DaysInMonth(2028, time.February) != 29 || DaysInMonth(2026, time.February) != 28 {
		t.Fatal("неверная длина февраля")
	}
}

func TestBaseState(t *testing.T) {
	tests := []struct {
		day      string
		template Template
		want     ShiftType
	}{
		{"2026-07-06", week, ShiftWork}, {"2026-07-04", week, ShiftOff},
		{"2026-07-01", cycle, ShiftWork}, {"2026-07-03", cycle, ShiftOff},
		{"2026-07-05", cycle, ShiftWork}, {"2026-06-30", cycle, ShiftOff},
		{"2026-06-28", cycle, ShiftWork},
	}
	for _, test := range tests {
		if got := BaseState(test.template, date(test.day)).Type; got != test.want {
			t.Errorf("%s: %s, want %s", test.day, got, test.want)
		}
	}
}

func TestExceptionOverridesTemplate(t *testing.T) {
	exception := &Exception{Type: ShiftSick}
	if got := State(week, exception, date("2026-07-06")).Type; got != ShiftSick {
		t.Fatalf("got %s", got)
	}
}

func TestShiftHours(t *testing.T) {
	for _, test := range []struct {
		start, end string
		want       float64
	}{{"09:00", "18:00", 9}, {"09:30", "18:00", 8.5}, {"21:00", "09:00", 12}} {
		got, err := ShiftHours(test.start, test.end)
		if err != nil || got != test.want {
			t.Errorf("%s-%s = %v, %v", test.start, test.end, got, err)
		}
	}
}
