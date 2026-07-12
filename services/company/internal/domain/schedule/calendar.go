package schedule

import (
	"fmt"
	"time"
)

type ShiftType string

const (
	ShiftWork     ShiftType = "work"
	ShiftOff      ShiftType = "off"
	ShiftVacation ShiftType = "vacation"
	ShiftSick     ShiftType = "sick"
	ShiftTrip     ShiftType = "trip"
)

type Template struct {
	Type       string `json:"type"`
	Days       []int  `json:"days,omitempty"`
	On         int    `json:"on,omitempty"`
	Off        int    `json:"off,omitempty"`
	Start      string `json:"start"`
	End        string `json:"end"`
	CycleStart string `json:"cycleStart,omitempty"`
}

type Exception struct {
	Type  ShiftType
	Start string
	End   string
	Note  string
}

type DayState struct {
	Type  ShiftType
	Start string
	End   string
	Note  string
}

func ValidateTemplate(template Template) error {
	if err := validateTimeRange(template.Start, template.End); err != nil {
		return err
	}
	switch template.Type {
	case "week":
		if len(template.Days) == 0 {
			return fmt.Errorf("укажите хотя бы один рабочий день")
		}
		seen := make(map[int]struct{}, len(template.Days))
		for _, day := range template.Days {
			if day < 0 || day > 6 {
				return fmt.Errorf("день недели должен быть от 0 до 6")
			}
			if _, exists := seen[day]; exists {
				return fmt.Errorf("рабочие дни не должны повторяться")
			}
			seen[day] = struct{}{}
		}
	case "cycle":
		if template.On < 1 || template.Off < 1 {
			return fmt.Errorf("длины рабочих и выходных частей цикла должны быть не меньше 1")
		}
		if _, err := time.Parse(time.DateOnly, template.CycleStart); err != nil {
			return fmt.Errorf("некорректная дата начала цикла")
		}
	default:
		return fmt.Errorf("неизвестный тип шаблона графика")
	}
	return nil
}

func ValidateException(exception Exception) error {
	switch exception.Type {
	case ShiftWork:
		return validateTimeRange(exception.Start, exception.End)
	case ShiftOff, ShiftVacation, ShiftSick, ShiftTrip:
		return nil
	default:
		return fmt.Errorf("неизвестный тип смены")
	}
}

func WeekdayIndex(date time.Time) int { return (int(date.Weekday()) + 6) % 7 }

func IsWeekend(date time.Time) bool { return WeekdayIndex(date) >= 5 }

func DaysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func BaseState(template Template, date time.Time) DayState {
	if template.Type == "week" {
		weekday := WeekdayIndex(date)
		for _, day := range template.Days {
			if day == weekday {
				return DayState{Type: ShiftWork, Start: template.Start, End: template.End}
			}
		}
		return DayState{Type: ShiftOff}
	}
	cycleStart, _ := time.Parse(time.DateOnly, template.CycleStart)
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	days := int(date.Sub(cycleStart).Hours() / 24)
	cycle := template.On + template.Off
	index := days % cycle
	if index < 0 {
		index += cycle
	}
	if index < template.On {
		return DayState{Type: ShiftWork, Start: template.Start, End: template.End}
	}
	return DayState{Type: ShiftOff}
}

func State(template Template, exception *Exception, date time.Time) DayState {
	if exception == nil {
		return BaseState(template, date)
	}
	return DayState{Type: exception.Type, Start: exception.Start, End: exception.End, Note: exception.Note}
}

func ShiftHours(start, end string) (float64, error) {
	startMinutes, err := parseTime(start)
	if err != nil {
		return 0, err
	}
	endMinutes, err := parseTime(end)
	if err != nil {
		return 0, err
	}
	minutes := endMinutes - startMinutes
	if minutes < 0 {
		minutes += 24 * 60
	}
	return float64(minutes) / 60, nil
}

func validateTimeRange(start, end string) error {
	if _, err := parseTime(start); err != nil {
		return fmt.Errorf("некорректное время начала смены")
	}
	if _, err := parseTime(end); err != nil {
		return fmt.Errorf("некорректное время окончания смены")
	}
	return nil
}

func parseTime(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}
