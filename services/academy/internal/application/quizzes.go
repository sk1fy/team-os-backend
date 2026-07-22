package application

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) GetQuizzes(ctx context.Context, actor Actor, lessonID *uuid.UUID) ([]Quiz, error) {
	queries := db.New(s.pool)
	if lessonID != nil {
		versionLesson, versionLessonErr := queries.GetCourseVersionLesson(ctx, db.GetCourseVersionLessonParams{
			CompanyID: actor.CompanyID, ID: *lessonID,
		})
		if versionLessonErr == nil {
			version, versionErr := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{
				CompanyID: actor.CompanyID, ID: versionLesson.CourseVersionID,
			})
			if versionErr != nil {
				return nil, internal("Не удалось проверить версию курса", versionErr)
			}
			if accessErr := s.requireCourseAccess(ctx, queries, actor, version.CourseID); accessErr != nil {
				return nil, accessErr
			}
			rows, listErr := queries.GetCourseVersionQuizzes(ctx, db.GetCourseVersionQuizzesParams{
				CompanyID: actor.CompanyID, CourseVersionID: version.ID,
			})
			if listErr != nil {
				return nil, internal("Не удалось получить тесты версии", listErr)
			}
			result := make([]Quiz, 0, 1)
			for _, row := range rows {
				if row.LessonVersionID == *lessonID {
					result = append(result, versionQuizAsLegacy(row))
				}
			}
			return result, nil
		}
		if !isNoRows(versionLessonErr) {
			return nil, internal("Не удалось проверить урок версии", versionLessonErr)
		}
		lesson, err := queries.GetLesson(ctx, db.GetLessonParams{
			CompanyID: actor.CompanyID, ID: *lessonID,
		})
		if err != nil {
			if isNoRows(err) {
				return nil, notFound("Урок")
			}
			return nil, internal("Не удалось проверить урок", err)
		}
		if err = s.requireCourseAccess(ctx, queries, actor, lesson.CourseID); err != nil {
			return nil, err
		}
		rows, err := queries.GetLessonQuizzes(ctx, db.GetLessonQuizzesParams{
			CompanyID: actor.CompanyID, LessonID: *lessonID,
		})
		if err != nil {
			return nil, internal("Не удалось получить тесты", err)
		}
		return quizzesFromRows(rows), nil
	}
	visibleCourses, err := s.GetCourses(ctx, actor)
	if err != nil {
		return nil, err
	}
	result := make([]Quiz, 0)
	for _, course := range visibleCourses {
		version, versionErr := s.displayCourseVersion(ctx, queries, actor, course)
		if versionErr != nil {
			return nil, versionErr
		}
		if version != nil {
			rows, listErr := queries.GetCourseVersionQuizzes(ctx, db.GetCourseVersionQuizzesParams{
				CompanyID: actor.CompanyID, CourseVersionID: version.ID,
			})
			if listErr != nil {
				return nil, internal("Не удалось получить тесты версии", listErr)
			}
			for _, row := range rows {
				result = append(result, versionQuizAsLegacy(row))
			}
			continue
		}
		rows, listErr := queries.GetQuizzesByCourseIds(ctx, db.GetQuizzesByCourseIdsParams{
			CompanyID: actor.CompanyID, CourseIds: []uuid.UUID{course.ID},
		})
		if listErr != nil {
			return nil, internal("Не удалось получить тесты", listErr)
		}
		for _, row := range rows {
			result = append(result, Quiz{
				ID: row.ID, CompanyID: row.CompanyID, LessonID: row.LessonID,
				Questions: append(json.RawMessage(nil), row.Questions...), PassingScore: row.PassingScore,
				MaxAttempts: int4Pointer(row.MaxAttempts),
			})
		}
	}
	return result, nil
}

type UpsertQuizInput struct {
	ID           *uuid.UUID
	LessonID     uuid.UUID
	Questions    json.RawMessage
	PassingScore int32
	MaxAttempts  *int32
}

func (s *Service) UpsertQuiz(ctx context.Context, actor Actor, input UpsertQuizInput) (Quiz, error) {
	if input.PassingScore < 0 || input.PassingScore > 100 {
		return Quiz{}, validation("Проходной балл должен быть от 0 до 100")
	}
	if input.MaxAttempts != nil && *input.MaxAttempts < 1 {
		return Quiz{}, validation("Число попыток должно быть не меньше одной")
	}
	questions := input.Questions
	if len(questions) == 0 {
		questions = json.RawMessage(`[]`)
	}
	if !json.Valid(questions) {
		return Quiz{}, validation("Некорректные вопросы теста")
	}
	rootQueries := db.New(s.pool)
	if _, versionErr := rootQueries.GetCourseVersionLesson(ctx, db.GetCourseVersionLessonParams{
		CompanyID: actor.CompanyID, ID: input.LessonID,
	}); versionErr == nil {
		updated, updateErr := s.UpsertCourseVersionQuiz(ctx, actor, UpsertCourseVersionQuizInput{
			LessonVersionID: input.LessonID, Questions: questions,
			PassingScore: input.PassingScore, MaxAttempts: input.MaxAttempts,
		})
		if updateErr != nil {
			return Quiz{}, updateErr
		}
		return Quiz{
			ID: updated.ID, CompanyID: updated.CompanyID, LessonID: updated.LessonVersionID,
			Questions: updated.Questions, PassingScore: updated.PassingScore, MaxAttempts: updated.MaxAttempts,
		}, nil
	} else if !isNoRows(versionErr) {
		return Quiz{}, internal("Не удалось проверить урок версии", versionErr)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Quiz{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	var row db.Quiz
	if input.ID != nil {
		current, getErr := queries.GetQuiz(ctx, db.GetQuizParams{CompanyID: actor.CompanyID, ID: *input.ID})
		if getErr != nil {
			if isNoRows(getErr) {
				return Quiz{}, notFound("Тест")
			}
			return Quiz{}, internal("Не удалось проверить тест", getErr)
		}
		lesson, getErr := queries.GetLesson(ctx, db.GetLessonParams{CompanyID: actor.CompanyID, ID: current.LessonID})
		if getErr != nil {
			return Quiz{}, internal("Не удалось проверить урок", getErr)
		}
		if _, getErr = s.requireCourseEditAccess(ctx, queries, actor, lesson.CourseID); getErr != nil {
			return Quiz{}, getErr
		}
		row, err = queries.UpdateQuiz(ctx, db.UpdateQuizParams{
			CompanyID: actor.CompanyID, ID: *input.ID,
			Questions: questions, PassingScore: input.PassingScore,
			MaxAttempts: nullInt4(input.MaxAttempts),
		})
		if err != nil {
			if isNoRows(err) {
				return Quiz{}, notFound("Тест")
			}
			return Quiz{}, internal("Не удалось обновить тест", err)
		}
	} else {
		lesson, getErr := queries.GetLesson(ctx, db.GetLessonParams{
			CompanyID: actor.CompanyID, ID: input.LessonID,
		})
		if getErr != nil {
			if isNoRows(getErr) {
				return Quiz{}, notFound("Урок")
			}
			return Quiz{}, internal("Не удалось проверить урок", getErr)
		}
		if _, getErr = s.requireCourseEditAccess(ctx, queries, actor, lesson.CourseID); getErr != nil {
			return Quiz{}, getErr
		}
		row, err = queries.CreateQuiz(ctx, db.CreateQuizParams{
			ID: uuid.New(), CompanyID: actor.CompanyID, LessonID: input.LessonID,
			Questions: questions, PassingScore: input.PassingScore,
			MaxAttempts: nullInt4(input.MaxAttempts),
		})
		if err != nil {
			return Quiz{}, internal("Не удалось создать тест", err)
		}
		quizID := row.ID
		if err = queries.SetLessonQuiz(ctx, db.SetLessonQuizParams{
			CompanyID: actor.CompanyID, ID: input.LessonID,
			QuizID: nullUUID(&quizID),
		}); err != nil {
			return Quiz{}, internal("Не удалось привязать тест к уроку", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Quiz{}, internal("Не удалось сохранить тест", err)
	}
	return quizFromRow(row), nil
}
