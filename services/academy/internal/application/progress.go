package application

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) GetProgress(ctx context.Context, actor Actor, courseID *uuid.UUID) ([]Progress, error) {
	queries := db.New(s.pool)
	var rows []db.Progress
	var err error
	switch {
	case actor.Role == "partner":
		rows, err = queries.GetUserProgressRows(ctx, db.GetUserProgressRowsParams{
			CompanyID: actor.CompanyID, UserID: actor.UserID,
		})
	case courseID != nil:
		rows, err = queries.GetCourseProgress(ctx, db.GetCourseProgressParams{
			CompanyID: actor.CompanyID, CourseID: *courseID,
		})
	default:
		rows, err = queries.GetProgress(ctx, actor.CompanyID)
	}
	if err != nil {
		return nil, internal("Не удалось получить прогресс", err)
	}
	if actor.Role == "partner" && courseID != nil {
		rows = slices.DeleteFunc(rows, func(row db.Progress) bool { return row.CourseID != *courseID })
	}

	attemptRows, err := queries.GetQuizAttemptsWithCourse(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить попытки тестов", err)
	}
	type progressKey struct {
		userID   uuid.UUID
		courseID uuid.UUID
	}
	attempts := make(map[progressKey][]QuizAttempt)
	for _, attempt := range attemptRows {
		key := progressKey{userID: attempt.UserID, courseID: attempt.CourseID}
		attempts[key] = append(attempts[key], QuizAttempt{
			ID: attempt.ID, QuizID: attempt.QuizID, UserID: attempt.UserID,
			Score: attempt.Score, Passed: attempt.Passed,
			PendingReview: attempt.PendingReview, CreatedAt: attempt.CreatedAt,
		})
	}

	result := make([]Progress, len(rows))
	for index, row := range rows {
		result[index] = progressFromRow(row, attempts[progressKey{userID: row.UserID, courseID: row.CourseID}])
	}
	return result, nil
}

type MarkLessonCompleteInput struct {
	CourseID uuid.UUID
	LessonID uuid.UUID
	UserID   *uuid.UUID
}

func (s *Service) MarkLessonComplete(ctx context.Context, actor Actor, input MarkLessonCompleteInput) (Progress, error) {
	userID := actor.UserID
	if input.UserID != nil && *input.UserID != actor.UserID {
		if !actor.canManage() {
			return Progress{}, forbidden("Отмечать уроки можно только за себя")
		}
		userID = *input.UserID
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Progress{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	lesson, err := queries.GetLesson(ctx, db.GetLessonParams{CompanyID: actor.CompanyID, ID: input.LessonID})
	if err != nil {
		if isNoRows(err) {
			return Progress{}, notFound("Урок")
		}
		return Progress{}, internal("Не удалось получить урок", err)
	}
	if lesson.CourseID != input.CourseID {
		return Progress{}, validation("Урок не принадлежит указанному курсу")
	}

	now := s.now().UTC()
	row, err := queries.GetUserCourseProgressForUpdate(ctx, db.GetUserCourseProgressForUpdateParams{
		CompanyID: actor.CompanyID, UserID: userID, CourseID: input.CourseID,
	})
	if err != nil {
		if !isNoRows(err) {
			return Progress{}, internal("Не удалось получить прогресс", err)
		}
		row, err = queries.InsertProgress(ctx, db.InsertProgressParams{
			CompanyID: actor.CompanyID, UserID: userID, CourseID: input.CourseID,
			Status: "in_progress", CompletedLessonIds: []uuid.UUID{},
			StartedAt: nullTimestamptz(&now),
		})
		if err != nil {
			return Progress{}, internal("Не удалось создать прогресс", err)
		}
	}

	completed := row.CompletedLessonIds
	if !slices.Contains(completed, input.LessonID) {
		completed = append(completed, input.LessonID)
	}

	lessonIDs, err := queries.GetCourseLessonIds(ctx, db.GetCourseLessonIdsParams{
		CompanyID: actor.CompanyID, CourseID: input.CourseID,
	})
	if err != nil {
		return Progress{}, internal("Не удалось получить уроки курса", err)
	}
	allDone := true
	for _, id := range lessonIDs {
		if !slices.Contains(completed, id) {
			allDone = false
			break
		}
	}

	status := "in_progress"
	completedAt := timestamptzPointer(row.CompletedAt)
	if allDone {
		status = "completed"
		if completedAt == nil {
			completedAt = &now
		}
	} else {
		completedAt = nil
	}
	startedAt := timestamptzPointer(row.StartedAt)
	if startedAt == nil {
		startedAt = &now
	}

	updated, err := queries.UpdateProgressRow(ctx, db.UpdateProgressRowParams{
		CompanyID: actor.CompanyID, UserID: userID, CourseID: input.CourseID,
		Status: status, CompletedLessonIds: completed,
		StartedAt: nullTimestamptz(startedAt), CompletedAt: nullTimestamptz(completedAt),
	})
	if err != nil {
		return Progress{}, internal("Не удалось обновить прогресс", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Progress{}, internal("Не удалось сохранить прогресс", err)
	}
	return progressFromRow(updated, nil), nil
}
