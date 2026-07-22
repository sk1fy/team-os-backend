package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

// GetProgress is the legacy DTO adapter. Enrollment is the only source of
// truth; no reads are served from the mutable legacy progress table.
func (s *Service) GetProgress(ctx context.Context, actor Actor, courseID *uuid.UUID) ([]Progress, error) {
	filters := EnrollmentFilters{CourseID: courseID}
	enrollments, err := s.GetEnrollments(ctx, actor, filters)
	if err != nil {
		return nil, err
	}
	result := make([]Progress, 0, len(enrollments))
	for _, enrollment := range enrollments {
		if enrollment.UserID == nil {
			continue
		}
		snapshot, snapshotErr := s.getEnrollmentProgressSnapshot(ctx, actor, enrollment.ID)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		result = append(result, legacyProgressFromEnrollment(snapshot))
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
	row, err := db.New(s.pool).GetLatestUserCourseEnrollment(ctx, db.GetLatestUserCourseEnrollmentParams{
		CompanyID: actor.CompanyID, UserID: nullUUID(&userID), CourseID: input.CourseID,
	})
	if err != nil {
		if isNoRows(err) {
			return Progress{}, forbidden("Курс не назначен пользователю")
		}
		return Progress{}, internal("Не удалось получить прохождение курса", err)
	}
	if row.AccessStatus == "ready" || row.AccessStatus == "invited" {
		if _, _, err = s.ResumeEnrollment(ctx, actor, row.ID); err != nil {
			return Progress{}, err
		}
	}
	snapshot, err := s.CompleteEnrollmentLesson(ctx, actor, CompleteEnrollmentLessonInput{
		EnrollmentID: row.ID, LessonID: input.LessonID,
		IdempotencyKey: "legacy-mark:" + row.ID.String() + ":" + input.LessonID.String(),
	})
	if err != nil {
		return Progress{}, err
	}
	return legacyProgressFromEnrollment(snapshot), nil
}

func legacyProgressFromEnrollment(snapshot EnrollmentProgressSnapshot) Progress {
	completedIDs := make([]uuid.UUID, 0, len(snapshot.Lessons))
	for _, lesson := range snapshot.Lessons {
		if lesson.Status == "completed" {
			completedIDs = append(completedIDs, lesson.LessonVersionID)
		}
	}
	userID := uuid.Nil
	if snapshot.Enrollment.UserID != nil {
		userID = *snapshot.Enrollment.UserID
	}
	attempts := make([]QuizAttempt, len(snapshot.QuizAttempts))
	for index, attempt := range snapshot.QuizAttempts {
		attempts[index] = QuizAttempt{
			ID: attempt.ID, QuizID: attempt.QuizVersionID, UserID: userID,
			Score: attempt.Score, Passed: attempt.Passed, PendingReview: attempt.PendingReview,
			CreatedAt: attempt.CreatedAt,
		}
	}
	return Progress{
		UserID: userID, CourseID: snapshot.Enrollment.CourseID,
		EnrollmentID: &snapshot.Enrollment.ID, CourseVersionID: &snapshot.Enrollment.CourseVersionID,
		Status: snapshot.Enrollment.ProgressStatus, ProgressPercent: &snapshot.Enrollment.ProgressPercent,
		CurrentLessonVersionID: snapshot.Enrollment.CurrentLessonVersionID,
		CompletedLessonIDs:     completedIDs, QuizAttempts: attempts,
		StartedAt: snapshot.Enrollment.StartedAt, CompletedAt: snapshot.Enrollment.CompletedAt,
	}
}
