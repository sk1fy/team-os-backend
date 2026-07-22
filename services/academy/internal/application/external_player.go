package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

const (
	externalOperationCompleteLesson = "complete_lesson"
	externalOperationSubmitQuiz     = "submit_quiz"
)

func (s *Service) GetPublicAcademyEnrollment(
	ctx context.Context,
	principal ExternalPrincipal,
	enrollmentID uuid.UUID,
) (Enrollment, error) {
	if err := s.materializeExternalState(ctx, principal.CompanyID); err != nil {
		return Enrollment{}, err
	}
	return s.getPublicAcademyEnrollmentWithQueries(
		ctx, db.New(s.pool), principal, enrollmentID, s.now().UTC(),
	)
}

func (s *Service) getPublicAcademyEnrollmentWithQueries(
	ctx context.Context,
	queries *db.Queries,
	principal ExternalPrincipal,
	enrollmentID uuid.UUID,
	now time.Time,
) (Enrollment, error) {
	row, err := queries.GetExternalEnrollmentForSession(ctx, db.GetExternalEnrollmentForSessionParams{
		CompanyID: principal.CompanyID, EnrollmentID: enrollmentID, SessionID: principal.SessionID, Now: now,
	})
	if err != nil {
		return Enrollment{}, notFound("Внешнее прохождение")
	}
	value := enrollmentFromExternalSessionRow(row)
	outline, outlineErr := queries.ListExternalEnrollmentOutlineForSession(ctx, db.ListExternalEnrollmentOutlineForSessionParams{
		Now: nullTimestamptzPointer(now), CompanyID: principal.CompanyID,
		EnrollmentID: enrollmentID, SessionID: principal.SessionID,
	})
	if outlineErr == nil && len(outline) > 0 {
		completed := 0
		for _, lesson := range outline {
			if lesson.LessonStatus.Valid && lesson.LessonStatus.String == "completed" {
				completed++
			}
		}
		value.ProgressPercent = int32(completed * 100 / len(outline))
	}
	return value, nil
}

func (s *Service) GetPublicAcademyEnrollmentOutline(
	ctx context.Context,
	principal ExternalPrincipal,
	enrollmentID uuid.UUID,
) (EnrollmentOutline, error) {
	enrollment, err := s.GetPublicAcademyEnrollment(ctx, principal, enrollmentID)
	if err != nil {
		return EnrollmentOutline{}, err
	}
	rows, err := db.New(s.pool).ListExternalEnrollmentOutlineForSession(ctx, db.ListExternalEnrollmentOutlineForSessionParams{
		Now: nullTimestamptzPointer(s.now().UTC()), CompanyID: principal.CompanyID,
		EnrollmentID: enrollmentID, SessionID: principal.SessionID,
	})
	if err != nil {
		return EnrollmentOutline{}, internal("Не удалось получить структуру внешнего прохождения", err)
	}
	sections := make([]EnrollmentOutlineSection, 0)
	indexes := make(map[uuid.UUID]int)
	for _, row := range rows {
		index, ok := indexes[row.SectionVersionID]
		if !ok {
			index = len(sections)
			indexes[row.SectionVersionID] = index
			sections = append(sections, EnrollmentOutlineSection{CourseVersionSection: CourseVersionSection{
				ID: row.SectionVersionID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
				Title: row.SectionTitle, Order: row.SectionOrder,
			}})
		}
		status := "locked"
		if row.LessonStatus.Valid {
			status = row.LessonStatus.String
		}
		var reason *string
		if !row.ContentAvailable {
			value := "Урок недоступен"
			reason = &value
			if status != "completed" {
				status = "locked"
			}
		}
		var quizID *uuid.UUID
		if hasQuiz, ok := row.HasQuiz.(bool); ok && hasQuiz {
			value := uuid.Nil
			quizID = &value
		}
		sections[index].Lessons = append(sections[index].Lessons, EnrollmentOutlineLesson{
			CourseVersionLesson: CourseVersionLesson{
				ID: row.ID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
				SectionVersionID: row.SectionVersionID, Title: row.Title, Order: row.LessonOrder,
				EstimatedMinutes: int4Pointer(row.EstimatedMinutes), QuizVersionID: quizID,
			},
			Status: status, LockReason: reason, FirstOpenedAt: timestamptzPointer(row.FirstOpenedAt),
			CompletedAt: timestamptzPointer(row.CompletedAt),
		})
	}
	return EnrollmentOutline{Enrollment: enrollment, Sections: sections}, nil
}

func (s *Service) GetPublicAcademyEnrollmentLesson(
	ctx context.Context,
	principal ExternalPrincipal,
	enrollmentID, lessonID uuid.UUID,
) (EnrollmentLesson, error) {
	enrollment, err := s.GetPublicAcademyEnrollment(ctx, principal, enrollmentID)
	if err != nil {
		return EnrollmentLesson{}, err
	}
	row, err := db.New(s.pool).GetExternalLessonContentForSession(ctx, db.GetExternalLessonContentForSessionParams{
		CompanyID: principal.CompanyID, EnrollmentID: enrollmentID, LessonVersionID: lessonID,
		SessionID: principal.SessionID, Now: s.now().UTC(),
	})
	if err != nil {
		return EnrollmentLesson{}, notFound("Урок внешнего прохождения")
	}
	lesson := CourseVersionLesson{
		ID: row.ID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
		SectionVersionID: row.SectionVersionID, Title: row.Title, Order: row.Order,
		Content: append(json.RawMessage(nil), row.Content...), SourceType: row.SourceType,
		EstimatedMinutes: int4Pointer(row.EstimatedMinutes), QuizVersionID: nullUUIDPointer(row.QuizVersionID),
		FileIDs: append([]uuid.UUID(nil), row.FileIds...),
	}
	var quiz *CourseVersionQuiz
	if row.QuizVersionID.Valid && enrollment.AccessStatus == "active" {
		quizRow, quizErr := db.New(s.pool).GetExternalQuizForSession(ctx, db.GetExternalQuizForSessionParams{
			QuizVersionID: row.QuizVersionID.UUID, CompanyID: principal.CompanyID,
			EnrollmentID: enrollmentID, SessionID: principal.SessionID, Now: s.now().UTC(),
		})
		if quizErr == nil {
			quiz = &CourseVersionQuiz{ID: quizRow.ID, CompanyID: quizRow.CompanyID,
				CourseVersionID: quizRow.CourseVersionID, LessonVersionID: quizRow.LessonVersionID,
				Questions: append(json.RawMessage(nil), quizRow.Questions...), PassingScore: quizRow.PassingScore,
				MaxAttempts: int4Pointer(quizRow.MaxAttempts)}
		}
	}
	return EnrollmentLesson{Enrollment: enrollment, Lesson: lesson, Quiz: quiz, Progress: EnrollmentLessonProgress{
		CompanyID: row.CompanyID, EnrollmentID: enrollmentID, LessonVersionID: lessonID,
		Status: row.LessonStatus, FirstOpenedAt: timestamptzPointer(row.FirstOpenedAt),
		CompletedAt: timestamptzPointer(row.CompletedAt), ActiveSeconds: row.ActiveSeconds,
		LastPosition: textPointer(row.LastPosition),
	}}, nil
}

func (s *Service) CompletePublicAcademyEnrollmentLesson(
	ctx context.Context,
	principal ExternalPrincipal,
	enrollmentID, lessonID uuid.UUID,
	idempotencyKey string,
) (Enrollment, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return Enrollment{}, validation("Требуется ключ идемпотентности")
	}
	requestHash := externalMutationRequestHash(struct {
		EnrollmentID uuid.UUID `json:"enrollmentId"`
		LessonID     uuid.UUID `json:"lessonId"`
	}{enrollmentID, lessonID})
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Enrollment{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	now := s.now().UTC()
	reservation, err := s.reserveExternalMutationInTx(ctx, queries, principal,
		externalOperationCompleteLesson, key, requestHash, lessonID, now)
	if err != nil {
		return Enrollment{}, err
	}
	if reservation.CompletedAt.Valid && len(reservation.ResultPayload) > 0 {
		var previous Enrollment
		if err = json.Unmarshal(reservation.ResultPayload, &previous); err != nil {
			return Enrollment{}, internal("Не удалось прочитать сохранённый результат урока", err)
		}
		if err = tx.Commit(ctx); err != nil {
			return Enrollment{}, internal("Не удалось завершить повторную операцию урока", err)
		}
		return previous, nil
	}
	actor := Actor{CompanyID: principal.CompanyID, UserID: principal.LearnerID, Role: "external"}
	if err = s.completeEnrollmentLessonInTx(ctx, queries, actor, CompleteEnrollmentLessonInput{
		EnrollmentID: enrollmentID, LessonID: lessonID,
	}, &principal); err != nil {
		return Enrollment{}, err
	}
	value, err := s.getPublicAcademyEnrollmentWithQueries(ctx, queries, principal, enrollmentID, now)
	if err != nil {
		return Enrollment{}, err
	}
	resultPayload, _ := json.Marshal(value)
	if _, err = queries.CompleteExternalMutationIdempotency(ctx, db.CompleteExternalMutationIdempotencyParams{
		ResultID: nullUUID(&lessonID), EnrollmentID: nullUUID(&enrollmentID), ResultPayload: resultPayload,
		CompletedAt: nullTimestamptz(&now), CompanyID: principal.CompanyID, ID: reservation.ID,
	}); err != nil {
		return Enrollment{}, internal("Не удалось сохранить результат завершения урока", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Enrollment{}, internal("Не удалось сохранить прогресс внешнего урока", err)
	}
	if isCampaignEnrollment(value) {
		event := campaignAnalyticsEvent{
			CampaignID: *value.SourceID, EnrollmentID: &value.ID, ExternalLearnerID: &principal.LearnerID,
			Type: "lesson_completed", IdempotencyKey: "lesson-completed:" + value.ID.String() + ":" + lessonID.String(),
			LessonVersionID: &lessonID, ProgressPercent: &value.ProgressPercent, OccurredAt: s.now().UTC(),
		}
		if analyticsErr := s.recordCampaignAnalyticsEvent(ctx, db.New(s.pool), principal.CompanyID, event); analyticsErr != nil {
			s.logger.Warn("campaign lesson analytics failed", "enrollmentId", value.ID, "error", analyticsErr)
		}
		if value.ProgressStatus == "completed" {
			event.Type = "course_completed"
			event.IdempotencyKey = "course-completed:" + value.ID.String()
			if value.StartedAt != nil && value.CompletedAt != nil {
				seconds := int64(value.CompletedAt.Sub(*value.StartedAt).Seconds())
				if seconds >= 0 {
					event.CompletionSecs = &seconds
				}
			}
			if analyticsErr := s.recordCampaignAnalyticsEvent(ctx, db.New(s.pool), principal.CompanyID, event); analyticsErr != nil {
				s.logger.Warn("campaign completion analytics failed", "enrollmentId", value.ID, "error", analyticsErr)
			}
		}
	}
	return value, nil
}

func (s *Service) SubmitPublicAcademyQuizAttempt(
	ctx context.Context,
	principal ExternalPrincipal,
	input SubmitExternalQuizInput,
) (ExternalQuizAttemptResult, error) {
	key := strings.TrimSpace(input.IdempotencyKey)
	if key == "" {
		return ExternalQuizAttemptResult{}, validation("Требуется ключ идемпотентности")
	}
	requestHash := externalMutationRequestHash(struct {
		EnrollmentID uuid.UUID              `json:"enrollmentId"`
		QuizID       uuid.UUID              `json:"quizId"`
		Answers      []EnrollmentQuizAnswer `json:"answers"`
	}{input.EnrollmentID, input.QuizID, input.Answers})
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalQuizAttemptResult{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	now := s.now().UTC()
	reservation, err := s.reserveExternalMutationInTx(ctx, queries, principal,
		externalOperationSubmitQuiz, key, requestHash, input.QuizID, now)
	if err != nil {
		return ExternalQuizAttemptResult{}, err
	}
	if reservation.CompletedAt.Valid && len(reservation.ResultPayload) > 0 {
		var previous ExternalQuizAttemptResult
		if err = json.Unmarshal(reservation.ResultPayload, &previous); err != nil {
			return ExternalQuizAttemptResult{}, internal("Не удалось прочитать сохранённый результат теста", err)
		}
		if err = tx.Commit(ctx); err != nil {
			return ExternalQuizAttemptResult{}, internal("Не удалось завершить повторную отправку теста", err)
		}
		return previous, nil
	}
	actor := Actor{CompanyID: principal.CompanyID, UserID: principal.LearnerID, Role: "external"}
	attempt, err := s.submitEnrollmentQuizAttemptInTx(ctx, queries, actor, SubmitEnrollmentQuizInput{
		EnrollmentID: input.EnrollmentID, QuizID: input.QuizID, Answers: input.Answers,
	}, &principal, reservation.ID)
	if err != nil {
		return ExternalQuizAttemptResult{}, err
	}
	var remaining *int32
	quiz, quizErr := queries.GetCourseVersionQuiz(ctx, db.GetCourseVersionQuizParams{
		CompanyID: principal.CompanyID, ID: input.QuizID,
	})
	if quizErr == nil && quiz.MaxAttempts.Valid {
		value := quiz.MaxAttempts.Int32 - attempt.AttemptNumber
		if value < 0 {
			value = 0
		}
		remaining = &value
	}
	result := ExternalQuizAttemptResult{ID: attempt.ID, Score: attempt.Score, Passed: attempt.Passed,
		PendingReview: attempt.PendingReview, AttemptsRemaining: remaining, CreatedAt: attempt.CreatedAt}
	resultPayload, _ := json.Marshal(result)
	if _, err = queries.CompleteExternalMutationIdempotency(ctx, db.CompleteExternalMutationIdempotencyParams{
		ResultID: nullUUID(&attempt.ID), EnrollmentID: nullUUID(&input.EnrollmentID), ResultPayload: resultPayload,
		CompletedAt: nullTimestamptz(&now), CompanyID: principal.CompanyID, ID: reservation.ID,
	}); err != nil {
		return ExternalQuizAttemptResult{}, internal("Не удалось сохранить результат идемпотентной отправки", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalQuizAttemptResult{}, internal("Не удалось сохранить результат внешнего теста", err)
	}
	if enrollment, loadErr := s.GetPublicAcademyEnrollment(ctx, principal, input.EnrollmentID); loadErr == nil && isCampaignEnrollment(enrollment) {
		analyticsErr := s.recordCampaignAnalyticsEvent(ctx, db.New(s.pool), principal.CompanyID, campaignAnalyticsEvent{
			CampaignID: *enrollment.SourceID, EnrollmentID: &enrollment.ID, ExternalLearnerID: &principal.LearnerID,
			Type: "quiz_submitted", IdempotencyKey: "quiz-submitted:" + strings.TrimSpace(input.IdempotencyKey),
			ProgressPercent: &enrollment.ProgressPercent, OccurredAt: now,
		})
		if analyticsErr != nil {
			s.logger.Warn("campaign quiz analytics failed", "enrollmentId", enrollment.ID, "error", analyticsErr)
		}
	}
	return result, nil
}

func (s *Service) reserveExternalMutationInTx(
	ctx context.Context,
	queries *db.Queries,
	principal ExternalPrincipal,
	operation string,
	idempotencyKey string,
	requestHash string,
	aggregateID uuid.UUID,
	now time.Time,
) (db.ExternalMutationIdempotency, error) {
	_, err := queries.ReserveExternalMutationIdempotency(ctx, db.ReserveExternalMutationIdempotencyParams{
		ID: uuid.New(), CompanyID: principal.CompanyID, ExternalLearnerID: principal.LearnerID,
		Operation: operation, IdempotencyKey: idempotencyKey, RequestHash: requestHash,
		AggregateID: aggregateID, CreatedAt: now,
	})
	if err != nil {
		return db.ExternalMutationIdempotency{}, internal("Не удалось зарезервировать внешнюю операцию", err)
	}
	reservation, err := queries.GetExternalMutationIdempotencyForUpdate(ctx, db.GetExternalMutationIdempotencyForUpdateParams{
		CompanyID: principal.CompanyID, ExternalLearnerID: principal.LearnerID,
		Operation: operation, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return db.ExternalMutationIdempotency{}, internal("Не удалось заблокировать внешнюю операцию", err)
	}
	if reservation.RequestHash != requestHash || reservation.AggregateID != aggregateID {
		return db.ExternalMutationIdempotency{}, conflict("Ключ идемпотентности уже использован для другого запроса")
	}
	return reservation, nil
}

func externalMutationRequestHash(value any) string {
	requestBytes, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(requestBytes))
}

func (s *Service) GetPublicAcademyEnrollmentResults(
	ctx context.Context,
	principal ExternalPrincipal,
	enrollmentID uuid.UUID,
) (ExternalEnrollmentResults, error) {
	enrollment, err := s.GetPublicAcademyEnrollment(ctx, principal, enrollmentID)
	if err != nil {
		return ExternalEnrollmentResults{}, err
	}
	outline, err := db.New(s.pool).ListExternalEnrollmentOutlineForSession(ctx, db.ListExternalEnrollmentOutlineForSessionParams{
		Now: nullTimestamptzPointer(s.now().UTC()), CompanyID: principal.CompanyID,
		EnrollmentID: enrollmentID, SessionID: principal.SessionID,
	})
	if err != nil {
		return ExternalEnrollmentResults{}, internal("Не удалось получить результаты уроков", err)
	}
	completed := make([]uuid.UUID, 0)
	for _, row := range outline {
		if row.LessonStatus.Valid && row.LessonStatus.String == "completed" {
			completed = append(completed, row.ID)
		}
	}
	rows, err := db.New(s.pool).ListExternalQuizResultsForSession(ctx, db.ListExternalQuizResultsForSessionParams{
		CompanyID: principal.CompanyID, EnrollmentID: enrollmentID, SessionID: principal.SessionID, Now: s.now().UTC(),
	})
	if err != nil {
		return ExternalEnrollmentResults{}, internal("Не удалось получить результаты тестов", err)
	}
	attempts := make([]ExternalQuizAttemptResult, len(rows))
	for index, row := range rows {
		attempts[index] = ExternalQuizAttemptResult{ID: row.ID, Score: row.Score, Passed: row.Passed,
			PendingReview: row.PendingReview, CreatedAt: row.CreatedAt}
	}
	return ExternalEnrollmentResults{Enrollment: enrollment, CompletedLessonIDs: completed, QuizAttempts: attempts}, nil
}

func (s *Service) materializeExternalState(ctx context.Context, companyID uuid.UUID) error {
	now := s.now().UTC()
	queries := db.New(s.pool)
	if _, err := queries.MaterializeExpiredExternalEnrollments(ctx, db.MaterializeExpiredExternalEnrollmentsParams{
		Now: now, CompanyID: companyID, BatchSize: 100,
	}); err != nil {
		return internal("Не удалось обновить истёкшие внешние прохождения", err)
	}
	if _, err := queries.MaterializeExpiredExternalChallenges(ctx, db.MaterializeExpiredExternalChallengesParams{
		CompanyID: companyID, Now: now, BatchSize: 100,
	}); err != nil {
		return internal("Не удалось очистить истёкшие подтверждения", err)
	}
	return nil
}

func enrollmentFromExternalSessionRow(row db.GetExternalEnrollmentForSessionRow) Enrollment {
	return Enrollment{ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		VersionNumber: row.CourseVersionNumber, LearnerType: "external", ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID),
		SourceType: row.SourceType, SourceID: nullUUIDPointer(row.SourceID), AttemptNumber: row.AttemptNumber,
		ProgressStatus: row.ProgressStatus, AccessStatus: row.AccessStatus,
		CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID), ActivatedAt: timestamptzPointer(row.ActivatedAt),
		AccessUntil: timestamptzPointer(row.AccessUntil), StartedAt: timestamptzPointer(row.StartedAt),
		CompletedAt: timestamptzPointer(row.CompletedAt), LastActivityAt: timestamptzPointer(row.LastActivityAt),
		FrozenAt: timestamptzPointer(row.FrozenAt), SuspendedAt: timestamptzPointer(row.SuspendedAt),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func isCampaignEnrollment(value Enrollment) bool {
	return value.SourceID != nil && (value.SourceType == "partner_promo_campaign" ||
		value.SourceType == "company_candidate_campaign")
}
