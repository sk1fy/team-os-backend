package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	domainschedule "github.com/sk1fy/team-os-backend/services/company/internal/domain/schedule"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) ListSchedules(ctx context.Context, actor Actor) ([]UserSchedule, error) {
	rows, err := db.New(s.pool).ListSchedules(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить графики", err)
	}
	result := make([]UserSchedule, len(rows))
	for index, row := range rows {
		result[index], err = scheduleFromDB(row)
		if err != nil {
			return nil, internal("Не удалось прочитать шаблон графика", err)
		}
	}
	return result, nil
}

func (s *Service) SaveSchedule(ctx context.Context, actor Actor, userID uuid.UUID, template ScheduleTemplate) (UserSchedule, error) {
	if err := requireAdministrator(actor); err != nil {
		return UserSchedule{}, err
	}
	domain := scheduleToDomain(template)
	if err := domainschedule.ValidateTemplate(domain); err != nil {
		return UserSchedule{}, validation(err.Error())
	}
	queries := db.New(s.pool)
	if _, err := queries.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{CompanyID: actor.CompanyID, ID: userID}); err != nil {
		if isNoRows(err) {
			return UserSchedule{}, notFound("Сотрудник")
		}
		return UserSchedule{}, internal("Не удалось проверить сотрудника", err)
	}
	payload, err := json.Marshal(domain)
	if err != nil {
		return UserSchedule{}, internal("Не удалось сохранить шаблон графика", err)
	}
	row, err := queries.UpsertSchedule(ctx, db.UpsertScheduleParams{CompanyID: actor.CompanyID, UserID: userID, Template: payload})
	if err != nil {
		return UserSchedule{}, internal("Не удалось сохранить график", err)
	}
	return scheduleFromDB(row)
}

func (s *Service) ListShiftExceptions(ctx context.Context, actor Actor, month string) ([]ShiftException, error) {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, validation("Месяц должен быть в формате ГГГГ-ММ")
	}
	rows, err := db.New(s.pool).ListShiftExceptionsByMonth(ctx, db.ListShiftExceptionsByMonthParams{CompanyID: actor.CompanyID, Date: start, Date_2: start.AddDate(0, 1, 0)})
	if err != nil {
		return nil, internal("Не удалось получить изменения графика", err)
	}
	result := make([]ShiftException, len(rows))
	for index, row := range rows {
		result[index] = exceptionFromDB(row)
	}
	return result, nil
}

func (s *Service) SaveShiftExceptions(ctx context.Context, actor Actor, inputs []SaveShiftExceptionInput) ([]ShiftException, error) {
	if err := requireAdministrator(actor); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, internal("Не удалось начать сохранение графика", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	result := make([]ShiftException, 0, len(inputs))
	for _, input := range inputs {
		date, parseErr := time.Parse(time.DateOnly, input.Date)
		if parseErr != nil {
			return nil, validation("Дата должна быть в формате ГГГГ-ММ-ДД")
		}
		if _, queryErr := queries.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{CompanyID: actor.CompanyID, ID: input.UserID}); queryErr != nil {
			if isNoRows(queryErr) {
				return nil, notFound("Сотрудник")
			}
			return nil, internal("Не удалось проверить сотрудника", queryErr)
		}
		domain := domainschedule.Exception{Type: domainschedule.ShiftType(input.Type), Start: dereference(input.Start), End: dereference(input.End), Note: dereference(input.Note)}
		if validationErr := domainschedule.ValidateException(domain); validationErr != nil {
			return nil, validation(validationErr.Error())
		}
		startTime, timeErr := pgTime(input.Start)
		if timeErr != nil {
			return nil, validation("Некорректное время начала смены")
		}
		endTime, timeErr := pgTime(input.End)
		if timeErr != nil {
			return nil, validation("Некорректное время окончания смены")
		}
		row, queryErr := queries.UpsertShiftException(ctx, db.UpsertShiftExceptionParams{ID: uuid.New(), CompanyID: actor.CompanyID, UserID: input.UserID, Date: date, Type: input.Type, StartTime: startTime, EndTime: endTime, Note: pgText(input.Note)})
		if queryErr != nil {
			return nil, internal("Не удалось сохранить изменение графика", queryErr)
		}
		result = append(result, exceptionFromDB(row))
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, internal("Не удалось сохранить изменения графика", err)
	}
	return result, nil
}

func scheduleFromDB(row db.UserSchedule) (UserSchedule, error) {
	var template ScheduleTemplate
	if err := json.Unmarshal(row.Template, &template); err != nil {
		return UserSchedule{}, err
	}
	return UserSchedule{UserID: row.UserID, Template: template}, nil
}

func scheduleToDomain(value ScheduleTemplate) domainschedule.Template {
	return domainschedule.Template{Type: value.Type, Days: value.Days, On: value.On, Off: value.Off, Start: value.Start, End: value.End, CycleStart: value.CycleStart}
}

func exceptionFromDB(row db.ShiftException) ShiftException {
	return ShiftException{ID: row.ID, UserID: row.UserID, Date: row.Date.Format(time.DateOnly), Type: row.Type, Start: timeString(row.StartTime), End: timeString(row.EndTime), Note: textPointer(row.Note)}
}

func pgTime(value *string) (pgtype.Time, error) {
	if value == nil {
		return pgtype.Time{}, nil
	}
	parsed, err := time.Parse("15:04", *value)
	if err != nil {
		return pgtype.Time{}, err
	}
	return pgtype.Time{Microseconds: int64(parsed.Hour()*60+parsed.Minute()) * 60 * 1_000_000, Valid: true}, nil
}

func timeString(value pgtype.Time) *string {
	if !value.Valid {
		return nil
	}
	minutes := value.Microseconds / 1_000_000 / 60
	formatted := fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
	return &formatted
}

func dereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
