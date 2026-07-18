package application

import (
	"github.com/google/uuid"
	domainrecurrence "github.com/sk1fy/team-os-backend/services/tasks/internal/domain/recurrence"
)

func normalizeUUIDs(values []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(values))
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cloneUUIDs(values []uuid.UUID) []uuid.UUID {
	result := make([]uuid.UUID, len(values))
	copy(result, values)
	return result
}

func normalizeRecurrence(rule *RecurrenceRule) error {
	if rule == nil {
		return nil
	}
	if rule.Interval < 1 {
		return validation("Интервал повторения должен быть не меньше 1")
	}
	switch rule.Frequency {
	case domainrecurrence.FrequencyDaily, domainrecurrence.FrequencyWeekly, domainrecurrence.FrequencyMonthly:
	default:
		return validation("Некорректная частота повторения")
	}
	seen := make(map[int]struct{}, len(rule.Weekdays))
	weekdays := make([]int, 0, len(rule.Weekdays))
	for _, weekday := range rule.Weekdays {
		if weekday < 0 || weekday > 6 {
			return validation("Некорректный день недели")
		}
		if _, exists := seen[weekday]; exists {
			continue
		}
		seen[weekday] = struct{}{}
		weekdays = append(weekdays, weekday)
	}
	rule.Weekdays = weekdays
	return nil
}
