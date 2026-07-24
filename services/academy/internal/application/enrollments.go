package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	domainenrollment "github.com/sk1fy/team-os-backend/services/academy/internal/domain/enrollment"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type EnrollmentFilters struct {
	CourseID        *uuid.UUID
	CourseVersionID *uuid.UUID
	UserID          *uuid.UUID
	ProgressStatus  *string
	AccessStatus    *string
}

func (s *Service) GetCatalogCourseVersion(ctx context.Context, actor Actor, courseID uuid.UUID) (CourseVersionContent, error) {
	if !canReadAcademy(actor) {
		return CourseVersionContent{}, forbidden("Недостаточно прав для просмотра каталога")
	}
	queries := db.New(s.pool)
	row, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return CourseVersionContent{}, notFound("Курс каталога")
		}
		return CourseVersionContent{}, internal("Не удалось получить курс каталога", err)
	}
	course := courseFromRow(row)
	if course.OwnerType != "company" || course.LifecycleStatus != "active" ||
		course.DistributionStatus != "active" || course.Status != "published" ||
		(course.Visibility != "public" && course.Visibility != "company") ||
		course.LatestPublishedVersionID == nil {
		return CourseVersionContent{}, notFound("Курс каталога")
	}
	if allowed, audienceErr := s.partnerAudienceAllows(ctx, queries, actor, course); audienceErr != nil {
		return CourseVersionContent{}, audienceErr
	} else if !allowed {
		return CourseVersionContent{}, notFound("Курс каталога")
	}
	version, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{
		CompanyID: actor.CompanyID, ID: *course.LatestPublishedVersionID,
	})
	if err != nil {
		return CourseVersionContent{}, internal("Не удалось получить версию курса каталога", err)
	}
	return s.loadCourseVersionContent(ctx, queries, version)
}

func (s *Service) GetEnrollments(ctx context.Context, actor Actor, filters EnrollmentFilters) ([]Enrollment, error) {
	if !canReadAcademy(actor) {
		return nil, forbidden("Недостаточно прав для просмотра академии")
	}
	if err := validateEnrollmentFilters(filters); err != nil {
		return nil, err
	}
	userID := filters.UserID
	var partnerOwnerID *uuid.UUID
	if !actor.canManage() && actor.Role != "partner" {
		userID = &actor.UserID
	} else if actor.Role == "partner" {
		partnerOwnerID = &actor.UserID
	}
	rows, err := db.New(s.pool).ListInternalEnrollments(ctx, db.ListInternalEnrollmentsParams{
		Now: s.now().UTC(), CompanyID: actor.CompanyID,
		UserID: nullUUID(userID), CourseID: nullUUID(filters.CourseID),
		CourseVersionID: nullUUID(filters.CourseVersionID),
		ProgressStatus:  nullText(filters.ProgressStatus), AccessStatus: nullText(filters.AccessStatus),
		PartnerOwnerID: nullUUID(partnerOwnerID),
	})
	if err != nil {
		return nil, internal("Не удалось получить прохождения", err)
	}
	result := make([]Enrollment, 0, len(rows))
	for _, row := range rows {
		result = append(result, enrollmentFromListRow(row))
	}
	return result, nil
}

func (s *Service) GetInternalEnrollmentReportPage(
	ctx context.Context,
	actor Actor,
	query InternalEnrollmentReportQuery,
) (InternalEnrollmentReportPage, error) {
	if !actor.canManage() {
		return InternalEnrollmentReportPage{}, forbidden("Отчёт доступен только владельцу или администратору")
	}
	if err := validateInternalEnrollmentReportQuery(query); err != nil {
		return InternalEnrollmentReportPage{}, err
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > maxInternalEnrollmentReportPageSize {
		pageSize = maxInternalEnrollmentReportPageSize
	}
	var search *string
	if query.Search != nil {
		if trimmed := strings.TrimSpace(*query.Search); trimmed != "" {
			search = &trimmed
		}
	}
	queries := db.New(s.pool)
	countParams := db.CountInternalEnrollmentReportRowsParams{
		Now: s.now().UTC(), CompanyID: actor.CompanyID,
		UserIds:  append([]uuid.UUID(nil), query.UserIDs...),
		CourseID: nullUUID(query.CourseID), Search: nullText(search),
		SearchUserIds: append([]uuid.UUID(nil), query.SearchUserIDs...),
		Status:        nullText(query.Status),
	}
	total, err := queries.CountInternalEnrollmentReportRows(ctx, countParams)
	if err != nil {
		return InternalEnrollmentReportPage{}, internal("Не удалось подсчитать строки внутреннего отчёта", err)
	}
	rows, err := queries.ListInternalEnrollmentReportRows(ctx, db.ListInternalEnrollmentReportRowsParams{
		Now: countParams.Now, CompanyID: countParams.CompanyID,
		UserIds: countParams.UserIds, CourseID: countParams.CourseID,
		Search: countParams.Search, SearchUserIds: countParams.SearchUserIds,
		Status: countParams.Status, Sort: normalizedInternalEnrollmentReportSort(query.Sort),
		PageLimit: pageSize, PageOffset: (page - 1) * pageSize,
	})
	if err != nil {
		return InternalEnrollmentReportPage{}, internal("Не удалось получить внутренний отчёт", err)
	}
	items := make([]Enrollment, len(rows))
	for index := range rows {
		items[index] = enrollmentFromInternalReportRow(rows[index])
	}
	return InternalEnrollmentReportPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

const maxInternalEnrollmentReportPageSize int32 = 10_001

func validateInternalEnrollmentReportQuery(query InternalEnrollmentReportQuery) error {
	if query.Status != nil {
		switch *query.Status {
		case "not_started", "in_progress", "completed", "overdue", "frozen":
		default:
			return validation("Некорректный статус внутреннего отчёта")
		}
	}
	switch query.Sort {
	case "", "updated_desc", "updated_asc", "title_asc", "title_desc", "deadline_asc", "status":
		return nil
	default:
		return validation("Некорректная сортировка внутреннего отчёта")
	}
}

func normalizedInternalEnrollmentReportSort(value string) string {
	if value == "" {
		return "updated_desc"
	}
	return value
}

func (s *Service) SelfEnrollCourse(ctx context.Context, actor Actor, courseID uuid.UUID) (Enrollment, error) {
	if !canReadAcademy(actor) {
		return Enrollment{}, forbidden("Недостаточно прав для самостоятельной записи на курс")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Enrollment{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	courseRow, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{
		CompanyID: actor.CompanyID, ID: courseID,
	})
	if err != nil {
		if isNoRows(err) {
			return Enrollment{}, notFound("Курс каталога")
		}
		return Enrollment{}, internal("Не удалось проверить курс каталога", err)
	}
	course := courseFromRow(courseRow)
	if course.OwnerType != "company" || course.LifecycleStatus != "active" ||
		course.DistributionStatus != "active" || course.Status != "published" ||
		(course.Visibility != "public" && course.Visibility != "company") ||
		course.LatestPublishedVersionID == nil {
		return Enrollment{}, notFound("Курс каталога")
	}
	if allowed, audienceErr := s.partnerAudienceAllows(ctx, queries, actor, course); audienceErr != nil {
		return Enrollment{}, audienceErr
	} else if !allowed {
		return Enrollment{}, notFound("Курс каталога")
	}
	existing, existingErr := queries.GetCurrentUserCourseEnrollmentForUpdate(ctx, db.GetCurrentUserCourseEnrollmentForUpdateParams{
		CompanyID: actor.CompanyID, UserID: nullUUID(&actor.UserID), CourseID: courseID,
	})
	if existingErr == nil {
		if err = tx.Commit(ctx); err != nil {
			return Enrollment{}, internal("Не удалось завершить проверку прохождения", err)
		}
		return s.getInternalEnrollmentReadModel(ctx, actor, existing.ID, courseID)
	}
	if !isNoRows(existingErr) {
		return Enrollment{}, internal("Не удалось проверить текущее прохождение", existingErr)
	}
	attemptNumber, err := queries.GetNextUserCourseAttemptNumber(ctx, db.GetNextUserCourseAttemptNumberParams{
		CompanyID: actor.CompanyID, UserID: nullUUID(&actor.UserID), CourseID: courseID,
	})
	if err != nil {
		return Enrollment{}, internal("Не удалось определить номер прохождения", err)
	}
	row, err := queries.CreateSelfEnrollment(ctx, db.CreateSelfEnrollmentParams{
		ID: uuid.New(), UserID: nullUUID(&actor.UserID), AttemptNumber: attemptNumber,
		CreatedAt: s.now().UTC(), CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		if isNoRows(err) {
			return Enrollment{}, notFound("Курс каталога")
		}
		return Enrollment{}, internal("Не удалось записаться на курс", err)
	}
	if err = s.auditMutation(ctx, queries, actor, "self_enrollment_created", "course_enrollment", row.ID,
		nil, map[string]any{"courseId": courseID, "sourceType": "self_enrollment"}); err != nil {
		return Enrollment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Enrollment{}, internal("Не удалось сохранить запись на курс", err)
	}
	return s.getInternalEnrollmentReadModel(ctx, actor, row.ID, courseID)
}

func (s *Service) getInternalEnrollmentReadModel(
	ctx context.Context,
	actor Actor,
	enrollmentID, courseID uuid.UUID,
) (Enrollment, error) {
	values, err := s.GetEnrollments(ctx, actor, EnrollmentFilters{CourseID: &courseID})
	if err != nil {
		return Enrollment{}, err
	}
	for _, value := range values {
		if value.ID == enrollmentID {
			return value, nil
		}
	}
	return Enrollment{}, notFound("Прохождение")
}

func validateEnrollmentFilters(filters EnrollmentFilters) error {
	if filters.ProgressStatus != nil {
		switch *filters.ProgressStatus {
		case "not_started", "in_progress", "completed":
		default:
			return validation("Некорректное состояние прогресса")
		}
	}
	if filters.AccessStatus != nil {
		switch *filters.AccessStatus {
		case "invited", "ready", "active", "expired", "frozen", "suspended", "revoked", "closed":
		default:
			return validation("Некорректное состояние доступа")
		}
	}
	return nil
}

func (s *Service) GetEnrollment(ctx context.Context, actor Actor, id uuid.UUID) (Enrollment, error) {
	queries := db.New(s.pool)
	return s.requireEnrollmentAccess(ctx, queries, actor, id, false)
}

func (s *Service) requireEnrollmentAccess(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	id uuid.UUID,
	mutation bool,
) (Enrollment, error) {
	row, err := queries.GetEnrollmentResume(ctx, db.GetEnrollmentResumeParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return Enrollment{}, notFound("Прохождение")
		}
		return Enrollment{}, internal("Не удалось получить прохождение", err)
	}
	value := enrollmentFromResumeRow(row)
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: value.CourseID})
	if err != nil {
		return Enrollment{}, internal("Не удалось проверить курс прохождения", err)
	}
	if !canViewEnrollment(actor, value, courseFromRow(courseRow)) {
		return Enrollment{}, notFound("Прохождение")
	}
	if mutation && !canMutateEnrollment(actor, value) {
		return Enrollment{}, forbidden("Недостаточно прав для изменения этого прохождения")
	}
	return value, nil
}

func (s *Service) GetEnrollmentOutline(ctx context.Context, actor Actor, id uuid.UUID) (EnrollmentOutline, error) {
	queries := db.New(s.pool)
	enrollment, err := s.requireEnrollmentAccess(ctx, queries, actor, id, false)
	if err != nil {
		return EnrollmentOutline{}, err
	}
	lessonRows, err := queries.ListEnrollmentVersionLessonsWithQuiz(ctx, db.ListEnrollmentVersionLessonsWithQuizParams{
		CompanyID: actor.CompanyID, EnrollmentID: id,
	})
	if err != nil {
		return EnrollmentOutline{}, internal("Не удалось получить структуру прохождения", err)
	}
	sectionRows, err := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{
		CompanyID: actor.CompanyID, CourseVersionID: enrollment.CourseVersionID,
	})
	if err != nil {
		return EnrollmentOutline{}, internal("Не удалось получить разделы прохождения", err)
	}
	sections := make([]EnrollmentOutlineSection, len(sectionRows))
	sectionIndex := make(map[uuid.UUID]int, len(sectionRows))
	for index, row := range sectionRows {
		sections[index].CourseVersionSection = courseVersionSectionFromRow(row)
		sections[index].Lessons = []EnrollmentOutlineLesson{}
		sectionIndex[row.ID] = index
	}
	for _, row := range lessonRows {
		index, ok := sectionIndex[row.SectionVersionID]
		if !ok {
			return EnrollmentOutline{}, internal("Урок прохождения ссылается на неизвестный раздел", nil)
		}
		status := "locked"
		if row.ProgressStatus.Valid {
			status = row.ProgressStatus.String
		}
		lesson := enrollmentLessonFromVersionRow(row)
		sections[index].Lessons = append(sections[index].Lessons, EnrollmentOutlineLesson{
			CourseVersionLesson: lesson, Status: status,
			FirstOpenedAt: timestamptzPointer(row.FirstOpenedAt), CompletedAt: timestamptzPointer(row.CompletedAt),
		})
	}
	return EnrollmentOutline{Enrollment: enrollment, Sections: sections}, nil
}

func (s *Service) GetEnrollmentLesson(
	ctx context.Context,
	actor Actor,
	enrollmentID, lessonID uuid.UUID,
) (EnrollmentLesson, error) {
	queries := db.New(s.pool)
	enrollment, err := s.requireEnrollmentAccess(ctx, queries, actor, enrollmentID, false)
	if err != nil {
		return EnrollmentLesson{}, err
	}
	aggregate, err := s.loadEnrollmentAggregate(ctx, queries, actor.CompanyID, enrollmentID)
	if err != nil {
		return EnrollmentLesson{}, err
	}
	if err = aggregate.CanViewLesson(domainenrollment.ID(lessonID.String()), s.now().UTC()); err != nil {
		return EnrollmentLesson{}, enrollmentDomainError(err)
	}
	rows, err := queries.ListEnrollmentVersionLessonsWithQuiz(ctx, db.ListEnrollmentVersionLessonsWithQuizParams{
		CompanyID: actor.CompanyID, EnrollmentID: enrollmentID,
	})
	if err != nil {
		return EnrollmentLesson{}, internal("Не удалось получить урок прохождения", err)
	}
	for _, row := range rows {
		if row.ID != lessonID {
			continue
		}
		if !row.ProgressStatus.Valid {
			return EnrollmentLesson{}, forbidden("Будущий урок ещё не открыт")
		}
		lesson := enrollmentLessonFromVersionRow(row)
		var quiz *CourseVersionQuiz
		if row.QuizVersionID.Valid {
			quiz = &CourseVersionQuiz{
				ID: row.QuizVersionID.UUID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
				LessonVersionID: row.ID, Questions: append(json.RawMessage(nil), row.QuizQuestions...),
				PassingScore: row.QuizPassingScore, MaxAttempts: int4Pointer(row.QuizMaxAttempts),
			}
		}
		return EnrollmentLesson{
			Enrollment: enrollment, Lesson: lesson, Quiz: quiz,
			Progress: EnrollmentLessonProgress{
				CompanyID: row.CompanyID, EnrollmentID: enrollmentID, LessonVersionID: row.ID,
				Status: row.ProgressStatus.String, FirstOpenedAt: timestamptzPointer(row.FirstOpenedAt),
				CompletedAt: timestamptzPointer(row.CompletedAt), ActiveSeconds: row.ActiveSeconds,
				LastPosition: textPointer(row.LastPosition),
			},
		}, nil
	}
	return EnrollmentLesson{}, notFound("Урок прохождения")
}

func enrollmentLessonFromVersionRow(row db.ListEnrollmentVersionLessonsWithQuizRow) CourseVersionLesson {
	return CourseVersionLesson{
		ID: row.ID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
		SectionVersionID: row.SectionVersionID, StableKey: row.StableKey.String(), Title: row.Title,
		Order: row.Order, Content: append(json.RawMessage(nil), row.Content...), SourceType: row.SourceType,
		SourceArticleID: nullUUIDPointer(row.SourceArticleID), SourceArticleVersion: int4Pointer(row.SourceArticleVersion),
		SourceTemplateID: nullUUIDPointer(row.SourceTemplateID), SourceTemplateVersionID: nullUUIDPointer(row.SourceTemplateVersionID),
		EstimatedMinutes: int4Pointer(row.EstimatedMinutes), QuizVersionID: nullUUIDPointer(row.QuizVersionID),
	}
}

func (s *Service) ResumeEnrollment(ctx context.Context, actor Actor, id uuid.UUID) (Enrollment, *EnrollmentLesson, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Enrollment{}, nil, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	current, err := s.requireEnrollmentAccess(ctx, queries, actor, id, true)
	if err != nil {
		return Enrollment{}, nil, err
	}
	aggregate, err := s.loadEnrollmentAggregate(ctx, queries, actor.CompanyID, id)
	if err != nil {
		return Enrollment{}, nil, err
	}
	now := s.now().UTC()
	activated := current.AccessStatus == "ready" || current.AccessStatus == "invited"
	if activated {
		if err = aggregate.Activate(domainenrollment.Activation{At: now}); err != nil {
			return Enrollment{}, nil, enrollmentDomainError(err)
		}
	}
	decision := aggregate.Resume(now)
	snapshot := aggregate.Snapshot()
	if _, err = s.persistEnrollmentSnapshot(ctx, queries, snapshot, now); err != nil {
		return Enrollment{}, nil, err
	}
	if activated {
		if err = s.emitEnrollmentActivated(ctx, queries, actor, snapshot); err != nil {
			return Enrollment{}, nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Enrollment{}, nil, internal("Не удалось сохранить возобновление прохождения", err)
	}
	updated, err := s.GetEnrollment(ctx, actor, id)
	if err != nil {
		return Enrollment{}, nil, err
	}
	if decision.LessonVersionID == nil {
		return updated, nil, nil
	}
	lessonID, parseErr := uuid.Parse(string(*decision.LessonVersionID))
	if parseErr != nil {
		return Enrollment{}, nil, internal("Некорректный текущий урок прохождения", parseErr)
	}
	lesson, err := s.GetEnrollmentLesson(ctx, actor, id, lessonID)
	if err != nil {
		return Enrollment{}, nil, err
	}
	return updated, &lesson, nil
}

type CompleteEnrollmentLessonInput struct {
	EnrollmentID   uuid.UUID
	LessonID       uuid.UUID
	ActiveSeconds  int64
	LastPosition   *string
	IdempotencyKey string
}

func (s *Service) CompleteEnrollmentLesson(
	ctx context.Context,
	actor Actor,
	input CompleteEnrollmentLessonInput,
) (EnrollmentProgressSnapshot, error) {
	return s.completeEnrollmentLesson(ctx, actor, input, nil)
}

func (s *Service) completeEnrollmentLesson(
	ctx context.Context,
	actor Actor,
	input CompleteEnrollmentLessonInput,
	external *ExternalPrincipal,
) (EnrollmentProgressSnapshot, error) {
	key, err := normalizeEnrollmentIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return EnrollmentProgressSnapshot{}, err
	}
	requestHash := enrollmentMutationRequestHash(struct {
		EnrollmentID  uuid.UUID `json:"enrollmentId"`
		LessonID      uuid.UUID `json:"lessonId"`
		ActiveSeconds int64     `json:"activeSeconds"`
		LastPosition  *string   `json:"lastPosition"`
	}{input.EnrollmentID, input.LessonID, input.ActiveSeconds, input.LastPosition})
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return EnrollmentProgressSnapshot{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if _, err = s.requireEnrollmentAccess(ctx, queries, actor, input.EnrollmentID, true); err != nil {
		return EnrollmentProgressSnapshot{}, err
	}
	now := s.now().UTC()
	reservation, err := s.reserveEnrollmentMutationInTx(
		ctx, queries, actor, input.EnrollmentID, enrollmentOperationCompleteLesson, key, requestHash, now,
	)
	if err != nil {
		return EnrollmentProgressSnapshot{}, err
	}
	if reservation.CompletedAt.Valid {
		if err = tx.Commit(ctx); err != nil {
			return EnrollmentProgressSnapshot{}, internal("Не удалось завершить повторную операцию урока", err)
		}
		return s.getEnrollmentProgressSnapshot(ctx, actor, input.EnrollmentID)
	}
	if err = s.completeEnrollmentLessonInTx(ctx, queries, actor, input, external); err != nil {
		return EnrollmentProgressSnapshot{}, err
	}
	if _, err = queries.CompleteEnrollmentMutationIdempotency(ctx, db.CompleteEnrollmentMutationIdempotencyParams{
		ResultID: nullUUID(nil), CompletedAt: nullTimestamptz(&now),
		CompanyID: actor.CompanyID, ID: reservation.ID,
	}); err != nil {
		return EnrollmentProgressSnapshot{}, internal("Не удалось сохранить результат идемпотентного завершения урока", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return EnrollmentProgressSnapshot{}, internal("Не удалось сохранить прогресс урока", err)
	}
	if external != nil {
		return EnrollmentProgressSnapshot{}, nil
	}
	return s.getEnrollmentProgressSnapshot(ctx, actor, input.EnrollmentID)
}

func (s *Service) completeEnrollmentLessonInTx(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	input CompleteEnrollmentLessonInput,
	external *ExternalPrincipal,
) error {
	var err error
	if external == nil {
		if _, err = s.requireEnrollmentAccess(ctx, queries, actor, input.EnrollmentID, true); err != nil {
			return err
		}
	} else {
		_, err = queries.GetExternalEnrollmentForMutationForUpdate(ctx, db.GetExternalEnrollmentForMutationForUpdateParams{
			CompanyID: external.CompanyID, EnrollmentID: input.EnrollmentID, SessionID: external.SessionID, Now: s.now().UTC(),
		})
		if err != nil {
			return forbidden("Урок недоступен во внешней сессии")
		}
	}
	aggregate, err := s.loadEnrollmentAggregate(ctx, queries, actor.CompanyID, input.EnrollmentID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	lessonID := domainenrollment.ID(input.LessonID.String())
	if input.ActiveSeconds > 0 || input.LastPosition != nil {
		if err = aggregate.RecordPosition(lessonID, input.ActiveSeconds, input.LastPosition, now); err != nil {
			return enrollmentDomainError(err)
		}
	}
	_, completed, err := aggregate.CompleteLesson(lessonID, now)
	if err != nil {
		return enrollmentDomainError(err)
	}
	snapshot := aggregate.Snapshot()
	if _, err = s.persistEnrollmentSnapshot(ctx, queries, snapshot, now); err != nil {
		return err
	}
	if err = s.emitEnrollmentProgressed(ctx, queries, actor, snapshot, &input.LessonID, nil, now); err != nil {
		return err
	}
	if completed {
		if err = s.emitEnrollmentCompleted(ctx, queries, actor, snapshot); err != nil {
			return err
		}
	}
	return nil
}

type SubmitEnrollmentQuizInput struct {
	EnrollmentID   uuid.UUID
	QuizID         uuid.UUID
	Answers        []EnrollmentQuizAnswer
	ActiveSeconds  int64
	LastPosition   *string
	IdempotencyKey string
}

func (s *Service) SubmitEnrollmentQuizAttempt(
	ctx context.Context,
	actor Actor,
	input SubmitEnrollmentQuizInput,
) (EnrollmentQuizAttempt, EnrollmentProgressSnapshot, error) {
	return s.submitEnrollmentQuizAttempt(ctx, actor, input, nil)
}

func (s *Service) submitEnrollmentQuizAttempt(
	ctx context.Context,
	actor Actor,
	input SubmitEnrollmentQuizInput,
	external *ExternalPrincipal,
) (EnrollmentQuizAttempt, EnrollmentProgressSnapshot, error) {
	key, err := normalizeEnrollmentIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	requestHash := enrollmentMutationRequestHash(struct {
		EnrollmentID  uuid.UUID              `json:"enrollmentId"`
		QuizID        uuid.UUID              `json:"quizId"`
		Answers       []EnrollmentQuizAnswer `json:"answers"`
		ActiveSeconds int64                  `json:"activeSeconds"`
		LastPosition  *string                `json:"lastPosition"`
	}{input.EnrollmentID, input.QuizID, input.Answers, input.ActiveSeconds, input.LastPosition})
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if _, err = s.requireEnrollmentAccess(ctx, queries, actor, input.EnrollmentID, true); err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	now := s.now().UTC()
	reservation, err := s.reserveEnrollmentMutationInTx(
		ctx, queries, actor, input.EnrollmentID, enrollmentOperationSubmitQuiz, key, requestHash, now,
	)
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	replayed := reservation.CompletedAt.Valid
	attemptID := uuid.New()
	var created EnrollmentQuizAttempt
	if replayed {
		if !reservation.ResultID.Valid {
			return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal(
				"У сохранённого результата теста отсутствует идентификатор попытки", nil,
			)
		}
		attemptID = reservation.ResultID.UUID
	} else {
		created, err = s.submitEnrollmentQuizAttemptInTx(ctx, queries, actor, input, external, attemptID)
		if err != nil {
			return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
		}
		if _, err = queries.CompleteEnrollmentMutationIdempotency(ctx, db.CompleteEnrollmentMutationIdempotencyParams{
			ResultID: nullUUID(&attemptID), CompletedAt: nullTimestamptz(&now),
			CompanyID: actor.CompanyID, ID: reservation.ID,
		}); err != nil {
			return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal(
				"Не удалось сохранить результат идемпотентной отправки теста", err,
			)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal("Не удалось сохранить результат теста", err)
	}
	if external != nil {
		return created, EnrollmentProgressSnapshot{}, nil
	}
	progress, err := s.getEnrollmentProgressSnapshot(ctx, actor, input.EnrollmentID)
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	if replayed {
		for _, attempt := range progress.QuizAttempts {
			if attempt.ID == attemptID {
				return attempt, progress, nil
			}
		}
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal(
			"Сохранённая попытка теста не найдена", nil,
		)
	}
	return created, progress, nil
}

func (s *Service) submitEnrollmentQuizAttemptInTx(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	input SubmitEnrollmentQuizInput,
	external *ExternalPrincipal,
	attemptID uuid.UUID,
) (EnrollmentQuizAttempt, error) {
	var err error
	if external == nil {
		if _, err = s.requireEnrollmentAccess(ctx, queries, actor, input.EnrollmentID, true); err != nil {
			return EnrollmentQuizAttempt{}, err
		}
	} else {
		_, err = queries.GetExternalEnrollmentForMutationForUpdate(ctx, db.GetExternalEnrollmentForMutationForUpdateParams{
			CompanyID: external.CompanyID, EnrollmentID: input.EnrollmentID, SessionID: external.SessionID, Now: s.now().UTC(),
		})
		if err != nil {
			return EnrollmentQuizAttempt{}, forbidden("Тест недоступен во внешней сессии")
		}
	}
	quiz, err := queries.GetCourseVersionQuizForEnrollment(ctx, db.GetCourseVersionQuizForEnrollmentParams{
		CompanyID: actor.CompanyID, EnrollmentID: input.EnrollmentID, QuizVersionID: input.QuizID,
	})
	if err != nil {
		if isNoRows(err) {
			return EnrollmentQuizAttempt{}, notFound("Тест прохождения")
		}
		return EnrollmentQuizAttempt{}, internal("Не удалось получить тест прохождения", err)
	}
	aggregate, err := s.loadEnrollmentAggregate(ctx, queries, actor.CompanyID, input.EnrollmentID)
	if err != nil {
		return EnrollmentQuizAttempt{}, err
	}
	now := s.now().UTC()
	lessonID := domainenrollment.ID(quiz.LessonVersionID.String())
	if input.ActiveSeconds > 0 || input.LastPosition != nil {
		if err = aggregate.RecordPosition(lessonID, input.ActiveSeconds, input.LastPosition, now); err != nil {
			return EnrollmentQuizAttempt{}, enrollmentDomainError(err)
		}
	}
	score, pendingReview, err := evaluateEnrollmentQuiz(quiz.Questions, input.Answers)
	if err != nil {
		return EnrollmentQuizAttempt{}, err
	}
	maxAttempts := optionalInt(int4Pointer(quiz.MaxAttempts))
	outcome, err := aggregate.SubmitQuiz(domainenrollment.QuizSubmission{
		AttemptID: domainenrollment.ID(attemptID.String()), LessonID: lessonID,
		QuizVersionID: domainenrollment.ID(input.QuizID.String()), Score: score,
		PassingScore: int(quiz.PassingScore), MaxAttempts: maxAttempts, PendingReview: pendingReview, At: now,
	})
	if err != nil {
		return EnrollmentQuizAttempt{}, enrollmentDomainError(err)
	}
	encodedAnswers, _ := json.Marshal(input.Answers)
	createdRow, err := queries.CreateEnrollmentQuizAttempt(ctx, db.CreateEnrollmentQuizAttemptParams{
		ID: attemptID, Answers: encodedAnswers, Score: int32(score),
		Passed: outcome.Decision == domainenrollment.QuizPassed, PendingReview: pendingReview,
		CreatedAt: now, CompanyID: actor.CompanyID, EnrollmentID: input.EnrollmentID, QuizVersionID: input.QuizID,
	})
	if err != nil {
		return EnrollmentQuizAttempt{}, internal("Не удалось сохранить попытку теста", err)
	}
	snapshot := aggregate.Snapshot()
	if _, err = s.persistEnrollmentSnapshot(ctx, queries, snapshot, now); err != nil {
		return EnrollmentQuizAttempt{}, err
	}
	completedLessonID, convertErr := optionalUUIDFromEnrollmentID(outcome.CompletedLessonID)
	if convertErr != nil {
		return EnrollmentQuizAttempt{}, internal("Некорректный завершённый урок", convertErr)
	}
	if err = s.emitEnrollmentProgressed(ctx, queries, actor, snapshot, completedLessonID, &attemptID, now); err != nil {
		return EnrollmentQuizAttempt{}, err
	}
	if outcome.EnrollmentComplete {
		if err = s.emitEnrollmentCompleted(ctx, queries, actor, snapshot); err != nil {
			return EnrollmentQuizAttempt{}, err
		}
	}
	created := enrollmentQuizAttemptFromCreateRow(createdRow)
	created.AttemptNumber = int32(outcome.AttemptNumber)
	return created, nil
}

// ReviewEnrollmentQuizAttempt resolves an open-answer attempt that is stuck in
// pending_review and, on pass, completes the lesson the same way a closed quiz would.
func (s *Service) ReviewEnrollmentQuizAttempt(
	ctx context.Context,
	actor Actor,
	input ReviewEnrollmentQuizInput,
) (EnrollmentQuizAttempt, EnrollmentProgressSnapshot, error) {
	if !actor.canManage() && actor.Role != "partner" {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, forbidden("Недостаточно прав для проверки открытых ответов")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	enrollment, err := s.requireEnrollmentAccess(ctx, queries, actor, input.EnrollmentID, false)
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	if actor.Role == "partner" {
		courseRow, courseErr := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: enrollment.CourseID})
		if courseErr != nil {
			return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal("Не удалось проверить курс прохождения", courseErr)
		}
		course := courseFromRow(courseRow)
		if course.OwnerType != "partner" || course.OwnerUserID == nil || *course.OwnerUserID != actor.UserID {
			return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, forbidden("Недостаточно прав для проверки открытых ответов")
		}
	}
	aggregate, err := s.loadEnrollmentAggregate(ctx, queries, actor.CompanyID, input.EnrollmentID)
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	now := s.now().UTC()
	outcome, err := aggregate.ReviewAttempt(domainenrollment.Review{
		AttemptID: domainenrollment.ID(input.AttemptID.String()),
		ActorID:   domainenrollment.ID(actor.UserID.String()),
		Passed:    input.Passed,
		Comment:   input.Comment,
		At:        now,
	})
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, enrollmentDomainError(err)
	}
	score := int32(0)
	for _, attempt := range aggregate.Snapshot().QuizAttempts {
		if attempt.ID == domainenrollment.ID(input.AttemptID.String()) {
			score = int32(attempt.Score)
			break
		}
	}
	reviewedRow, err := queries.ReviewEnrollmentQuizAttempt(ctx, db.ReviewEnrollmentQuizAttemptParams{
		Passed: input.Passed, Score: score, ReviewedByID: nullUUID(&actor.UserID),
		ReviewedAt: nullTimestamptz(&now), ReviewComment: nullText(input.Comment),
		CompanyID: actor.CompanyID, EnrollmentID: input.EnrollmentID, ID: input.AttemptID,
	})
	if err != nil {
		if isNoRows(err) {
			return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, notFound("Попытка теста")
		}
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal("Не удалось сохранить результат проверки", err)
	}
	snapshot := aggregate.Snapshot()
	if _, err = s.persistEnrollmentSnapshot(ctx, queries, snapshot, now); err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	completedLessonID, convertErr := optionalUUIDFromEnrollmentID(outcome.CompletedLessonID)
	if convertErr != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal("Некорректный завершённый урок", convertErr)
	}
	if err = s.emitEnrollmentProgressed(ctx, queries, actor, snapshot, completedLessonID, &input.AttemptID, now); err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	if outcome.EnrollmentComplete {
		if err = s.emitEnrollmentCompleted(ctx, queries, actor, snapshot); err != nil {
			return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
		}
	}
	if err = s.auditMutation(ctx, queries, actor, "quiz_attempt_reviewed", "quiz_attempt", input.AttemptID,
		map[string]any{"pendingReview": true},
		map[string]any{"pendingReview": false, "passed": input.Passed, "enrollmentId": input.EnrollmentID}); err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, internal("Не удалось сохранить проверку теста", err)
	}
	progress, err := s.getEnrollmentProgressSnapshot(ctx, actor, input.EnrollmentID)
	if err != nil {
		return EnrollmentQuizAttempt{}, EnrollmentProgressSnapshot{}, err
	}
	reviewed := enrollmentQuizAttemptFromReviewRow(reviewedRow)
	reviewed.AttemptNumber = int32(outcome.AttemptNumber)
	return reviewed, progress, nil
}

func (s *Service) getEnrollmentProgressSnapshot(
	ctx context.Context,
	actor Actor,
	id uuid.UUID,
) (EnrollmentProgressSnapshot, error) {
	enrollment, err := s.GetEnrollment(ctx, actor, id)
	if err != nil {
		return EnrollmentProgressSnapshot{}, err
	}
	queries := db.New(s.pool)
	lessonRows, err := queries.ListEnrollmentLessonProgress(ctx, db.ListEnrollmentLessonProgressParams{
		CompanyID: actor.CompanyID, EnrollmentID: id,
	})
	if err != nil {
		return EnrollmentProgressSnapshot{}, internal("Не удалось получить прогресс уроков", err)
	}
	lessons := make([]EnrollmentLessonProgress, len(lessonRows))
	for index := range lessonRows {
		lessons[index] = enrollmentLessonProgressFromRow(lessonRows[index])
	}
	attemptRows, err := queries.ListEnrollmentQuizAttempts(ctx, db.ListEnrollmentQuizAttemptsParams{
		CompanyID: actor.CompanyID, EnrollmentID: id,
	})
	if err != nil {
		return EnrollmentProgressSnapshot{}, internal("Не удалось получить попытки тестов", err)
	}
	attempts := make([]EnrollmentQuizAttempt, len(attemptRows))
	numbers := make(map[uuid.UUID]int32)
	for index := len(attemptRows) - 1; index >= 0; index-- {
		attempt := enrollmentQuizAttemptFromListRow(attemptRows[index])
		numbers[attempt.QuizVersionID]++
		attempt.AttemptNumber = numbers[attempt.QuizVersionID]
		attempts[index] = attempt
	}
	return EnrollmentProgressSnapshot{Enrollment: enrollment, Lessons: lessons, QuizAttempts: attempts}, nil
}

func (s *Service) GetEnrollmentReport(ctx context.Context, actor Actor, id uuid.UUID) (EnrollmentReport, error) {
	if _, err := s.GetEnrollment(ctx, actor, id); err != nil {
		return EnrollmentReport{}, err
	}
	snapshot, err := s.getEnrollmentProgressSnapshot(ctx, actor, id)
	if err != nil {
		return EnrollmentReport{}, err
	}
	reportRows, err := db.New(s.pool).ListInternalEnrollmentReports(ctx, db.ListInternalEnrollmentReportsParams{
		Now: s.now().UTC(), CompanyID: actor.CompanyID,
		UserID: nullUUID(snapshot.Enrollment.UserID), CourseID: nullUUID(&snapshot.Enrollment.CourseID),
	})
	if err != nil {
		return EnrollmentReport{}, internal("Не удалось получить срок прохождения", err)
	}
	for _, reportRow := range reportRows {
		if reportRow.EnrollmentID == id {
			snapshot.Enrollment.DueDate = timestamptzPointer(reportRow.DueDate)
			snapshot.Enrollment.Overdue, _ = reportRow.Overdue.(bool)
			break
		}
	}
	activeSeconds := int64(0)
	for _, lesson := range snapshot.Lessons {
		activeSeconds += lesson.ActiveSeconds
	}
	versionRow, err := db.New(s.pool).GetCourseVersion(ctx, db.GetCourseVersionParams{
		CompanyID: actor.CompanyID, ID: snapshot.Enrollment.CourseVersionID,
	})
	if err != nil {
		return EnrollmentReport{}, internal("Не удалось получить версию отчёта", err)
	}
	return EnrollmentReport{
		Enrollment: snapshot.Enrollment, Version: courseVersionFromRow(versionRow), Lessons: snapshot.Lessons,
		QuizAttempts: snapshot.QuizAttempts, ActiveSeconds: activeSeconds,
	}, nil
}

func (s *Service) loadEnrollmentAggregate(
	ctx context.Context,
	queries *db.Queries,
	companyID, enrollmentID uuid.UUID,
) (*domainenrollment.Enrollment, error) {
	row, err := queries.GetEnrollmentForUpdate(ctx, db.GetEnrollmentForUpdateParams{CompanyID: companyID, ID: enrollmentID})
	if err != nil {
		if isNoRows(err) {
			return nil, notFound("Прохождение")
		}
		return nil, internal("Не удалось заблокировать прохождение", err)
	}
	version, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: companyID, ID: row.CourseVersionID})
	if err != nil {
		return nil, internal("Не удалось получить версию прохождения", err)
	}
	lessonRows, err := queries.ListEnrollmentVersionLessonsWithQuiz(ctx, db.ListEnrollmentVersionLessonsWithQuizParams{
		CompanyID: companyID, EnrollmentID: enrollmentID,
	})
	if err != nil {
		return nil, internal("Не удалось получить уроки прохождения", err)
	}
	lessons := make([]domainenrollment.LessonSpec, len(lessonRows))
	quizLessons := make(map[uuid.UUID]uuid.UUID)
	progress := make([]domainenrollment.LessonProgress, 0, len(lessonRows))
	for index, lesson := range lessonRows {
		lessons[index] = domainenrollment.LessonSpec{ID: domainenrollment.ID(lesson.ID.String())}
		if lesson.QuizVersionID.Valid {
			quizID := domainenrollment.ID(lesson.QuizVersionID.UUID.String())
			lessons[index].QuizID = &quizID
			quizLessons[lesson.QuizVersionID.UUID] = lesson.ID
		}
		if row.ProgressStatus != "not_started" && lesson.ProgressStatus.Valid {
			progress = append(progress, domainenrollment.LessonProgress{
				LessonVersionID: domainenrollment.ID(lesson.ID.String()), Status: domainenrollment.LessonStatus(lesson.ProgressStatus.String),
				FirstOpenedAt: timestamptzPointer(lesson.FirstOpenedAt), CompletedAt: timestamptzPointer(lesson.CompletedAt),
				ActiveSeconds: lesson.ActiveSeconds, LastPosition: textPointer(lesson.LastPosition),
			})
		}
	}
	attemptRows, err := queries.ListEnrollmentQuizAttempts(ctx, db.ListEnrollmentQuizAttemptsParams{
		CompanyID: companyID, EnrollmentID: enrollmentID,
	})
	if err != nil {
		return nil, internal("Не удалось получить попытки прохождения", err)
	}
	attempts := make([]domainenrollment.QuizAttempt, 0, len(attemptRows))
	numbers := make(map[uuid.UUID]int)
	for index := len(attemptRows) - 1; index >= 0; index-- {
		attempt := attemptRows[index]
		numbers[attempt.QuizVersionID]++
		attempts = append(attempts, domainenrollment.QuizAttempt{
			ID: domainenrollment.ID(attempt.ID.String()), QuizVersionID: domainenrollment.ID(attempt.QuizVersionID.String()),
			LessonID: domainenrollment.ID(quizLessons[attempt.QuizVersionID].String()), Number: numbers[attempt.QuizVersionID],
			Score: int(attempt.Score), Passed: attempt.Passed, PendingReview: attempt.PendingReview, CreatedAt: attempt.CreatedAt,
		})
	}
	snapshot := domainenrollment.Snapshot{
		ID: domainenrollment.ID(row.ID.String()), CompanyID: domainenrollment.ID(row.CompanyID.String()),
		CourseID: domainenrollment.ID(row.CourseID.String()), CourseVersionID: domainenrollment.ID(row.CourseVersionID.String()),
		LearnerType: domainenrollment.LearnerType(row.LearnerType), UserID: optionalEnrollmentID(nullUUIDPointer(row.UserID)),
		ExternalID: optionalEnrollmentID(nullUUIDPointer(row.ExternalLearnerID)), SourceType: domainenrollment.SourceType(row.SourceType),
		SourceID: optionalEnrollmentID(nullUUIDPointer(row.SourceID)), AttemptNumber: int(row.AttemptNumber),
		ProgressStatus: domainenrollment.ProgressStatus(row.ProgressStatus), AccessStatus: domainenrollment.AccessStatus(row.AccessStatus),
		Sequential: version.Sequential, Lessons: lessons, LessonProgress: progress, QuizAttempts: attempts,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), AccessUntil: timestamptzPointer(row.AccessUntil),
		StartedAt: timestamptzPointer(row.StartedAt), CompletedAt: timestamptzPointer(row.CompletedAt),
		LastActivityAt: timestamptzPointer(row.LastActivityAt), FrozenAt: timestamptzPointer(row.FrozenAt),
		SuspendedAt:    timestamptzPointer(row.SuspendedAt),
		PreviousAccess: optionalEnrollmentAccess(textPointer(row.RestrictionPreviousAccessStatus)), CreatedAt: row.CreatedAt,
	}
	aggregate, err := domainenrollment.Rehydrate(snapshot)
	if err != nil {
		return nil, internal("Некорректное состояние прохождения", err)
	}
	return aggregate, nil
}

func optionalEnrollmentAccess(value *string) *domainenrollment.AccessStatus {
	if value == nil {
		return nil
	}
	converted := domainenrollment.AccessStatus(*value)
	return &converted
}

func optionalEnrollmentID(value *uuid.UUID) *domainenrollment.ID {
	if value == nil {
		return nil
	}
	converted := domainenrollment.ID(value.String())
	return &converted
}

func (s *Service) persistEnrollmentSnapshot(
	ctx context.Context,
	queries *db.Queries,
	snapshot domainenrollment.Snapshot,
	now time.Time,
) (Enrollment, error) {
	companyID, err := uuid.Parse(string(snapshot.CompanyID))
	if err != nil {
		return Enrollment{}, internal("Некорректная компания прохождения", err)
	}
	enrollmentID, err := uuid.Parse(string(snapshot.ID))
	if err != nil {
		return Enrollment{}, internal("Некорректный идентификатор прохождения", err)
	}
	var currentID *uuid.UUID
	for _, progress := range snapshot.LessonProgress {
		lessonID, parseErr := uuid.Parse(string(progress.LessonVersionID))
		if parseErr != nil {
			return Enrollment{}, internal("Некорректный урок прохождения", parseErr)
		}
		if progress.Status == domainenrollment.LessonCurrent {
			currentID = &lessonID
		}
		if _, upsertErr := queries.UpsertEnrollmentLessonProgress(ctx, db.UpsertEnrollmentLessonProgressParams{
			CompanyID: companyID, EnrollmentID: enrollmentID, LessonVersionID: lessonID,
			Status: string(progress.Status), FirstOpenedAt: nullTimestamptz(progress.FirstOpenedAt),
			CompletedAt: nullTimestamptz(progress.CompletedAt), ActiveSeconds: progress.ActiveSeconds,
			LastPosition: nullText(progress.LastPosition),
		}); upsertErr != nil {
			return Enrollment{}, internal("Не удалось сохранить прогресс урока", upsertErr)
		}
	}
	updated, err := queries.UpdateEnrollmentResumeStatus(ctx, db.UpdateEnrollmentResumeStatusParams{
		ProgressStatus: string(snapshot.ProgressStatus), AccessStatus: string(snapshot.AccessStatus),
		CurrentLessonVersionID: nullUUID(currentID), ActivatedAt: nullTimestamptz(snapshot.ActivatedAt),
		StartedAt: nullTimestamptz(snapshot.StartedAt), CompletedAt: nullTimestamptz(snapshot.CompletedAt),
		LastActivityAt: nullTimestamptz(snapshot.LastActivityAt), UpdatedAt: now,
		CompanyID: companyID, ID: enrollmentID,
	})
	if err != nil {
		return Enrollment{}, internal("Не удалось обновить состояние прохождения", err)
	}
	return enrollmentFromRow(updated), nil
}

func evaluateEnrollmentQuiz(raw json.RawMessage, answers []EnrollmentQuizAnswer) (int, bool, error) {
	var questions []storedQuizQuestion
	if err := json.Unmarshal(raw, &questions); err != nil {
		return 0, false, internal("Не удалось прочитать вопросы теста", err)
	}
	provided := make(map[string]EnrollmentQuizAnswer, len(answers))
	for _, answer := range answers {
		if answer.QuestionID == "" {
			return 0, false, validation("В ответе не указан вопрос")
		}
		if _, duplicate := provided[answer.QuestionID]; duplicate {
			return 0, false, validation("Ответ на вопрос передан повторно")
		}
		provided[answer.QuestionID] = answer
	}
	closed, correct := 0, 0
	pendingReview := false
	known := make(map[string]struct{}, len(questions))
	for _, question := range questions {
		known[question.ID] = struct{}{}
		if question.Type == "open" {
			pendingReview = true
			continue
		}
		closed++
		answer := provided[question.ID]
		expected := make([]string, 0)
		validOptions := make(map[string]struct{}, len(question.Options))
		for _, option := range question.Options {
			validOptions[option.ID] = struct{}{}
			if option.Correct {
				expected = append(expected, option.ID)
			}
		}
		for _, selected := range answer.SelectedOptionIDs {
			if _, ok := validOptions[selected]; !ok {
				return 0, false, validation("Выбран неизвестный вариант ответа")
			}
		}
		if sameStringSet(expected, answer.SelectedOptionIDs) {
			correct++
		}
	}
	for questionID := range provided {
		if _, ok := known[questionID]; !ok {
			return 0, false, validation("Ответ относится к неизвестному вопросу")
		}
	}
	if closed == 0 {
		return 0, pendingReview, nil
	}
	return correct * 100 / closed, pendingReview, nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy, rightCopy := append([]string(nil), left...), append([]string(nil), right...)
	slices.Sort(leftCopy)
	slices.Sort(rightCopy)
	return slices.Equal(leftCopy, rightCopy)
}

func optionalUUIDFromEnrollmentID(value *domainenrollment.ID) (*uuid.UUID, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := uuid.Parse(string(*value))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (s *Service) emitEnrollmentActivated(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	snapshot domainenrollment.Snapshot,
) error {
	if snapshot.ActivatedAt == nil {
		return internal("У активированного прохождения отсутствует время активации", nil)
	}
	payload := &eventsv1.AcademyEnrollmentActivatedPayload{
		EnrollmentId: string(snapshot.ID), CourseId: string(snapshot.CourseID), CourseVersionId: string(snapshot.CourseVersionID),
		LearnerType: enrollmentLearnerTypeToEvent(snapshot.LearnerType), SourceType: enrollmentSourceTypeToEvent(snapshot.SourceType),
		AttemptNumber: uint32(max(0, snapshot.AttemptNumber)), ActivatedAt: timestamppb.New(snapshot.ActivatedAt.UTC()),
	}
	payload.UserId = optionalEventEnrollmentID(snapshot.UserID)
	payload.ExternalLearnerId = optionalEventEnrollmentID(snapshot.ExternalID)
	payload.SourceId = optionalEventEnrollmentID(snapshot.SourceID)
	if snapshot.AccessUntil != nil {
		payload.AccessUntil = timestamppb.New(snapshot.AccessUntil.UTC())
	}
	enrollmentID, _ := uuid.Parse(string(snapshot.ID))
	return s.emit(ctx, queries, actor.CompanyID, enrollmentID, actor.UserID,
		"teamos.academy.enrollment.activated.v1", payload)
}

func (s *Service) emitEnrollmentProgressed(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	snapshot domainenrollment.Snapshot,
	completedLessonID, quizAttemptID *uuid.UUID,
	at time.Time,
) error {
	payload := &eventsv1.AcademyEnrollmentProgressedPayload{
		EnrollmentId: string(snapshot.ID), CourseId: string(snapshot.CourseID), CourseVersionId: string(snapshot.CourseVersionID),
		LearnerType:    enrollmentLearnerTypeToEvent(snapshot.LearnerType),
		ProgressStatus: enrollmentProgressStatusToEvent(snapshot.ProgressStatus),
		OccurredAt:     timestamppb.New(at.UTC()),
	}
	// ProgressPercent is derived from the aggregate snapshot without trusting a
	// client-supplied percentage.
	if rehydrated, err := domainenrollment.Rehydrate(snapshot); err == nil {
		payload.ProgressPercent = uint32(rehydrated.ProgressPercent())
	}
	payload.UserId = optionalEventEnrollmentID(snapshot.UserID)
	payload.ExternalLearnerId = optionalEventEnrollmentID(snapshot.ExternalID)
	if completedLessonID != nil {
		value := completedLessonID.String()
		payload.CompletedLessonVersionId = &value
	}
	for _, progress := range snapshot.LessonProgress {
		if progress.Status == domainenrollment.LessonCurrent {
			value := string(progress.LessonVersionID)
			payload.CurrentLessonVersionId = &value
			break
		}
	}
	if quizAttemptID != nil {
		value := quizAttemptID.String()
		payload.QuizAttemptId = &value
	}
	enrollmentID, _ := uuid.Parse(string(snapshot.ID))
	return s.emit(ctx, queries, actor.CompanyID, enrollmentID, actor.UserID,
		"teamos.academy.enrollment.progressed.v1", payload)
}

func (s *Service) emitEnrollmentCompleted(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	snapshot domainenrollment.Snapshot,
) error {
	if snapshot.CompletedAt == nil {
		return internal("У завершённого прохождения отсутствует время завершения", nil)
	}
	payload := &eventsv1.AcademyEnrollmentCompletedPayload{
		EnrollmentId: string(snapshot.ID), CourseId: string(snapshot.CourseID), CourseVersionId: string(snapshot.CourseVersionID),
		LearnerType: enrollmentLearnerTypeToEvent(snapshot.LearnerType), AttemptNumber: uint32(max(0, snapshot.AttemptNumber)),
		CompletedAt: timestamppb.New(snapshot.CompletedAt.UTC()),
	}
	payload.UserId = optionalEventEnrollmentID(snapshot.UserID)
	payload.ExternalLearnerId = optionalEventEnrollmentID(snapshot.ExternalID)
	enrollmentID, _ := uuid.Parse(string(snapshot.ID))
	return s.emit(ctx, queries, actor.CompanyID, enrollmentID, actor.UserID,
		"teamos.academy.enrollment.completed.v1", payload)
}

func optionalEventEnrollmentID(value *domainenrollment.ID) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

func enrollmentLearnerTypeToEvent(value domainenrollment.LearnerType) eventsv1.AcademyEnrollmentLearnerType {
	if value == domainenrollment.LearnerUser {
		return eventsv1.AcademyEnrollmentLearnerType_ACADEMY_ENROLLMENT_LEARNER_TYPE_USER
	}
	if value == domainenrollment.LearnerExternal {
		return eventsv1.AcademyEnrollmentLearnerType_ACADEMY_ENROLLMENT_LEARNER_TYPE_EXTERNAL
	}
	return eventsv1.AcademyEnrollmentLearnerType_ACADEMY_ENROLLMENT_LEARNER_TYPE_UNSPECIFIED
}

func enrollmentSourceTypeToEvent(value domainenrollment.SourceType) eventsv1.AcademyEnrollmentSourceType {
	switch value {
	case domainenrollment.SourceAssignment:
		return eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_ASSIGNMENT
	case domainenrollment.SourcePersonalAccess:
		return eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_PERSONAL_ACCESS
	case domainenrollment.SourcePartnerPromoCampaign:
		return eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_PARTNER_PROMO_CAMPAIGN
	case domainenrollment.SourceCompanyCandidateCampaign:
		return eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_COMPANY_CANDIDATE_CAMPAIGN
	case domainenrollment.SourceRepeatTraining:
		return eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_REPEAT_TRAINING
	case domainenrollment.SourceSelfEnrollment:
		return eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_SELF_ENROLLMENT
	case domainenrollment.SourceLegacy:
		return eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_LEGACY
	default:
		return eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_UNSPECIFIED
	}
}

func enrollmentProgressStatusToEvent(value domainenrollment.ProgressStatus) eventsv1.AcademyEnrollmentProgressStatus {
	switch value {
	case domainenrollment.ProgressNotStarted:
		return eventsv1.AcademyEnrollmentProgressStatus_ACADEMY_ENROLLMENT_PROGRESS_STATUS_NOT_STARTED
	case domainenrollment.ProgressInProgress:
		return eventsv1.AcademyEnrollmentProgressStatus_ACADEMY_ENROLLMENT_PROGRESS_STATUS_IN_PROGRESS
	case domainenrollment.ProgressCompleted:
		return eventsv1.AcademyEnrollmentProgressStatus_ACADEMY_ENROLLMENT_PROGRESS_STATUS_COMPLETED
	default:
		return eventsv1.AcademyEnrollmentProgressStatus_ACADEMY_ENROLLMENT_PROGRESS_STATUS_UNSPECIFIED
	}
}

func enrollmentDomainError(err error) error {
	switch {
	case errors.Is(err, domainenrollment.ErrEnrollmentRevoked),
		errors.Is(err, domainenrollment.ErrEnrollmentClosed),
		errors.Is(err, domainenrollment.ErrContentUnavailable),
		errors.Is(err, domainenrollment.ErrFutureContentUnavailable):
		return forbidden(err.Error())
	case errors.Is(err, domainenrollment.ErrEnrollmentAlreadyCompleted),
		errors.Is(err, domainenrollment.ErrLessonAlreadyCompleted),
		errors.Is(err, domainenrollment.ErrQuizAttemptLimit),
		errors.Is(err, domainenrollment.ErrQuizPendingReview):
		return conflict(err.Error())
	case errors.Is(err, domainenrollment.ErrAttemptNotFound):
		return notFound("Попытка теста")
	case errors.Is(err, domainenrollment.ErrAttemptNotPending):
		return conflict(err.Error())
	default:
		return validation(err.Error())
	}
}
