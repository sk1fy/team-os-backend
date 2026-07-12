package recurrence

import (
	"errors"
	"time"
)

type Frequency string

const (
	FrequencyDaily   Frequency = "daily"
	FrequencyWeekly  Frequency = "weekly"
	FrequencyMonthly Frequency = "monthly"
)

type Rule struct {
	Frequency Frequency
	Interval  int
	Weekdays  []int
}

func NextDueDate(rule Rule, from time.Time) (time.Time, error) {
	if rule.Interval < 1 {
		return time.Time{}, errors.New("интервал повторения должен быть не меньше 1")
	}
	switch rule.Frequency {
	case FrequencyDaily:
		return from.AddDate(0, 0, rule.Interval), nil
	case FrequencyWeekly:
		return nextWeekly(rule, from)
	case FrequencyMonthly:
		return from.AddDate(0, rule.Interval, 0), nil
	default:
		return time.Time{}, errors.New("неподдерживаемая частота повторения")
	}
}

func nextWeekly(rule Rule, from time.Time) (time.Time, error) {
	if len(rule.Weekdays) == 0 {
		return from.AddDate(0, 0, 7*rule.Interval), nil
	}
	allowed := make(map[time.Weekday]struct{}, len(rule.Weekdays))
	for _, weekday := range rule.Weekdays {
		if weekday < 0 || weekday > 6 {
			return time.Time{}, errors.New("некорректный день недели")
		}
		allowed[time.Weekday(weekday)] = struct{}{}
	}
	candidate := from.AddDate(0, 0, 1)
	for range 366 * rule.Interval {
		if _, ok := allowed[candidate.Weekday()]; ok {
			weeksSince := weeksBetween(from, candidate)
			if weeksSince%rule.Interval == 0 {
				return candidate, nil
			}
		}
		candidate = candidate.AddDate(0, 0, 1)
	}
	return time.Time{}, errors.New("не удалось вычислить следующую дату повторения")
}

func weeksBetween(from, to time.Time) int {
	fromMonday := startOfWeek(from)
	toMonday := startOfWeek(to)
	days := int(toMonday.Sub(fromMonday).Hours() / 24)
	return days / 7
}

func startOfWeek(value time.Time) time.Time {
	weekday := int(value.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return time.Date(value.Year(), value.Month(), value.Day()-(weekday-1), 0, 0, 0, 0, value.Location())
}