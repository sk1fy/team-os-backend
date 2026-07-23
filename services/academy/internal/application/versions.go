package application

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	domainversion "github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) GetCourseVersions(ctx context.Context, actor Actor, courseID uuid.UUID) ([]CourseVersion, error) {
	queries := db.New(s.pool)
	if _, err := s.requireCourseVersionReadAccess(ctx, queries, actor, courseID); err != nil {
		return nil, err
	}
	rows, err := queries.GetCourseVersions(ctx, db.GetCourseVersionsParams{
		CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		return nil, internal("Не удалось получить версии курса", err)
	}
	return courseVersionsFromRows(rows), nil
}

func (s *Service) GetCourseVersion(
	ctx context.Context,
	actor Actor,
	courseID, versionID uuid.UUID,
) (CourseVersionContent, error) {
	queries := db.New(s.pool)
	if _, err := s.requireCourseVersionReadAccess(ctx, queries, actor, courseID); err != nil {
		return CourseVersionContent{}, err
	}
	row, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: versionID})
	if err != nil || row.CourseID != courseID {
		if isNoRows(err) || err == nil {
			return CourseVersionContent{}, notFound("Версия курса")
		}
		return CourseVersionContent{}, internal("Не удалось получить версию курса", err)
	}
	return s.loadCourseVersionContent(ctx, queries, row)
}

func (s *Service) CreateCourseDraft(ctx context.Context, actor Actor, courseID uuid.UUID) (CourseVersion, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersion{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	courseRow, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return CourseVersion{}, notFound("Курс")
		}
		return CourseVersion{}, internal("Не удалось получить курс", err)
	}
	course := courseFromRow(courseRow)
	if course.LifecycleStatus == "deleted" {
		return CourseVersion{}, conflict("Курс удалён")
	}
	if !canEditCourse(actor, course) {
		return CourseVersion{}, forbidden("Недостаточно прав для изменения этого курса")
	}
	if course.CurrentDraftVersionID != nil {
		return CourseVersion{}, conflict("У курса уже есть черновик версии")
	}
	if course.LatestPublishedVersionID == nil {
		return CourseVersion{}, conflict("У курса нет опубликованной версии для создания черновика")
	}

	now := s.now().UTC()
	row, err := queries.CreateNextDraftCourseVersionFromPublished(ctx, db.CreateNextDraftCourseVersionFromPublishedParams{
		ID: uuid.New(), CreatedByID: actor.UserID, CreatedAt: now,
		CompanyID: actor.CompanyID, SourceVersionID: *course.LatestPublishedVersionID,
	})
	if err != nil {
		return CourseVersion{}, internal("Не удалось создать черновик версии", err)
	}
	cloneParams := db.CloneCourseVersionSectionsParams{
		TargetVersionID: row.ID, CompanyID: actor.CompanyID, SourceVersionID: *course.LatestPublishedVersionID,
	}
	if _, err = queries.CloneCourseVersionSections(ctx, cloneParams); err != nil {
		return CourseVersion{}, internal("Не удалось скопировать разделы версии", err)
	}
	if _, err = queries.CloneCourseVersionLessons(ctx, db.CloneCourseVersionLessonsParams(cloneParams)); err != nil {
		return CourseVersion{}, internal("Не удалось скопировать уроки версии", err)
	}
	if _, err = queries.CloneCourseVersionQuizzes(ctx, db.CloneCourseVersionQuizzesParams(cloneParams)); err != nil {
		return CourseVersion{}, internal("Не удалось скопировать тесты версии", err)
	}
	if affected, setErr := queries.SetCourseCurrentDraftVersion(ctx, db.SetCourseCurrentDraftVersionParams{
		UpdatedAt: now, CompanyID: actor.CompanyID, CourseID: courseID, VersionID: row.ID,
	}); setErr != nil || affected != 1 {
		return CourseVersion{}, internal("Не удалось связать черновик с курсом", setErr)
	}
	created := courseVersionFromRow(row)
	if err = s.auditCourseVersion(ctx, queries, actor, "course_version_draft_created", nil, created); err != nil {
		return CourseVersion{}, err
	}
	if err = s.emitCourseVersionDraftCreated(ctx, queries, actor, course, created, course.LatestPublishedVersionID); err != nil {
		return CourseVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseVersion{}, internal("Не удалось сохранить черновик версии", err)
	}
	return created, nil
}

type UpdateCourseDraftInput struct {
	CourseID                    uuid.UUID
	Title                       *string
	Description                 *string
	CoverFileID                 *uuid.UUID
	Sequential                  *bool
	DefaultInternalDeadlineDays *int32
	SetDefaultDeadline          bool
}

func (s *Service) UpdateCourseDraft(ctx context.Context, actor Actor, input UpdateCourseDraftInput) (CourseVersion, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersion{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.LockCourseAndCurrentDraftVersion(ctx, db.LockCourseAndCurrentDraftVersionParams{
		CompanyID: actor.CompanyID, CourseID: input.CourseID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseVersion{}, notFound("Черновик версии курса")
		}
		return CourseVersion{}, internal("Не удалось получить черновик версии", err)
	}
	current := courseVersionFromRow(row)
	if err = s.requireDraftVersionEdit(ctx, queries, actor, current); err != nil {
		return CourseVersion{}, err
	}
	if input.Title != nil {
		current.Title, err = requiredText(*input.Title, "Укажите название курса")
		if err != nil {
			return CourseVersion{}, err
		}
	}
	if input.Description != nil {
		current.Description = input.Description
	}
	if input.CoverFileID != nil {
		current.CoverFileID = input.CoverFileID
	}
	if input.Sequential != nil {
		current.Sequential = *input.Sequential
	}
	if input.SetDefaultDeadline || input.DefaultInternalDeadlineDays != nil {
		if input.DefaultInternalDeadlineDays != nil && *input.DefaultInternalDeadlineDays < 1 {
			return CourseVersion{}, validation("Срок выполнения должен быть не меньше одного дня")
		}
		current.DefaultInternalDeadlineDays = input.DefaultInternalDeadlineDays
	}
	updatedRow, err := queries.UpdateDraftCourseVersion(ctx, db.UpdateDraftCourseVersionParams{
		Title: current.Title, Description: nullText(current.Description), CoverFileID: nullUUID(current.CoverFileID),
		CoverUrl: nullText(current.CoverURL), Sequential: current.Sequential,
		DefaultInternalDeadlineDays: nullInt4(current.DefaultInternalDeadlineDays),
		CompanyID:                   actor.CompanyID, ID: current.ID,
	})
	if err != nil {
		return CourseVersion{}, internal("Не удалось обновить черновик версии", err)
	}
	updated := courseVersionFromRow(updatedRow)
	if err = tx.Commit(ctx); err != nil {
		return CourseVersion{}, internal("Не удалось сохранить черновик версии", err)
	}
	return updated, nil
}

func (s *Service) PublishCourseVersion(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
	idempotencyKey string,
) (CourseVersion, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len([]byte(idempotencyKey)) > 512 {
		return CourseVersion{}, validation("Некорректный ключ идемпотентности")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersion{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	courseRow, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return CourseVersion{}, notFound("Курс")
		}
		return CourseVersion{}, internal("Не удалось получить курс", err)
	}
	course := courseFromRow(courseRow)
	if !canPublishCourse(actor, course) {
		return CourseVersion{}, forbidden("Недостаточно прав для публикации этого курса")
	}
	previous, previousErr := queries.GetCourseVersionPublishIdempotency(ctx, db.GetCourseVersionPublishIdempotencyParams{
		CompanyID: actor.CompanyID, CourseID: courseID, IdempotencyKey: idempotencyKey,
	})
	if previousErr == nil {
		publishedRow, getErr := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: previous.VersionID})
		if getErr != nil {
			return CourseVersion{}, internal("Не удалось получить результат публикации", getErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return CourseVersion{}, internal("Не удалось завершить повторную публикацию", commitErr)
		}
		return courseVersionFromRow(publishedRow), nil
	}
	if !isNoRows(previousErr) {
		return CourseVersion{}, internal("Не удалось проверить повторную публикацию", previousErr)
	}
	draftRow, err := queries.LockCourseAndCurrentDraftVersion(ctx, db.LockCourseAndCurrentDraftVersionParams{
		CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseVersion{}, notFound("Черновик версии курса")
		}
		return CourseVersion{}, internal("Не удалось получить черновик версии", err)
	}
	draft := courseVersionFromRow(draftRow)
	if err = s.requireDraftVersionEdit(ctx, queries, actor, draft); err != nil {
		return CourseVersion{}, err
	}
	cloneJobs, err := queries.LockCourseVersionFileCloneJobs(ctx, db.LockCourseVersionFileCloneJobsParams{
		CompanyID: actor.CompanyID, CourseVersionID: draft.ID,
	})
	if err != nil {
		return CourseVersion{}, internal("Не удалось проверить копирование файлов версии", err)
	}
	if err = requireCompletedFileCloneJobs(cloneJobs); err != nil {
		return CourseVersion{}, err
	}
	content, err := s.loadCourseVersionContent(ctx, queries, draftRow)
	if err != nil {
		return CourseVersion{}, err
	}
	domainValue, err := domainVersionFromContent(content)
	if err != nil {
		return CourseVersion{}, validation(err.Error())
	}
	now := s.now().UTC()
	if err = domainValue.Publish(domainversion.PublishParams{
		ActorID: domainversion.ID(actor.UserID.String()), At: now,
	}, authorizationCourse(course), domainversion.PublicationValidators{
		RichText: richtext.Validate,
		// File ownership validation is performed by the files integration once a
		// file id is present. This pure predicate keeps publication fail-closed at
		// the domain boundary while the academy has no cross-service file client.
		FileAvailable: func(domainversion.ID) bool { return true },
	}); err != nil {
		return CourseVersion{}, validation(err.Error())
	}
	snapshot := domainValue.Snapshot()
	publishedRow, err := queries.PublishCourseVersion(ctx, db.PublishCourseVersionParams{
		PublishedByID: nullUUID(&actor.UserID), PublishedAt: nullTimestamptz(&now),
		ContentHash: nullText(&snapshot.ContentHash), CompanyID: actor.CompanyID,
		CourseID: courseID, ID: draft.ID,
	})
	if err != nil {
		return CourseVersion{}, internal("Не удалось опубликовать версию", err)
	}
	if affected, setErr := queries.SetCoursePublishedVersionPointers(ctx, db.SetCoursePublishedVersionPointersParams{
		UpdatedAt: now, CompanyID: actor.CompanyID, CourseID: courseID, VersionID: draft.ID,
	}); setErr != nil || affected != 1 {
		return CourseVersion{}, internal("Не удалось обновить опубликованную версию курса", setErr)
	}
	if _, err = queries.CreateCourseVersionPublishIdempotency(ctx, db.CreateCourseVersionPublishIdempotencyParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: courseID,
		IdempotencyKey: idempotencyKey, VersionID: draft.ID, CreatedAt: now,
	}); err != nil {
		return CourseVersion{}, internal("Не удалось сохранить ключ публикации", err)
	}
	published := courseVersionFromRow(publishedRow)
	if err = s.auditCourseVersion(ctx, queries, actor, "course_version_published", &draft, published); err != nil {
		return CourseVersion{}, err
	}
	if err = s.emitCourseVersionPublished(ctx, queries, actor, course, published); err != nil {
		return CourseVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseVersion{}, internal("Не удалось сохранить публикацию версии", err)
	}
	return published, nil
}

func requireCompletedFileCloneJobs(jobs []db.LockCourseVersionFileCloneJobsRow) error {
	for _, job := range jobs {
		if job.Status != "completed" {
			return conflict("Версию нельзя опубликовать, пока копирование файлов не завершено")
		}
	}
	return nil
}

func (s *Service) GetPublishedCourseVersion(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
	versionID *uuid.UUID,
) (CourseVersionContent, error) {
	queries := db.New(s.pool)
	if err := s.requireCourseAccess(ctx, queries, actor, courseID); err != nil {
		return CourseVersionContent{}, err
	}
	var row db.CourseVersion
	var err error
	if versionID == nil {
		row, err = queries.GetLatestPublishedCourseVersion(ctx, db.GetLatestPublishedCourseVersionParams{
			CompanyID: actor.CompanyID, CourseID: courseID,
		})
	} else {
		row, err = queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: *versionID})
	}
	if err != nil || row.CourseID != courseID || (row.Status != "published" && row.Status != "retired") {
		if isNoRows(err) || err == nil {
			return CourseVersionContent{}, notFound("Опубликованная версия курса")
		}
		return CourseVersionContent{}, internal("Не удалось получить опубликованную версию", err)
	}
	return s.loadCourseVersionContent(ctx, queries, row)
}

func (s *Service) loadCourseVersionContent(
	ctx context.Context,
	queries *db.Queries,
	row db.CourseVersion,
) (CourseVersionContent, error) {
	sections, err := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{
		CompanyID: row.CompanyID, CourseVersionID: row.ID,
	})
	if err != nil {
		return CourseVersionContent{}, internal("Не удалось получить разделы версии", err)
	}
	lessons, err := queries.GetCourseVersionLessons(ctx, db.GetCourseVersionLessonsParams{
		CompanyID: row.CompanyID, CourseVersionID: row.ID,
	})
	if err != nil {
		return CourseVersionContent{}, internal("Не удалось получить уроки версии", err)
	}
	quizzes, err := queries.GetCourseVersionQuizzes(ctx, db.GetCourseVersionQuizzesParams{
		CompanyID: row.CompanyID, CourseVersionID: row.ID,
	})
	if err != nil {
		return CourseVersionContent{}, internal("Не удалось получить тесты версии", err)
	}
	return CourseVersionContent{
		Version: courseVersionFromRow(row), Sections: courseVersionSectionsFromRows(sections),
		Lessons: courseVersionLessonsFromRows(lessons), Quizzes: courseVersionQuizzesFromRows(quizzes),
	}, nil
}

func (s *Service) requireCourseVersionReadAccess(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	courseID uuid.UUID,
) (Course, error) {
	row, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return Course{}, notFound("Курс")
		}
		return Course{}, internal("Не удалось проверить курс", err)
	}
	course := courseFromRow(row)
	if course.LifecycleStatus == "deleted" {
		return Course{}, notFound("Курс")
	}
	if actor.canManage() {
		return course, nil
	}
	if actor.Role == "partner" && course.OwnerType == "partner" && course.OwnerUserID != nil && *course.OwnerUserID == actor.UserID {
		return course, nil
	}
	return Course{}, forbidden("Недостаточно прав для просмотра версий этого курса")
}

func (s *Service) requireDraftVersionEdit(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	version CourseVersion,
) error {
	if version.Status != "draft" {
		return conflict("Опубликованную версию нельзя редактировать")
	}
	row, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: version.CourseID})
	if err != nil {
		if isNoRows(err) {
			return notFound("Курс")
		}
		return internal("Не удалось проверить курс", err)
	}
	course := courseFromRow(row)
	if course.LifecycleStatus == "deleted" {
		return conflict("Курс удалён")
	}
	if !canEditCourse(actor, course) {
		return forbidden("Недостаточно прав для изменения этого курса")
	}
	if version.CreatedByID != actor.UserID {
		return forbidden("Черновик версии принадлежит другому пользователю")
	}
	return nil
}

func (s *Service) emitCourseVersionDraftCreated(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	course Course,
	version CourseVersion,
	basedOn *uuid.UUID,
) error {
	payload := &eventsv1.AcademyCourseVersionDraftCreatedPayload{
		CourseId: course.ID.String(), CourseVersionId: version.ID.String(),
		VersionNumber: uint32(max(0, version.Number)), OwnerType: courseOwnerTypeToEvent(course.OwnerType),
		OwnerUserId: optionalUUIDStringValue(course.OwnerUserID), CreatedById: actor.UserID.String(),
		BasedOnVersionId: optionalUUIDStringValue(basedOn),
	}
	return s.emit(ctx, queries, actor.CompanyID, version.ID, actor.UserID,
		"teamos.academy.course_version.draft_created.v1", payload)
}

func (s *Service) emitCourseVersionPublished(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	course Course,
	version CourseVersion,
) error {
	recipients := []string{}
	if course.OwnerType == "partner" && s.company != nil {
		managerIDs, err := s.company.GetManagerUserIDs(ctx, actor.Token)
		if err != nil {
			s.logger.Warn("не удалось определить получателей уведомления о публикации партнёра", "error", err)
		} else {
			for _, id := range managerIDs {
				recipients = append(recipients, id.String())
			}
		}
	}
	payload := &eventsv1.AcademyCourseVersionPublishedPayload{
		CourseId: course.ID.String(), CourseVersionId: version.ID.String(),
		VersionNumber: uint32(max(0, version.Number)), OwnerType: courseOwnerTypeToEvent(course.OwnerType),
		OwnerUserId: optionalUUIDStringValue(course.OwnerUserID), PublishedById: actor.UserID.String(),
		ContentHash: "", CourseTitle: optionalStringValue(version.Title),
		Link: optionalStringValue(academyLink + "/courses/" + course.ID.String()), RecipientUserIds: recipients,
	}
	if version.PublishedAt != nil {
		payload.PublishedAt = timestamppb.New(version.PublishedAt.UTC())
	}
	if version.ContentHash != nil {
		payload.ContentHash = *version.ContentHash
	}
	return s.emit(ctx, queries, actor.CompanyID, version.ID, actor.UserID,
		"teamos.academy.course_version.published.v1", payload)
}

func (s *Service) createInitialDraftFromLegacy(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	course Course,
) (CourseVersion, error) {
	now := s.now().UTC()
	versionRow, err := queries.CreateCourseVersion(ctx, db.CreateCourseVersionParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: course.ID, Number: 1,
		Title: course.Title, Description: nullText(course.Description), CoverUrl: nullText(course.CoverURL),
		Sequential: course.Sequential, DefaultInternalDeadlineDays: nullInt4(course.DeadlineDays),
		CreatedByID: actor.UserID, CreatedAt: now,
	})
	if err != nil {
		return CourseVersion{}, internal("Не удалось создать начальную версию курса", err)
	}
	sections, err := queries.GetCourseSections(ctx, db.GetCourseSectionsParams{
		CompanyID: actor.CompanyID, CourseID: course.ID,
	})
	if err != nil {
		return CourseVersion{}, internal("Не удалось получить разделы начальной версии", err)
	}
	for _, section := range sections {
		if _, err = queries.CreateCourseVersionSection(ctx, db.CreateCourseVersionSectionParams{
			ID: section.ID, StableKey: section.ID, Title: section.Title, OrderValue: section.Order,
			CompanyID: actor.CompanyID, CourseVersionID: versionRow.ID,
		}); err != nil {
			return CourseVersion{}, internal("Не удалось создать раздел начальной версии", err)
		}
	}
	lessons, err := queries.GetCourseLessons(ctx, db.GetCourseLessonsParams{
		CompanyID: actor.CompanyID, CourseID: course.ID,
	})
	if err != nil {
		return CourseVersion{}, internal("Не удалось получить уроки начальной версии", err)
	}
	for _, lesson := range lessons {
		sourceType := "manual"
		if lesson.SourceArticleID.Valid {
			if lesson.SourceMode.Valid && lesson.SourceMode.String == "link" {
				sourceType = "kb_link"
			} else {
				sourceType = "kb_snapshot"
			}
		}
		versionLesson, createErr := queries.CreateCourseVersionLesson(ctx, db.CreateCourseVersionLessonParams{
			ID: lesson.ID, SectionVersionID: lesson.SectionID, StableKey: lesson.ID,
			Title: lesson.Title, OrderValue: lesson.Order, Content: lesson.Content,
			SourceType: sourceType, SourceArticleID: lesson.SourceArticleID,
			CompanyID: actor.CompanyID, CourseVersionID: versionRow.ID,
		})
		if createErr != nil {
			return CourseVersion{}, internal("Не удалось создать урок начальной версии", createErr)
		}
		quizzes, quizErr := queries.GetLessonQuizzes(ctx, db.GetLessonQuizzesParams{
			CompanyID: actor.CompanyID, LessonID: lesson.ID,
		})
		if quizErr != nil {
			return CourseVersion{}, internal("Не удалось получить тест начальной версии", quizErr)
		}
		for _, quiz := range quizzes {
			if _, quizErr = queries.CreateCourseVersionQuiz(ctx, db.CreateCourseVersionQuizParams{
				ID: quiz.ID, LessonVersionID: versionLesson.ID, Questions: quiz.Questions,
				PassingScore: quiz.PassingScore, MaxAttempts: quiz.MaxAttempts,
				CompanyID: actor.CompanyID, CourseVersionID: versionRow.ID,
			}); quizErr != nil {
				return CourseVersion{}, internal("Не удалось создать тест начальной версии", quizErr)
			}
		}
	}
	if affected, setErr := queries.SetCourseCurrentDraftVersion(ctx, db.SetCourseCurrentDraftVersionParams{
		UpdatedAt: now, CompanyID: actor.CompanyID, CourseID: course.ID, VersionID: versionRow.ID,
	}); setErr != nil || affected != 1 {
		return CourseVersion{}, internal("Не удалось связать начальную версию с курсом", setErr)
	}
	created := courseVersionFromRow(versionRow)
	if err = s.auditCourseVersion(ctx, queries, actor, "course_version_draft_created", nil, created); err != nil {
		return CourseVersion{}, err
	}
	if err = s.emitCourseVersionDraftCreated(ctx, queries, actor, course, created, nil); err != nil {
		return CourseVersion{}, err
	}
	return created, nil
}

type storedQuizQuestion struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Text    string `json:"text"`
	Options []struct {
		ID      string `json:"id"`
		Text    string `json:"text"`
		Correct bool   `json:"correct"`
	} `json:"options"`
}

func domainVersionFromContent(content CourseVersionContent) (*domainversion.Version, error) {
	sections := make([]domainversion.Section, len(content.Sections))
	for index, section := range content.Sections {
		sections[index] = domainversion.Section{
			ID: domainversion.ID(section.ID.String()), StableKey: section.StableKey,
			Title: section.Title, Order: int(section.Order),
		}
	}
	quizzes := make(map[uuid.UUID]CourseVersionQuiz, len(content.Quizzes))
	for _, quiz := range content.Quizzes {
		quizzes[quiz.ID] = quiz
	}
	lessons := make([]domainversion.Lesson, len(content.Lessons))
	for index, lesson := range content.Lessons {
		converted := domainversion.Lesson{
			ID: domainversion.ID(lesson.ID.String()), SectionID: domainversion.ID(lesson.SectionVersionID.String()),
			StableKey: lesson.StableKey, Title: lesson.Title, Order: int(lesson.Order),
			Content: append(json.RawMessage(nil), lesson.Content...), SourceType: lesson.SourceType,
			SourceArticleID:         optionalDomainID(lesson.SourceArticleID),
			SourceArticleVersion:    optionalInt(lesson.SourceArticleVersion),
			SourceTemplateID:        optionalDomainID(lesson.SourceTemplateID),
			SourceTemplateVersionID: optionalDomainID(lesson.SourceTemplateVersionID),
			EstimatedMinutes:        optionalInt(lesson.EstimatedMinutes),
		}
		for _, fileID := range lesson.FileIDs {
			converted.FileIDs = append(converted.FileIDs, domainversion.ID(fileID.String()))
		}
		if lesson.QuizVersionID != nil {
			quiz, ok := quizzes[*lesson.QuizVersionID]
			if ok {
				convertedQuiz, err := domainQuiz(quiz)
				if err != nil {
					return nil, err
				}
				converted.Quiz = &convertedQuiz
			}
		}
		lessons[index] = converted
	}
	definition := domainversion.Definition{
		Title: content.Version.Title, Description: content.Version.Description,
		CoverFileID: optionalDomainID(content.Version.CoverFileID), CoverURL: content.Version.CoverURL,
		Sequential:                  content.Version.Sequential,
		DefaultInternalDeadlineDays: optionalInt(content.Version.DefaultInternalDeadlineDays),
		Sections:                    sections, Lessons: lessons,
	}
	return domainversion.Rehydrate(domainversion.Snapshot{
		ID: domainversion.ID(content.Version.ID.String()), CompanyID: domainversion.ID(content.Version.CompanyID.String()),
		CourseID: domainversion.ID(content.Version.CourseID.String()), Number: int(content.Version.Number),
		Status: domainversion.Status(content.Version.Status), Definition: definition,
		CreatedByID: domainversion.ID(content.Version.CreatedByID.String()), CreatedAt: content.Version.CreatedAt,
		PublishedByID: optionalDomainID(content.Version.PublishedByID), PublishedAt: content.Version.PublishedAt,
		ContentHash: optionalString(content.Version.ContentHash),
	})
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func domainQuiz(value CourseVersionQuiz) (domainversion.Quiz, error) {
	var stored []storedQuizQuestion
	if err := json.Unmarshal(value.Questions, &stored); err != nil {
		return domainversion.Quiz{}, err
	}
	questions := make([]domainversion.Question, len(stored))
	for index, question := range stored {
		options := make([]domainversion.Option, len(question.Options))
		for optionIndex, option := range question.Options {
			options[optionIndex] = domainversion.Option{
				ID: domainversion.ID(option.ID), Text: option.Text, Correct: option.Correct,
			}
		}
		questions[index] = domainversion.Question{
			ID: domainversion.ID(question.ID), Type: domainversion.QuestionType(question.Type),
			Text: question.Text, Options: options,
		}
	}
	return domainversion.Quiz{
		ID: domainversion.ID(value.ID.String()), Questions: questions,
		PassingScore: int(value.PassingScore), MaxAttempts: optionalInt(value.MaxAttempts),
	}, nil
}

func optionalDomainID(value *uuid.UUID) *domainversion.ID {
	if value == nil {
		return nil
	}
	converted := domainversion.ID(value.String())
	return &converted
}

func optionalInt(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func (s *Service) displayCourseVersion(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	course Course,
) (*db.CourseVersion, error) {
	var versionID *uuid.UUID
	if canEditCourse(actor, course) && course.CurrentDraftVersionID != nil {
		versionID = course.CurrentDraftVersionID
	} else if course.LatestPublishedVersionID != nil {
		versionID = course.LatestPublishedVersionID
	}
	if versionID == nil {
		return nil, nil
	}
	row, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: *versionID})
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, internal("Не удалось получить отображаемую версию курса", err)
	}
	return &row, nil
}

func overlayCourseWithVersion(course Course, version db.CourseVersion) Course {
	course.Title = version.Title
	course.Description = textPointer(version.Description)
	course.CoverURL = textPointer(version.CoverUrl)
	course.Sequential = version.Sequential
	course.DeadlineDays = int4Pointer(version.DefaultInternalDeadlineDays)
	if version.Status == "draft" {
		course.Status = "draft"
	} else {
		course.Status = "published"
	}
	return course
}

func versionSectionAsLegacy(value db.CourseVersionSection, courseID uuid.UUID) CourseSection {
	return CourseSection{
		ID: value.ID, CompanyID: value.CompanyID, CourseID: courseID,
		Title: value.Title, Order: value.Order,
	}
}

func versionLessonAsLegacy(value db.CourseVersionLesson, courseID uuid.UUID) Lesson {
	result := Lesson{
		ID: value.ID, CompanyID: value.CompanyID, CourseID: courseID,
		SectionID: value.SectionVersionID, Title: value.Title, Order: value.Order,
		Content:         append(json.RawMessage(nil), value.Content...),
		SourceArticleID: nullUUIDPointer(value.SourceArticleID), QuizID: nullUUIDPointer(value.QuizVersionID),
	}
	var mode string
	switch value.SourceType {
	case "kb_link":
		mode = "link"
	case "kb_snapshot":
		mode = "copy"
	}
	if mode != "" {
		result.SourceMode = &mode
	}
	return result
}

func courseVersionLessonAsLegacy(value CourseVersionLesson, courseID uuid.UUID) Lesson {
	result := Lesson{
		ID: value.ID, CompanyID: value.CompanyID, CourseID: courseID,
		SectionID: value.SectionVersionID, Title: value.Title, Order: value.Order,
		Content: append(json.RawMessage(nil), value.Content...), SourceArticleID: value.SourceArticleID,
		QuizID: value.QuizVersionID,
	}
	var mode string
	switch value.SourceType {
	case "kb_link":
		mode = "link"
	case "kb_snapshot":
		mode = "copy"
	}
	if mode != "" {
		result.SourceMode = &mode
	}
	return result
}

func versionQuizAsLegacy(value db.CourseVersionQuiz) Quiz {
	return Quiz{
		ID: value.ID, CompanyID: value.CompanyID, LessonID: value.LessonVersionID,
		Questions: append(json.RawMessage(nil), value.Questions...), PassingScore: value.PassingScore,
		MaxAttempts: int4Pointer(value.MaxAttempts),
	}
}
