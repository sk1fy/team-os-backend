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
	if !canReadAcademy(actor) {
		return nil, forbidden("Недостаточно прав для просмотра академии")
	}
	if !actor.canManage() {
		visibleCourses, err := s.GetCourses(ctx, actor)
		if err != nil {
			return nil, err
		}
		courseIDs := make([]uuid.UUID, len(visibleCourses))
		for index := range visibleCourses {
			courseIDs[index] = visibleCourses[index].ID
		}
		if len(courseIDs) == 0 {
			return []Quiz{}, nil
		}
		rows, err := queries.GetQuizzesByCourseIds(ctx, db.GetQuizzesByCourseIdsParams{
			CompanyID: actor.CompanyID, CourseIds: courseIDs,
		})
		if err != nil {
			return nil, internal("Не удалось получить тесты", err)
		}
		return quizzesFromRows(rows), nil
	}
	rows, err := queries.GetQuizzes(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить тесты", err)
	}
	return quizzesFromRows(rows), nil
}

type UpsertQuizInput struct {
	ID           *uuid.UUID
	LessonID     uuid.UUID
	Questions    json.RawMessage
	PassingScore int32
	MaxAttempts  *int32
}

func (s *Service) UpsertQuiz(ctx context.Context, actor Actor, input UpsertQuizInput) (Quiz, error) {
	if !actor.canManage() {
		return Quiz{}, forbidden("Недостаточно прав для изменения академии")
	}
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

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Quiz{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	var row db.Quiz
	if input.ID != nil {
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
		if _, err = queries.GetLesson(ctx, db.GetLessonParams{
			CompanyID: actor.CompanyID, ID: input.LessonID,
		}); err != nil {
			if isNoRows(err) {
				return Quiz{}, notFound("Урок")
			}
			return Quiz{}, internal("Не удалось проверить урок", err)
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
