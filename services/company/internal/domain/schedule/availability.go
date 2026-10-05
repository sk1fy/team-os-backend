package schedule

import "time"

// Availability evaluates local calendar intervals. Search is deliberately bounded
// to the next 35 local calendar days; a missing next shift is not
// interpreted as permanent availability.
type Availability struct {
	Available   bool
	Reason      string
	Until, Next *time.Time
}

func AvailableAt(template *Template, exceptions map[string]Exception, timezone string, now time.Time) Availability {
	loc, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" || timezone == "Local" {
		return Availability{Reason: "timezone_required"}
	}
	if template == nil {
		return Availability{Reason: "schedule_missing"}
	}
	if ValidateTemplate(*template) != nil || template.Start == template.End ||
		(template.Type == "cycle" && template.On > int(^uint(0)>>1)-template.Off) {
		return Availability{Reason: "schedule_invalid"}
	}
	today := now.In(loc)
	date := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	reason := "outside_shift"
	if ex, ok := exceptions[date.Format(time.DateOnly)]; ok {
		if ValidateException(ex) != nil || (ex.Type == ShiftWork && ex.Start == ex.End) {
			return Availability{Reason: "schedule_invalid"}
		}
		if ex.Type != ShiftWork {
			reason = string(ex.Type)
		}
	}
	for offset := -1; offset <= 35; offset++ {
		day := date.AddDate(0, 0, offset)
		key := day.Format(time.DateOnly)
		var ex *Exception
		if value, ok := exceptions[key]; ok {
			ex = &value
		}
		if ex != nil && (ValidateException(*ex) != nil || (ex.Type == ShiftWork && ex.Start == ex.End)) {
			if offset <= 0 {
				reason = "schedule_invalid"
			}
			continue
		}
		state := availabilityState(*template, ex, day)
		if state.Type != ShiftWork {
			continue
		}
		start, ok1 := localBoundary(day, state.Start, loc)
		endDay := day
		if state.End <= state.Start {
			endDay = day.AddDate(0, 0, 1)
		}
		end, ok2 := localBoundary(endDay, state.End, loc)
		if !ok1 || !ok2 || state.Start == state.End {
			if offset <= 0 {
				reason = "schedule_invalid"
			}
			continue
		}
		// Explicit nonwork exception cuts the overnight tail at midnight.
		if endDay != day {
			if next, ok := exceptions[endDay.Format(time.DateOnly)]; ok && next.Type != ShiftWork {
				if ValidateException(next) != nil {
					if offset <= 0 {
						reason = "schedule_invalid"
					}
					continue
				}
				boundary, valid := localBoundary(endDay, "00:00", loc)
				if !valid {
					if offset <= 0 {
						reason = "schedule_invalid"
					}
					continue
				}
				end = boundary
			}
		}
		if !end.After(start) {
			continue
		}
		if !now.Before(start) && now.Before(end) {
			return Availability{Available: true, Reason: "available", Until: &end}
		}
		if start.After(now) {
			return Availability{Reason: reason, Next: &start}
		}
	}
	if reason == "outside_shift" {
		reason = "shift_not_found_within_horizon"
	}
	return Availability{Reason: reason}
}

// Cycle indexing uses civil dates represented at UTC midnight, never elapsed
// local hours or time.Sub (which saturates for dates more than 292 years apart).
func availabilityState(template Template, exception *Exception, date time.Time) DayState {
	if exception != nil || template.Type != "cycle" {
		return State(template, exception, date)
	}
	start, _ := time.Parse(time.DateOnly, template.CycleStart)
	days := date.Unix()/86400 - start.Unix()/86400
	cycle := int64(template.On) + int64(template.Off)
	index := days % cycle
	if index < 0 {
		index += cycle
	}
	if index < int64(template.On) {
		return DayState{Type: ShiftWork, Start: template.Start, End: template.End}
	}
	return DayState{Type: ShiftOff}
}

// Detect both missing wall times and folds. Enumerating offsets near the date
// supports non-hour transitions without relying on time.Date's fold selection.
func localBoundary(date time.Time, clock string, loc *time.Location) (time.Time, bool) {
	minutes, e := parseTime(clock)
	if e != nil {
		return time.Time{}, false
	}
	wall := time.Date(date.Year(), date.Month(), date.Day(), minutes/60, minutes%60, 0, 0, time.UTC)
	offsets := map[int]bool{}
	for h := -48; h <= 48; h++ {
		_, offset := wall.Add(time.Duration(h) * time.Hour).In(loc).Zone()
		offsets[offset] = true
	}
	var result time.Time
	count := 0
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(loc)
		if local.Year() == date.Year() && local.Month() == date.Month() && local.Day() == date.Day() && local.Hour() == minutes/60 && local.Minute() == minutes%60 {
			result = candidate
			count++
		}
	}
	return result, count == 1
}
