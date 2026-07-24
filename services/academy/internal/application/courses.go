package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type CourseFilters struct {
	OwnerType          *string
	PartnerID          *uuid.UUID
	LifecycleStatus    *string
	DistributionStatus *string
	HasDraft           *bool
	LatestVersion      *uint32
	OriginType         *string
}

func (s *Service) GetCourses(ctx context.Context, actor Actor, optionalFilters ...CourseFilters) ([]Course, error) {
	queries := db.New(s.pool)
	if !canReadAcademy(actor) {
		return nil, forbidden("Недостаточно прав для просмотра академии")
	}
	filters := CourseFilters{}
	if len(optionalFilters) > 0 {
		filters = optionalFilters[0]
	}
	if err := validateCourseFilters(filters); err != nil {
		return nil, err
	}
	filtered := filters.OwnerType != nil || filters.PartnerID != nil || filters.LifecycleStatus != nil || filters.DistributionStatus != nil
	var rows []db.Course
	var err error
	if filtered {
		rows, err = queries.GetCoursesFiltered(ctx, db.GetCoursesFilteredParams{
			CompanyID: actor.CompanyID, OwnerType: nullText(filters.OwnerType),
			OwnerUserID: nullUUID(filters.PartnerID), LifecycleStatus: nullText(filters.LifecycleStatus),
			DistributionStatus: nullText(filters.DistributionStatus),
		})
	} else {
		rows, err = queries.GetCourses(ctx, actor.CompanyID)
	}
	if err != nil {
		return nil, internal("Не удалось получить курсы", err)
	}
	courses := coursesFromRows(rows)
	if filters.LifecycleStatus == nil {
		visible := courses[:0]
		for _, value := range courses {
			if value.LifecycleStatus != "deleted" {
				visible = append(visible, value)
			}
		}
		courses = visible
	}
	if actor.canManage() {
		return s.overlayCourseListWithVersions(ctx, queries, actor, courses)
	}
	assignedIDs, err := s.assignedCourseIDs(ctx, queries, actor)
	if err != nil {
		return nil, err
	}
	assigned := make(map[uuid.UUID]struct{}, len(assignedIDs))
	for _, id := range assignedIDs {
		assigned[id] = struct{}{}
	}
	partnerAudience, err := s.partnerAudienceCourseIDs(ctx, queries, actor)
	if err != nil {
		return nil, err
	}
	result := make([]Course, 0, len(courses))
	for _, course := range courses {
		if visibleCourse(actor, course, assigned, partnerAudience) {
			result = append(result, course)
		}
	}
	return s.overlayCourseListWithVersions(ctx, queries, actor, result)
}

func (s *Service) overlayCourseListWithVersions(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	courses []Course,
) ([]Course, error) {
	for index := range courses {
		version, err := s.displayCourseVersion(ctx, queries, actor, courses[index])
		if err != nil {
			return nil, err
		}
		if version != nil {
			courses[index] = overlayCourseWithVersion(courses[index], *version)
		}
	}
	return courses, nil
}

func validateCourseFilters(filters CourseFilters) error {
	if filters.OwnerType != nil && *filters.OwnerType != "company" && *filters.OwnerType != "partner" {
		return validation("Некорректный тип владельца курса")
	}
	if filters.LifecycleStatus != nil {
		switch *filters.LifecycleStatus {
		case "active", "archived", "deleted":
		default:
			return validation("Некорректное состояние курса")
		}
	}
	if filters.DistributionStatus != nil {
		switch *filters.DistributionStatus {
		case "active", "paused", "blocked":
		default:
			return validation("Некорректное состояние распространения курса")
		}
	}
	if filters.PartnerID != nil && filters.OwnerType != nil && *filters.OwnerType != "partner" {
		return validation("Фильтр партнёра применим только к партнёрским курсам")
	}
	return nil
}

func (s *Service) GetCourse(ctx context.Context, actor Actor, id uuid.UUID) (Course, error) {
	queries := db.New(s.pool)
	row, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return Course{}, notFound("Курс")
		}
		return Course{}, internal("Не удалось получить курс", err)
	}
	if err = s.requireCourseAccess(ctx, queries, actor, id); err != nil {
		return Course{}, err
	}
	result := courseFromRow(row)
	version, err := s.displayCourseVersion(ctx, queries, actor, result)
	if err != nil {
		return Course{}, err
	}
	if version != nil {
		result = overlayCourseWithVersion(result, *version)
	}
	return result, nil
}

func (s *Service) GetPublicCourse(ctx context.Context, id uuid.UUID) (PublicCourse, error) {
	queries := db.New(s.pool)
	row, err := queries.GetPublicCourse(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return PublicCourse{}, notFound("Курс")
		}
		return PublicCourse{}, internal("Не удалось получить публичный курс", err)
	}
	course := courseFromRow(row)
	if course.LatestPublishedVersionID != nil {
		version, versionErr := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: course.CompanyID, ID: *course.LatestPublishedVersionID})
		if versionErr != nil {
			return PublicCourse{}, internal("Не удалось получить опубликованную версию курса", versionErr)
		}
		sectionRows, sectionErr := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{CompanyID: course.CompanyID, CourseVersionID: version.ID})
		if sectionErr != nil {
			return PublicCourse{}, internal("Не удалось получить разделы публичного курса", sectionErr)
		}
		lessonRows, lessonErr := queries.GetCourseVersionLessons(ctx, db.GetCourseVersionLessonsParams{CompanyID: course.CompanyID, CourseVersionID: version.ID})
		if lessonErr != nil {
			return PublicCourse{}, internal("Не удалось получить уроки публичного курса", lessonErr)
		}
		sections := make([]CourseSection, len(sectionRows))
		for index := range sectionRows {
			sections[index] = versionSectionAsLegacy(sectionRows[index], id)
		}
		lessons := make([]Lesson, len(lessonRows))
		for index := range lessonRows {
			lessons[index] = versionLessonAsLegacy(lessonRows[index], id)
		}
		if err = s.resolvePublicLinkedLessons(ctx, lessons); err != nil {
			return PublicCourse{}, err
		}
		return PublicCourse{Course: overlayCourseWithVersion(course, version), Sections: sections, Lessons: lessons}, nil
	}
	sections, err := queries.GetPublicCourseSections(ctx, id)
	if err != nil {
		return PublicCourse{}, internal("Не удалось получить разделы публичного курса", err)
	}
	lessons, err := queries.GetPublicCourseLessons(ctx, id)
	if err != nil {
		return PublicCourse{}, internal("Не удалось получить уроки публичного курса", err)
	}
	convertedLessons := lessonsFromRows(lessons)
	if err = s.resolvePublicLinkedLessons(ctx, convertedLessons); err != nil {
		return PublicCourse{}, err
	}
	convertedSections := make([]CourseSection, len(sections))
	for index := range sections {
		convertedSections[index] = sectionFromRow(sections[index])
	}
	return PublicCourse{Course: courseFromRow(row), Sections: convertedSections, Lessons: convertedLessons}, nil
}

func (s *Service) resolvePublicLinkedLessons(ctx context.Context, lessons []Lesson) error {
	for index := range lessons {
		lesson := &lessons[index]
		if lesson.SourceMode == nil || *lesson.SourceMode != "link" || lesson.SourceArticleID == nil {
			continue
		}
		if s.kb == nil {
			return notFound("Курс")
		}
		article, articleErr := s.kb.GetPublicArticle(ctx, *lesson.SourceArticleID)
		if articleErr != nil {
			// Fail closed if a linked article was unpublished or its section was closed.
			return notFound("Курс")
		}
		lesson.Content = append(json.RawMessage(nil), article.Content...)
	}
	return nil
}

type CreateCourseInput struct {
	Title        string
	Description  *string
	Status       *string
	Sequential   *bool
	DeadlineDays *int32
	Visibility   *string
}

func (s *Service) CreateCourse(ctx context.Context, actor Actor, input CreateCourseInput) (Course, error) {
	if !canCreateCourse(actor) {
		return Course{}, forbidden("Недостаточно прав для создания курса")
	}
	title, err := requiredText(input.Title, "Укажите название курса")
	if err != nil {
		return Course{}, err
	}
	status := "draft"
	if input.Status != nil {
		if err = validateCourseStatus(*input.Status); err != nil {
			return Course{}, err
		}
		status = *input.Status
	}
	if status == "published" {
		return Course{}, validation("Новый курс сначала создаётся как черновик; добавьте уроки и опубликуйте версию")
	}
	sequential := true
	if input.Sequential != nil {
		sequential = *input.Sequential
	}
	deadlineDays, err := normalizeDeadlineDays(input.DeadlineDays)
	if err != nil {
		return Course{}, err
	}
	visibility := "restricted"
	if input.Visibility != nil {
		if err = validateCourseVisibility(*input.Visibility); err != nil {
			return Course{}, err
		}
		visibility = *input.Visibility
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Course{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	now := s.now().UTC()
	ownerType := "company"
	var ownerUserID *uuid.UUID
	if actor.Role == "partner" {
		ownerType = "partner"
		ownerUserID = &actor.UserID
	}
	row, err := queries.CreateOwnedCourse(ctx, db.CreateOwnedCourseParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Title: title,
		Description: nullText(input.Description), Status: status,
		AuthorID: actor.UserID, Sequential: sequential,
		DeadlineDays: nullInt4(deadlineDays), CreatedAt: now,
		Visibility: visibility, OwnerType: ownerType,
		OwnerUserID: nullUUID(ownerUserID), CreatedByID: nullUUID(&actor.UserID),
	})
	if err != nil {
		return Course{}, internal("Не удалось создать курс", err)
	}
	// The mock seeds every new course with an initial section.
	if _, err = queries.CreateCourseSection(ctx, db.CreateCourseSectionParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: row.ID,
		Title: "Первый раздел", Order: 0,
	}); err != nil {
		return Course{}, internal("Не удалось создать раздел курса", err)
	}
	created := courseFromRow(row)
	initialVersion, err := s.createInitialDraftFromLegacy(ctx, queries, actor, created)
	if err != nil {
		return Course{}, err
	}
	created.CurrentDraftVersionID = &initialVersion.ID
	if err = s.auditCourse(ctx, queries, actor, "course_created", nil, created); err != nil {
		return Course{}, err
	}
	if err = s.emit(ctx, queries, actor.CompanyID, created.ID, actor.UserID, "teamos.academy.course.created.v1",
		&eventsv1.AcademyCourseCreatedPayload{
			CourseId: created.ID.String(), OwnerType: courseOwnerTypeToEvent(created.OwnerType),
			OwnerUserId: optionalUUIDStringValue(created.OwnerUserID), CreatedById: actor.UserID.String(),
			LifecycleStatus:    courseLifecycleToEvent(created.LifecycleStatus),
			DistributionStatus: courseDistributionToEvent(created.DistributionStatus),
		}); err != nil {
		return Course{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Course{}, internal("Не удалось сохранить курс", err)
	}
	return created, nil
}

type CreateCourseFromKbInput struct {
	Title        string
	Description  *string
	Sequential   *bool
	DeadlineDays *int32
	Mode         string
	SectionIDs   []uuid.UUID
	ArticleIDs   []uuid.UUID
	Visibility   *string
}

func (s *Service) CreateCourseFromKb(ctx context.Context, actor Actor, input CreateCourseFromKbInput) (Course, error) {
	if !actor.canManage() && actor.Role != "partner" {
		return Course{}, forbidden("Недостаточно прав для изменения академии")
	}
	title, err := requiredText(input.Title, "Укажите название курса")
	if err != nil {
		return Course{}, err
	}
	if input.Mode != "link" && input.Mode != "copy" {
		return Course{}, validation("Некорректный режим импорта статей")
	}
	if actor.Role == "partner" && input.Mode != "copy" {
		return Course{}, forbidden("Партнёр может использовать статью только как независимый снимок")
	}
	deadlineDays, err := normalizeDeadlineDays(input.DeadlineDays)
	if err != nil {
		return Course{}, err
	}
	if s.kb == nil {
		return Course{}, unavailable("Сервис базы знаний временно недоступен", nil)
	}
	articles, err := s.kb.GetArticlesByIds(ctx, actor.Token, input.ArticleIDs)
	if err != nil {
		return Course{}, unavailable("Не удалось получить статьи базы знаний", err)
	}
	partnerSnapshots := make(map[uuid.UUID]KbArticleSnapshot)
	if actor.Role == "partner" {
		for index := range articles {
			partnerID := actor.UserID
			snapshot, snapshotErr := s.kb.GetArticleSnapshotForCourseCopy(
				ctx, actor.Token, articles[index].ID, nil, &partnerID,
			)
			if snapshotErr != nil {
				return Course{}, forbidden("Статья недоступна для копирования в курс партнёра")
			}
			partnerSnapshots[articles[index].ID] = snapshot
			articles[index].Title = snapshot.Title
			articles[index].Content = snapshot.Content
		}
	}
	kbSections, err := s.kb.GetSections(ctx, actor.Token)
	if err != nil {
		return Course{}, unavailable("Не удалось получить разделы базы знаний", err)
	}
	sectionNames := make(map[uuid.UUID]string, len(kbSections))
	sectionVisibility := make(map[uuid.UUID]string, len(kbSections))
	for _, section := range kbSections {
		sectionNames[section.ID] = section.Name
		sectionVisibility[section.ID] = section.Visibility
	}
	articlesBySection := make(map[uuid.UUID][]KbArticle, len(input.SectionIDs))
	for _, article := range articles {
		articlesBySection[article.SectionID] = append(articlesBySection[article.SectionID], article)
	}

	type sourceSection struct {
		name     string
		articles []KbArticle
	}
	source := make([]sourceSection, 0, len(input.SectionIDs))
	for _, sectionID := range input.SectionIDs {
		name, known := sectionNames[sectionID]
		selected := articlesBySection[sectionID]
		if !known || len(selected) == 0 {
			continue
		}
		source = append(source, sourceSection{name: name, articles: selected})
	}
	if len(source) == 0 {
		return Course{}, validation("Выберите хотя бы одну статью для курса.")
	}

	sequential := true
	if input.Sequential != nil {
		sequential = *input.Sequential
	}
	visibility := "restricted"
	if input.Visibility != nil {
		if err = validateCourseVisibility(*input.Visibility); err != nil {
			return Course{}, err
		}
		visibility = *input.Visibility
	}
	if actor.Role == "partner" {
		visibility = "restricted"
	}
	if visibility == "public" && input.Mode == "link" {
		for _, article := range articles {
			if sectionVisibility[article.SectionID] != "public" {
				return Course{}, validation("Публичный курс может ссылаться только на публичные статьи; используйте режим copy")
			}
		}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Course{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	now := s.now().UTC()
	ownerType := "company"
	var ownerUserID *uuid.UUID
	if actor.Role == "partner" {
		ownerType = "partner"
		ownerUserID = &actor.UserID
	}
	courseRow, err := queries.CreateOwnedCourse(ctx, db.CreateOwnedCourseParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Title: title,
		Description: nullText(input.Description), Status: "draft",
		AuthorID: actor.UserID, Sequential: sequential,
		DeadlineDays: nullInt4(deadlineDays), CreatedAt: now,
		Visibility: visibility, OwnerType: ownerType, OwnerUserID: nullUUID(ownerUserID),
		CreatedByID: nullUUID(&actor.UserID),
	})
	if err != nil {
		return Course{}, internal("Не удалось создать курс", err)
	}
	mode := input.Mode
	for sectionOrder, section := range source {
		sectionRow, sectionErr := queries.CreateCourseSection(ctx, db.CreateCourseSectionParams{
			ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: courseRow.ID,
			Title: section.name, Order: int32(sectionOrder),
		})
		if sectionErr != nil {
			return Course{}, internal("Не удалось создать раздел курса", sectionErr)
		}
		for lessonOrder, article := range section.articles {
			articleID := article.ID
			if _, lessonErr := queries.CreateLesson(ctx, db.CreateLessonParams{
				ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: courseRow.ID,
				SectionID: sectionRow.ID, Title: article.Title, Order: int32(lessonOrder),
				Content:            article.Content,
				SourceArticleID:    nullUUID(&articleID),
				SourceArticleTitle: nullText(&article.Title),
				SourceMode:         nullText(&mode),
			}); lessonErr != nil {
				return Course{}, internal("Не удалось создать урок", lessonErr)
			}
		}
	}
	created := courseFromRow(courseRow)
	initialVersion, err := s.createInitialDraftFromLegacy(ctx, queries, actor, created)
	if err != nil {
		return Course{}, err
	}
	if actor.Role == "partner" {
		if err = s.attachPartnerKBSnapshots(ctx, queries, actor, courseRow.ID, initialVersion.ID, partnerSnapshots, now); err != nil {
			return Course{}, err
		}
	}
	created.CurrentDraftVersionID = &initialVersion.ID
	if err = s.auditCourse(ctx, queries, actor, "course_created", nil, created); err != nil {
		return Course{}, err
	}
	if err = s.emit(ctx, queries, actor.CompanyID, created.ID, actor.UserID, "teamos.academy.course.created.v1",
		&eventsv1.AcademyCourseCreatedPayload{
			CourseId: created.ID.String(), OwnerType: courseOwnerTypeToEvent(created.OwnerType),
			CreatedById: actor.UserID.String(), LifecycleStatus: courseLifecycleToEvent(created.LifecycleStatus),
			DistributionStatus: courseDistributionToEvent(created.DistributionStatus),
		}); err != nil {
		return Course{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Course{}, internal("Не удалось сохранить курс", err)
	}
	return created, nil
}

func (s *Service) attachPartnerKBSnapshots(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	courseID, versionID uuid.UUID,
	snapshots map[uuid.UUID]KbArticleSnapshot,
	now time.Time,
) error {
	if len(snapshots) == 0 {
		return nil
	}
	lessons, err := queries.GetCourseVersionLessons(ctx, db.GetCourseVersionLessonsParams{
		CompanyID: actor.CompanyID, CourseVersionID: versionID,
	})
	if err != nil {
		return internal("Не удалось получить уроки для снимков базы знаний", err)
	}
	for _, lesson := range lessons {
		if !lesson.SourceArticleID.Valid {
			continue
		}
		snapshot, exists := snapshots[lesson.SourceArticleID.UUID]
		if !exists {
			return internal("Снимок статьи для урока не найден", nil)
		}
		requestKey := fmt.Sprintf("partner-course:%s:%s", versionID, snapshot.ArticleVersionID)
		snapshotRow, snapshotErr := queries.GetKBArticleSnapshotByRequestKey(ctx, db.GetKBArticleSnapshotByRequestKeyParams{
			CompanyID: actor.CompanyID, RequestKey: requestKey,
		})
		if isNoRows(snapshotErr) {
			snapshotRow, snapshotErr = queries.CreateKBArticleSnapshot(ctx, db.CreateKBArticleSnapshotParams{
				ID: uuid.New(), CompanyID: actor.CompanyID, SourceArticleID: snapshot.ArticleID,
				SourceArticleVersionID:     nullUUID(&snapshot.ArticleVersionID),
				SourceArticleVersionNumber: pgtype.Int4{Int32: snapshot.Version, Valid: true},
				RequestedByID:              actor.UserID, RequestedByPartnerID: nullUUID(&actor.UserID),
				RequestKey: requestKey, Title: snapshot.Title, Content: snapshot.Content,
				SourceFileIds: snapshot.FileIDs, ContentHash: snapshot.ContentHash, CreatedAt: now,
			})
		}
		if snapshotErr != nil {
			return internal("Не удалось сохранить снимок статьи базы знаний", snapshotErr)
		}
		if _, updateErr := queries.UpdateCourseVersionLesson(ctx, db.UpdateCourseVersionLessonParams{
			Title: lesson.Title, Content: snapshot.Content, SourceType: "kb_snapshot",
			SourceArticleID:      nullUUID(&snapshot.ArticleID),
			SourceArticleVersion: pgtype.Int4{Int32: snapshot.Version, Valid: true},
			EstimatedMinutes:     lesson.EstimatedMinutes, FileIds: snapshot.FileIDs,
			KbSnapshotID: nullUUID(&snapshotRow.ID), CompanyID: actor.CompanyID, ID: lesson.ID,
		}); updateErr != nil {
			return internal("Не удалось связать урок со снимком статьи", updateErr)
		}
		if len(snapshot.FileIDs) == 0 {
			continue
		}
		jobID := uuid.New()
		if _, jobErr := queries.CreateFileCloneJob(ctx, db.CreateFileCloneJobParams{
			ID: jobID, CompanyID: actor.CompanyID, OperationType: "kb_snapshot", AggregateID: courseID,
			IdempotencyKey: requestKey, SourceOwnerType: "kb_article_version", SourceOwnerID: snapshot.ArticleVersionID,
			TargetOwnerType: "course_version", TargetOwnerID: versionID, CreatedAt: now,
		}); jobErr != nil {
			return internal("Не удалось создать задачу копирования файлов статьи", jobErr)
		}
		if _, itemErr := queries.AddFileCloneJobItems(ctx, db.AddFileCloneJobItemsParams{
			UpdatedAt: now, SourceFileIds: snapshot.FileIDs, CompanyID: actor.CompanyID, JobID: jobID,
		}); itemErr != nil {
			return internal("Не удалось сохранить файлы статьи для копирования", itemErr)
		}
	}
	return nil
}

type UpdateCourseInput struct {
	ID           uuid.UUID
	Title        *string
	Description  *string
	Status       *string
	Sequential   *bool
	DeadlineDays *int32
	Visibility   *string
}

func (s *Service) UpdateCourse(ctx context.Context, actor Actor, input UpdateCourseInput) (Course, error) {
	queries := db.New(s.pool)
	currentCourse, err := s.requireCourseEditAccess(ctx, queries, actor, input.ID)
	if err != nil {
		return Course{}, err
	}
	params := db.UpdateCourseParams{
		CompanyID: actor.CompanyID, ID: input.ID, UpdatedAt: s.now().UTC(),
	}
	if input.Title != nil {
		title, err := requiredText(*input.Title, "Укажите название курса")
		if err != nil {
			return Course{}, err
		}
		params.Title = nullText(&title)
	}
	if input.Description != nil {
		params.SetDescription = true
		params.Description = nullText(input.Description)
	}
	if input.Status != nil {
		if err = validateCourseStatus(*input.Status); err != nil {
			return Course{}, err
		}
		if *input.Status == "published" && !canPublishCourse(actor, currentCourse) {
			return Course{}, forbidden("Недостаточно прав для публикации этого курса")
		}
		params.Status = nullText(input.Status)
	}
	if input.Sequential != nil {
		params.Sequential = boolNull(input.Sequential)
	}
	if input.DeadlineDays != nil {
		// Zero clears the deadline, mirroring the frontend contract.
		params.SetDeadlineDays = true
		if *input.DeadlineDays > 0 {
			params.DeadlineDays = nullInt4(input.DeadlineDays)
		}
	}
	if input.Visibility != nil {
		if err := validateCourseVisibility(*input.Visibility); err != nil {
			return Course{}, err
		}
		params.Visibility = nullText(input.Visibility)
		if *input.Visibility == "public" && currentCourse.Visibility != "public" {
			linkedArticleIDs, linkedErr := queries.GetLinkedCourseArticleIDs(ctx, db.GetLinkedCourseArticleIDsParams{CompanyID: actor.CompanyID, CourseID: input.ID})
			if linkedErr != nil {
				return Course{}, internal("Не удалось проверить связанные статьи", linkedErr)
			}
			if len(linkedArticleIDs) > 0 && s.kb == nil {
				return Course{}, unavailable("Сервис базы знаний временно недоступен", nil)
			}
			for _, articleID := range linkedArticleIDs {
				if !articleID.Valid {
					continue
				}
				if _, publicErr := s.kb.GetPublicArticle(ctx, articleID.UUID); publicErr != nil {
					return Course{}, validation("Публичный курс может ссылаться только на публичные статьи; используйте режим copy")
				}
			}
		}
	}

	// The legacy course mutation remains an additive compatibility adapter. The
	// editable metadata now belongs to the current immutable-version draft; the
	// legacy courses row is still updated for rolling deployments and old readers.
	if input.Status != nil && *input.Status == "draft" && currentCourse.CurrentDraftVersionID == nil && currentCourse.LatestPublishedVersionID != nil {
		createdDraft, createErr := s.CreateCourseDraft(ctx, actor, input.ID)
		if createErr != nil {
			return Course{}, createErr
		}
		currentCourse.CurrentDraftVersionID = &createdDraft.ID
	}
	if currentCourse.CurrentDraftVersionID != nil && (input.Title != nil || input.Description != nil || input.Sequential != nil || input.DeadlineDays != nil) {
		var versionDeadline *int32
		if input.DeadlineDays != nil && *input.DeadlineDays > 0 {
			versionDeadline = input.DeadlineDays
		}
		if _, updateErr := s.UpdateCourseDraft(ctx, actor, UpdateCourseDraftInput{
			CourseID: input.ID, Title: input.Title, Description: input.Description,
			Sequential: input.Sequential, DefaultInternalDeadlineDays: versionDeadline,
			SetDefaultDeadline: input.DeadlineDays != nil,
		}); updateErr != nil {
			return Course{}, updateErr
		}
	}
	if input.Status != nil && *input.Status == "published" && currentCourse.CurrentDraftVersionID != nil {
		if _, publishErr := s.PublishCourseVersion(ctx, actor, input.ID, "legacy-update:"+uuid.NewString()); publishErr != nil {
			return Course{}, publishErr
		}
	}
	row, err := queries.UpdateCourse(ctx, params)
	if err != nil {
		if isNoRows(err) {
			return Course{}, notFound("Курс")
		}
		return Course{}, internal("Не удалось обновить курс", err)
	}
	_ = row
	return s.GetCourse(ctx, actor, input.ID)
}

func (s *Service) ArchiveCourse(ctx context.Context, actor Actor, id uuid.UUID) (Course, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Course{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return Course{}, notFound("Курс")
		}
		return Course{}, internal("Не удалось получить курс", err)
	}
	before := courseFromRow(row)
	if before.LifecycleStatus == "deleted" {
		return Course{}, conflict("Курс удалён")
	}
	if before.LifecycleStatus == "archived" {
		return Course{}, conflict("Курс уже находится в архиве")
	}
	if !canArchiveCourse(actor, before) {
		return Course{}, forbidden("Недостаточно прав для архивирования этого курса")
	}
	now := s.now().UTC()
	updatedRow, err := queries.ArchiveCourse(ctx, db.ArchiveCourseParams{
		ArchivedAt: nullTimestamptz(&now), ArchivedByID: nullUUID(&actor.UserID),
		CompanyID: actor.CompanyID, ID: id,
	})
	if err != nil {
		return Course{}, internal("Не удалось архивировать курс", err)
	}
	// Lifecycle semantics do not depend on who owns the course: once the root
	// is archived, every eligible unfinished run is frozen in the same transaction.
	// Restore deliberately leaves these runs frozen; reactivation/reassignment
	// is a separate explicit command.
	if _, err = queries.FreezeCourseEnrollmentsForArchive(ctx, db.FreezeCourseEnrollmentsForArchiveParams{
		FrozenAt: nullTimestamptz(&now), CompanyID: actor.CompanyID, CourseID: id,
	}); err != nil {
		return Course{}, internal("Не удалось заморозить прохождения архивного курса", err)
	}
	after := courseFromRow(updatedRow)
	if err = s.auditCourse(ctx, queries, actor, "course_archived", &before, after); err != nil {
		return Course{}, err
	}
	if err = s.emitCourseLifecycleChanged(ctx, queries, actor, before, after); err != nil {
		return Course{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Course{}, internal("Не удалось архивировать курс", err)
	}
	return after, nil
}

func (s *Service) RestoreCourse(ctx context.Context, actor Actor, id uuid.UUID) (Course, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Course{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return Course{}, notFound("Курс")
		}
		return Course{}, internal("Не удалось получить курс", err)
	}
	before := courseFromRow(row)
	if before.LifecycleStatus == "deleted" {
		return Course{}, conflict("Курс удалён")
	}
	if before.LifecycleStatus != "archived" {
		return Course{}, conflict("Курс не находится в архиве")
	}
	if !canEditCourse(actor, before) {
		return Course{}, forbidden("Недостаточно прав для восстановления этого курса")
	}
	afterRow, err := queries.RestoreCourse(ctx, db.RestoreCourseParams{
		RestoredAt: s.now().UTC(), CompanyID: actor.CompanyID, ID: id,
	})
	if err != nil {
		return Course{}, internal("Не удалось восстановить курс", err)
	}
	after := courseFromRow(afterRow)
	if err = s.auditCourse(ctx, queries, actor, "course_restored", &before, after); err != nil {
		return Course{}, err
	}
	if err = s.emitCourseLifecycleChanged(ctx, queries, actor, before, after); err != nil {
		return Course{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Course{}, internal("Не удалось восстановить курс", err)
	}
	return after, nil
}

func (s *Service) DeleteCourse(ctx context.Context, actor Actor, id uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return notFound("Курс")
		}
		return internal("Не удалось получить курс", err)
	}
	before := courseFromRow(row)
	if before.LifecycleStatus == "deleted" {
		return conflict("Курс удалён")
	}
	if !canDeleteCourse(actor, before) {
		return forbidden("Недостаточно прав для удаления этого курса")
	}
	now := s.now().UTC()
	deletedRow, err := queries.SoftDeleteCourse(ctx, db.SoftDeleteCourseParams{
		DeletedAt: nullTimestamptz(&now), DeletedByID: nullUUID(&actor.UserID),
		CompanyID: actor.CompanyID, ID: id,
	})
	if err != nil {
		return internal("Не удалось удалить курс", err)
	}
	if _, err = queries.CloseCourseEnrollmentsForDelete(ctx, db.CloseCourseEnrollmentsForDeleteParams{
		ClosedAt: now, CompanyID: actor.CompanyID, CourseID: id,
	}); err != nil {
		return internal("Не удалось закрыть прохождения удалённого курса", err)
	}
	if _, err = queries.CloseExternalCampaignsForCourse(ctx, db.CloseExternalCampaignsForCourseParams{
		ChangedAt: nullTimestamptz(&now), CompanyID: actor.CompanyID, CourseID: id,
	}); err != nil {
		return internal("Не удалось закрыть кампании удалённого курса", err)
	}
	if _, err = queries.CloseExternalPersonalAccessesForCourse(ctx, db.CloseExternalPersonalAccessesForCourseParams{
		ChangedAt: nullTimestamptz(&now), CompanyID: actor.CompanyID, CourseID: id,
	}); err != nil {
		return internal("Не удалось закрыть персональные доступы удалённого курса", err)
	}
	after := courseFromRow(deletedRow)
	if err = s.auditCourse(ctx, queries, actor, "course_deleted", &before, after); err != nil {
		return err
	}
	if err = s.emitCourseLifecycleChanged(ctx, queries, actor, before, after); err != nil {
		return err
	}
	if err = s.emit(ctx, queries, actor.CompanyID, id, actor.UserID, "teamos.academy.course.deleted.v1",
		&eventsv1.AcademyCourseDeletedPayload{
			CourseId: id.String(), OwnerType: courseOwnerTypeEventPointer(before.OwnerType),
			OwnerUserId: optionalUUIDStringValue(before.OwnerUserID), DeletedById: optionalUUIDStringValue(&actor.UserID),
			DeletedAt: timestamppb.New(now), CourseTitle: optionalStringValue(before.Title),
		}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось удалить курс", err)
	}
	return nil
}

func (s *Service) emitCourseLifecycleChanged(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	before Course,
	after Course,
) error {
	return s.emit(ctx, queries, actor.CompanyID, after.ID, actor.UserID,
		"teamos.academy.course_lifecycle.changed.v1",
		&eventsv1.AcademyCourseLifecycleChangedPayload{
			CourseId: after.ID.String(), PreviousStatus: courseLifecycleToEvent(before.LifecycleStatus),
			Status: courseLifecycleToEvent(after.LifecycleStatus),
		})
}

func requiredText(value, message string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", validation(message)
	}
	return trimmed, nil
}

func validateCourseStatus(status string) error {
	if status != "draft" && status != "published" {
		return validation("Некорректный статус курса")
	}
	return nil
}

func validateCourseVisibility(visibility string) error {
	switch visibility {
	case "public", "company", "restricted":
		return nil
	default:
		return validation("Некорректная видимость курса")
	}
}

func normalizeDeadlineDays(value *int32) (*int32, error) {
	if value == nil {
		return nil, nil
	}
	if *value == 0 {
		return nil, nil
	}
	if *value < 0 {
		return nil, validation("Дедлайн курса должен быть положительным числом дней")
	}
	return value, nil
}

func optionalUUIDStringValue(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	converted := value.String()
	return &converted
}

func optionalStringValue(value string) *string { return &value }

func courseOwnerTypeEventPointer(value string) *eventsv1.AcademyCourseOwnerType {
	converted := courseOwnerTypeToEvent(value)
	return &converted
}

func courseOwnerTypeToEvent(value string) eventsv1.AcademyCourseOwnerType {
	switch value {
	case "company":
		return eventsv1.AcademyCourseOwnerType_ACADEMY_COURSE_OWNER_TYPE_COMPANY
	case "partner":
		return eventsv1.AcademyCourseOwnerType_ACADEMY_COURSE_OWNER_TYPE_PARTNER
	default:
		return eventsv1.AcademyCourseOwnerType_ACADEMY_COURSE_OWNER_TYPE_UNSPECIFIED
	}
}

func courseLifecycleToEvent(value string) eventsv1.AcademyCourseLifecycleStatus {
	switch value {
	case "active":
		return eventsv1.AcademyCourseLifecycleStatus_ACADEMY_COURSE_LIFECYCLE_STATUS_ACTIVE
	case "archived":
		return eventsv1.AcademyCourseLifecycleStatus_ACADEMY_COURSE_LIFECYCLE_STATUS_ARCHIVED
	case "deleted":
		return eventsv1.AcademyCourseLifecycleStatus_ACADEMY_COURSE_LIFECYCLE_STATUS_DELETED
	default:
		return eventsv1.AcademyCourseLifecycleStatus_ACADEMY_COURSE_LIFECYCLE_STATUS_UNSPECIFIED
	}
}

func courseDistributionToEvent(value string) eventsv1.AcademyCourseDistributionStatus {
	switch value {
	case "active":
		return eventsv1.AcademyCourseDistributionStatus_ACADEMY_COURSE_DISTRIBUTION_STATUS_ACTIVE
	case "paused":
		return eventsv1.AcademyCourseDistributionStatus_ACADEMY_COURSE_DISTRIBUTION_STATUS_PAUSED
	case "blocked":
		return eventsv1.AcademyCourseDistributionStatus_ACADEMY_COURSE_DISTRIBUTION_STATUS_BLOCKED
	default:
		return eventsv1.AcademyCourseDistributionStatus_ACADEMY_COURSE_DISTRIBUTION_STATUS_UNSPECIFIED
	}
}
