package application

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	domainversion "github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) CreateCourseVersionSection(
	ctx context.Context,
	actor Actor,
	versionID uuid.UUID,
	title string,
) (CourseVersionSection, error) {
	title, err := requiredText(title, "Укажите название раздела")
	if err != nil {
		return CourseVersionSection{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersionSection{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	version, err := s.lockEditableCourseVersion(ctx, queries, actor, versionID)
	if err != nil {
		return CourseVersionSection{}, err
	}
	sections, err := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{
		CompanyID: actor.CompanyID, CourseVersionID: version.ID,
	})
	if err != nil {
		return CourseVersionSection{}, internal("Не удалось получить разделы версии", err)
	}
	sectionID := uuid.New()
	row, err := queries.CreateCourseVersionSection(ctx, db.CreateCourseVersionSectionParams{
		ID: sectionID, StableKey: sectionID, Title: title, OrderValue: int32(len(sections)),
		CompanyID: actor.CompanyID, CourseVersionID: version.ID,
	})
	if err != nil {
		return CourseVersionSection{}, internal("Не удалось создать раздел версии", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseVersionSection{}, internal("Не удалось сохранить раздел версии", err)
	}
	return courseVersionSectionFromRow(row), nil
}

type UpdateCourseVersionSectionInput struct {
	ID    uuid.UUID
	Title *string
	Order *int32
}

func (s *Service) UpdateCourseVersionSection(
	ctx context.Context,
	actor Actor,
	input UpdateCourseVersionSectionInput,
) (CourseVersionSection, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersionSection{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	current, err := queries.GetCourseVersionSection(ctx, db.GetCourseVersionSectionParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		if isNoRows(err) {
			return CourseVersionSection{}, notFound("Раздел версии")
		}
		return CourseVersionSection{}, internal("Не удалось получить раздел версии", err)
	}
	if _, err = s.lockEditableCourseVersion(ctx, queries, actor, current.CourseVersionID); err != nil {
		return CourseVersionSection{}, err
	}
	if input.Title != nil {
		value, validationErr := requiredText(*input.Title, "Укажите название раздела")
		if validationErr != nil {
			return CourseVersionSection{}, validationErr
		}
		input.Title = &value
	}
	if input.Order != nil {
		sections, listErr := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{
			CompanyID: actor.CompanyID, CourseVersionID: current.CourseVersionID,
		})
		if listErr != nil {
			return CourseVersionSection{}, internal("Не удалось получить разделы версии", listErr)
		}
		if *input.Order < 0 || int(*input.Order) >= len(sections) {
			return CourseVersionSection{}, validation("Некорректный порядок раздела")
		}
		sections = moveVersionSection(sections, input.ID, int(*input.Order))
		for index, section := range sections {
			var title pgtype.Text
			if section.ID == input.ID && input.Title != nil {
				title = nullText(input.Title)
			}
			if _, updateErr := queries.UpdateCourseVersionSection(ctx, db.UpdateCourseVersionSectionParams{
				Title: title, OrderValue: pgtype.Int4{Int32: int32(index), Valid: true},
				CompanyID: actor.CompanyID, ID: section.ID,
			}); updateErr != nil {
				return CourseVersionSection{}, internal("Не удалось изменить порядок разделов", updateErr)
			}
		}
	} else if input.Title != nil {
		if _, err = queries.UpdateCourseVersionSection(ctx, db.UpdateCourseVersionSectionParams{
			Title: nullText(input.Title), CompanyID: actor.CompanyID, ID: input.ID,
		}); err != nil {
			return CourseVersionSection{}, internal("Не удалось обновить раздел версии", err)
		}
	}
	row, err := queries.GetCourseVersionSection(ctx, db.GetCourseVersionSectionParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		return CourseVersionSection{}, internal("Не удалось получить обновлённый раздел", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseVersionSection{}, internal("Не удалось сохранить раздел версии", err)
	}
	return courseVersionSectionFromRow(row), nil
}

func (s *Service) DeleteCourseVersionSection(ctx context.Context, actor Actor, id uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	current, err := queries.GetCourseVersionSection(ctx, db.GetCourseVersionSectionParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return notFound("Раздел версии")
		}
		return internal("Не удалось получить раздел версии", err)
	}
	if _, err = s.lockEditableCourseVersion(ctx, queries, actor, current.CourseVersionID); err != nil {
		return err
	}
	if affected, deleteErr := queries.DeleteCourseVersionSection(ctx, db.DeleteCourseVersionSectionParams{
		CompanyID: actor.CompanyID, ID: id,
	}); deleteErr != nil || affected != 1 {
		return internal("Не удалось удалить раздел версии", deleteErr)
	}
	sections, err := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{
		CompanyID: actor.CompanyID, CourseVersionID: current.CourseVersionID,
	})
	if err != nil {
		return internal("Не удалось обновить порядок разделов", err)
	}
	for index, section := range sections {
		if _, err = queries.UpdateCourseVersionSection(ctx, db.UpdateCourseVersionSectionParams{
			OrderValue: pgtype.Int4{Int32: int32(index), Valid: true}, CompanyID: actor.CompanyID, ID: section.ID,
		}); err != nil {
			return internal("Не удалось обновить порядок разделов", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось удалить раздел версии", err)
	}
	return nil
}

type CreateCourseVersionLessonInput struct {
	VersionID            uuid.UUID
	SectionVersionID     uuid.UUID
	Title                string
	Content              json.RawMessage
	SourceType           *string
	SourceArticleID      *uuid.UUID
	SourceArticleVersion *int32
	EstimatedMinutes     *int32
}

func (s *Service) CreateCourseVersionLesson(
	ctx context.Context,
	actor Actor,
	input CreateCourseVersionLessonInput,
) (CourseVersionLesson, error) {
	title, err := requiredText(input.Title, "Укажите название урока")
	if err != nil {
		return CourseVersionLesson{}, err
	}
	content := input.Content
	if len(content) == 0 {
		content = json.RawMessage(`{"type":"doc"}`)
	}
	sourceType := "manual"
	if input.SourceType != nil {
		sourceType = *input.SourceType
	}
	if err = validateVersionLessonFields(content, sourceType, input.SourceArticleID, input.SourceArticleVersion, input.EstimatedMinutes); err != nil {
		return CourseVersionLesson{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersionLesson{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	version, err := s.lockEditableCourseVersion(ctx, queries, actor, input.VersionID)
	if err != nil {
		return CourseVersionLesson{}, err
	}
	section, err := queries.GetCourseVersionSection(ctx, db.GetCourseVersionSectionParams{
		CompanyID: actor.CompanyID, ID: input.SectionVersionID,
	})
	if err != nil || section.CourseVersionID != version.ID {
		if isNoRows(err) || err == nil {
			return CourseVersionLesson{}, notFound("Раздел версии")
		}
		return CourseVersionLesson{}, internal("Не удалось проверить раздел версии", err)
	}
	lessons, err := queries.GetCourseVersionLessons(ctx, db.GetCourseVersionLessonsParams{
		CompanyID: actor.CompanyID, CourseVersionID: version.ID,
	})
	if err != nil {
		return CourseVersionLesson{}, internal("Не удалось получить уроки версии", err)
	}
	order := int32(0)
	for _, lesson := range lessons {
		if lesson.SectionVersionID == input.SectionVersionID {
			order++
		}
	}
	lessonID := uuid.New()
	row, err := queries.CreateCourseVersionLesson(ctx, db.CreateCourseVersionLessonParams{
		ID: lessonID, SectionVersionID: input.SectionVersionID, StableKey: lessonID,
		Title: title, OrderValue: order, Content: content, SourceType: sourceType,
		SourceArticleID: nullUUID(input.SourceArticleID), SourceArticleVersion: nullInt4(input.SourceArticleVersion),
		EstimatedMinutes: nullInt4(input.EstimatedMinutes), CompanyID: actor.CompanyID, CourseVersionID: version.ID,
	})
	if err != nil {
		return CourseVersionLesson{}, internal("Не удалось создать урок версии", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseVersionLesson{}, internal("Не удалось сохранить урок версии", err)
	}
	return courseVersionLessonFromRow(row), nil
}

type UpdateCourseVersionLessonInput struct {
	ID                   uuid.UUID
	Title                *string
	Content              json.RawMessage
	SetContent           bool
	SourceType           *string
	SourceArticleID      *uuid.UUID
	SourceArticleVersion *int32
	EstimatedMinutes     *int32
}

func (s *Service) UpdateCourseVersionLesson(
	ctx context.Context,
	actor Actor,
	input UpdateCourseVersionLessonInput,
) (CourseVersionLesson, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersionLesson{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetCourseVersionLesson(ctx, db.GetCourseVersionLessonParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		if isNoRows(err) {
			return CourseVersionLesson{}, notFound("Урок версии")
		}
		return CourseVersionLesson{}, internal("Не удалось получить урок версии", err)
	}
	if _, err = s.lockEditableCourseVersion(ctx, queries, actor, row.CourseVersionID); err != nil {
		return CourseVersionLesson{}, err
	}
	current := courseVersionLessonFromRow(row)
	if input.Title != nil {
		current.Title, err = requiredText(*input.Title, "Укажите название урока")
		if err != nil {
			return CourseVersionLesson{}, err
		}
	}
	if input.SetContent {
		current.Content = append(json.RawMessage(nil), input.Content...)
	}
	if input.SourceType != nil {
		current.SourceType = *input.SourceType
		if current.SourceType == "manual" {
			current.SourceArticleID = nil
			current.SourceArticleVersion = nil
		}
	}
	if input.SourceArticleID != nil {
		current.SourceArticleID = input.SourceArticleID
	}
	if input.SourceArticleVersion != nil {
		current.SourceArticleVersion = input.SourceArticleVersion
	}
	if input.EstimatedMinutes != nil {
		current.EstimatedMinutes = input.EstimatedMinutes
	}
	if err = validateVersionLessonFields(current.Content, current.SourceType, current.SourceArticleID, current.SourceArticleVersion, current.EstimatedMinutes); err != nil {
		return CourseVersionLesson{}, err
	}
	updatedRow, err := queries.UpdateCourseVersionLesson(ctx, db.UpdateCourseVersionLessonParams{
		Title: current.Title, Content: current.Content, SourceType: current.SourceType,
		SourceArticleID: nullUUID(current.SourceArticleID), SourceArticleVersion: nullInt4(current.SourceArticleVersion),
		SourceTemplateID: nullUUID(current.SourceTemplateID), SourceTemplateVersionID: nullUUID(current.SourceTemplateVersionID),
		EstimatedMinutes: nullInt4(current.EstimatedMinutes), CompanyID: actor.CompanyID, ID: current.ID,
	})
	if err != nil {
		return CourseVersionLesson{}, internal("Не удалось обновить урок версии", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseVersionLesson{}, internal("Не удалось сохранить урок версии", err)
	}
	return courseVersionLessonFromRow(updatedRow), nil
}

func (s *Service) DeleteCourseVersionLesson(ctx context.Context, actor Actor, id uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetCourseVersionLesson(ctx, db.GetCourseVersionLessonParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return notFound("Урок версии")
		}
		return internal("Не удалось получить урок версии", err)
	}
	if _, err = s.lockEditableCourseVersion(ctx, queries, actor, row.CourseVersionID); err != nil {
		return err
	}
	if affected, deleteErr := queries.DeleteCourseVersionLesson(ctx, db.DeleteCourseVersionLessonParams{
		CompanyID: actor.CompanyID, ID: id,
	}); deleteErr != nil || affected != 1 {
		return internal("Не удалось удалить урок версии", deleteErr)
	}
	if err = s.normalizeVersionLessonOrders(ctx, queries, actor.CompanyID, row.CourseVersionID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось удалить урок версии", err)
	}
	return nil
}

type MoveCourseVersionLessonInput struct {
	ID               uuid.UUID
	SectionVersionID uuid.UUID
	Order            int32
}

func (s *Service) MoveCourseVersionLesson(
	ctx context.Context,
	actor Actor,
	input MoveCourseVersionLessonInput,
) (CourseVersionLesson, error) {
	if input.Order < 0 {
		return CourseVersionLesson{}, validation("Некорректный порядок урока")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersionLesson{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetCourseVersionLesson(ctx, db.GetCourseVersionLessonParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		if isNoRows(err) {
			return CourseVersionLesson{}, notFound("Урок версии")
		}
		return CourseVersionLesson{}, internal("Не удалось получить урок версии", err)
	}
	if _, err = s.lockEditableCourseVersion(ctx, queries, actor, row.CourseVersionID); err != nil {
		return CourseVersionLesson{}, err
	}
	section, err := queries.GetCourseVersionSection(ctx, db.GetCourseVersionSectionParams{CompanyID: actor.CompanyID, ID: input.SectionVersionID})
	if err != nil || section.CourseVersionID != row.CourseVersionID {
		if isNoRows(err) || err == nil {
			return CourseVersionLesson{}, notFound("Раздел версии")
		}
		return CourseVersionLesson{}, internal("Не удалось проверить раздел версии", err)
	}
	lessons, err := queries.GetCourseVersionLessons(ctx, db.GetCourseVersionLessonsParams{
		CompanyID: actor.CompanyID, CourseVersionID: row.CourseVersionID,
	})
	if err != nil {
		return CourseVersionLesson{}, internal("Не удалось получить уроки версии", err)
	}
	targetCount := 0
	for _, lesson := range lessons {
		if lesson.SectionVersionID == input.SectionVersionID && lesson.ID != input.ID {
			targetCount++
		}
	}
	if int(input.Order) > targetCount {
		return CourseVersionLesson{}, validation("Некорректный порядок урока")
	}
	if _, err = queries.MoveCourseVersionLesson(ctx, db.MoveCourseVersionLessonParams{
		OrderValue: input.Order, CompanyID: actor.CompanyID, ID: input.ID, SectionVersionID: input.SectionVersionID,
	}); err != nil {
		return CourseVersionLesson{}, internal("Не удалось переместить урок версии", err)
	}
	if err = s.normalizeVersionLessonOrdersWithPriority(ctx, queries, actor.CompanyID, row.CourseVersionID, input.ID, input.SectionVersionID, input.Order); err != nil {
		return CourseVersionLesson{}, err
	}
	updated, err := queries.GetCourseVersionLesson(ctx, db.GetCourseVersionLessonParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		return CourseVersionLesson{}, internal("Не удалось получить перемещённый урок", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseVersionLesson{}, internal("Не удалось сохранить перемещение урока", err)
	}
	return courseVersionLessonFromRow(updated), nil
}

type UpsertCourseVersionQuizInput struct {
	LessonVersionID uuid.UUID
	Questions       json.RawMessage
	PassingScore    int32
	MaxAttempts     *int32
}

func (s *Service) UpsertCourseVersionQuiz(
	ctx context.Context,
	actor Actor,
	input UpsertCourseVersionQuizInput,
) (CourseVersionQuiz, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseVersionQuiz{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	lesson, err := queries.GetCourseVersionLesson(ctx, db.GetCourseVersionLessonParams{
		CompanyID: actor.CompanyID, ID: input.LessonVersionID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseVersionQuiz{}, notFound("Урок версии")
		}
		return CourseVersionQuiz{}, internal("Не удалось получить урок версии", err)
	}
	if _, err = s.lockEditableCourseVersion(ctx, queries, actor, lesson.CourseVersionID); err != nil {
		return CourseVersionQuiz{}, err
	}
	quizID := uuid.New()
	if lesson.QuizVersionID.Valid {
		quizID = lesson.QuizVersionID.UUID
	}
	value := CourseVersionQuiz{
		ID: quizID, CompanyID: actor.CompanyID, CourseVersionID: lesson.CourseVersionID,
		LessonVersionID: lesson.ID, Questions: input.Questions,
		PassingScore: input.PassingScore, MaxAttempts: input.MaxAttempts,
	}
	domainValue, validationErr := domainQuiz(value)
	if validationErr != nil {
		return CourseVersionQuiz{}, validation("Некорректные вопросы теста")
	}
	if validationErr = domainversion.ValidateQuiz(domainValue); validationErr != nil {
		return CourseVersionQuiz{}, validation(validationErr.Error())
	}
	if lesson.QuizVersionID.Valid {
		row, updateErr := queries.UpdateCourseVersionQuiz(ctx, db.UpdateCourseVersionQuizParams{
			Questions: input.Questions, PassingScore: input.PassingScore,
			MaxAttempts: nullInt4(input.MaxAttempts), CompanyID: actor.CompanyID, ID: quizID,
		})
		if updateErr != nil {
			return CourseVersionQuiz{}, internal("Не удалось обновить тест версии", updateErr)
		}
		value = courseVersionQuizFromRow(row)
	} else {
		row, createErr := queries.CreateCourseVersionQuiz(ctx, db.CreateCourseVersionQuizParams{
			ID: quizID, LessonVersionID: lesson.ID, Questions: input.Questions,
			PassingScore: input.PassingScore, MaxAttempts: nullInt4(input.MaxAttempts),
			CompanyID: actor.CompanyID, CourseVersionID: lesson.CourseVersionID,
		})
		if createErr != nil {
			return CourseVersionQuiz{}, internal("Не удалось создать тест версии", createErr)
		}
		value = courseVersionQuizFromCreatedRow(row)
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseVersionQuiz{}, internal("Не удалось сохранить тест версии", err)
	}
	return value, nil
}

func (s *Service) DeleteCourseVersionQuiz(ctx context.Context, actor Actor, lessonID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	lesson, err := queries.GetCourseVersionLesson(ctx, db.GetCourseVersionLessonParams{
		CompanyID: actor.CompanyID, ID: lessonID,
	})
	if err != nil {
		if isNoRows(err) {
			return notFound("Урок версии")
		}
		return internal("Не удалось получить урок версии", err)
	}
	if _, err = s.lockEditableCourseVersion(ctx, queries, actor, lesson.CourseVersionID); err != nil {
		return err
	}
	if !lesson.QuizVersionID.Valid {
		return notFound("Тест версии курса")
	}
	affected, err := queries.DeleteCourseVersionQuiz(ctx, db.DeleteCourseVersionQuizParams{
		CompanyID: actor.CompanyID, ID: lesson.QuizVersionID.UUID,
	})
	if err != nil || affected != 1 {
		return internal("Не удалось удалить тест версии курса", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось сохранить удаление теста", err)
	}
	return nil
}

func (s *Service) lockEditableCourseVersion(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	versionID uuid.UUID,
) (CourseVersion, error) {
	row, err := queries.GetCourseVersionForUpdate(ctx, db.GetCourseVersionForUpdateParams{
		CompanyID: actor.CompanyID, ID: versionID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseVersion{}, notFound("Версия курса")
		}
		return CourseVersion{}, internal("Не удалось получить версию курса", err)
	}
	version := courseVersionFromRow(row)
	if err = s.requireDraftVersionEdit(ctx, queries, actor, version); err != nil {
		return CourseVersion{}, err
	}
	return version, nil
}

func validateVersionLessonFields(
	content json.RawMessage,
	sourceType string,
	sourceArticleID *uuid.UUID,
	sourceArticleVersion, estimatedMinutes *int32,
) error {
	if err := richtext.Validate(content); err != nil {
		return validation("Некорректный TipTap-документ урока")
	}
	if estimatedMinutes != nil && *estimatedMinutes < 1 {
		return validation("Продолжительность урока должна быть не меньше одной минуты")
	}
	if sourceArticleVersion != nil && *sourceArticleVersion < 1 {
		return validation("Версия статьи должна быть больше нуля")
	}
	switch sourceType {
	case "manual":
		if sourceArticleID != nil || sourceArticleVersion != nil {
			return validation("Ручной урок не может ссылаться на статью")
		}
	case "kb_link", "kb_snapshot":
		if sourceArticleID == nil {
			return validation("Для урока из базы знаний требуется статья")
		}
	case "template_snapshot":
		return validation("Урок шаблона создаётся только при инстанцировании шаблона")
	default:
		return validation("Некорректный источник урока")
	}
	return nil
}

func moveVersionSection(rows []db.CourseVersionSection, id uuid.UUID, target int) []db.CourseVersionSection {
	result := make([]db.CourseVersionSection, 0, len(rows))
	var moved db.CourseVersionSection
	for _, row := range rows {
		if row.ID == id {
			moved = row
			continue
		}
		result = append(result, row)
	}
	result = append(result, db.CourseVersionSection{})
	copy(result[target+1:], result[target:])
	result[target] = moved
	return result
}

func (s *Service) normalizeVersionLessonOrders(
	ctx context.Context,
	queries *db.Queries,
	companyID, versionID uuid.UUID,
) error {
	return s.normalizeVersionLessonOrdersWithPriority(ctx, queries, companyID, versionID, uuid.Nil, uuid.Nil, -1)
}

func (s *Service) normalizeVersionLessonOrdersWithPriority(
	ctx context.Context,
	queries *db.Queries,
	companyID, versionID, movedID, targetSectionID uuid.UUID,
	targetOrder int32,
) error {
	rows, err := queries.GetCourseVersionLessons(ctx, db.GetCourseVersionLessonsParams{
		CompanyID: companyID, CourseVersionID: versionID,
	})
	if err != nil {
		return internal("Не удалось получить уроки версии", err)
	}
	bySection := make(map[uuid.UUID][]db.CourseVersionLesson)
	for _, row := range rows {
		if row.ID == movedID {
			continue
		}
		bySection[row.SectionVersionID] = append(bySection[row.SectionVersionID], row)
	}
	if movedID != uuid.Nil {
		var moved db.CourseVersionLesson
		for _, row := range rows {
			if row.ID == movedID {
				moved = row
				break
			}
		}
		list := bySection[targetSectionID]
		index := int(targetOrder)
		list = append(list, db.CourseVersionLesson{})
		copy(list[index+1:], list[index:])
		list[index] = moved
		bySection[targetSectionID] = list
	}
	for _, sectionRows := range bySection {
		for index, row := range sectionRows {
			if _, err = queries.SetCourseVersionLessonOrder(ctx, db.SetCourseVersionLessonOrderParams{
				OrderValue: int32(index), CompanyID: companyID, ID: row.ID,
			}); err != nil {
				return internal("Не удалось обновить порядок уроков", err)
			}
		}
	}
	return nil
}
