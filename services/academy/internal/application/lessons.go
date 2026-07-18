package application

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

// defaultLessonContent mirrors db.richText('Новый урок') from the frontend mock.
var defaultLessonContent = json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Новый урок"}]}]}`)

func (s *Service) GetLessons(ctx context.Context, actor Actor, courseID *uuid.UUID) ([]Lesson, error) {
	queries := db.New(s.pool)
	if courseID != nil {
		if err := s.requireCourseAccess(ctx, queries, actor, *courseID); err != nil {
			return nil, err
		}
		rows, err := queries.GetCourseLessons(ctx, db.GetCourseLessonsParams{
			CompanyID: actor.CompanyID, CourseID: *courseID,
		})
		if err != nil {
			return nil, internal("Не удалось получить уроки", err)
		}
		return lessonsFromRows(rows), nil
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
			return []Lesson{}, nil
		}
		rows, err := queries.GetLessonsByCourseIds(ctx, db.GetLessonsByCourseIdsParams{
			CompanyID: actor.CompanyID, CourseIds: courseIDs,
		})
		if err != nil {
			return nil, internal("Не удалось получить уроки", err)
		}
		return lessonsFromRows(rows), nil
	}
	rows, err := queries.GetLessons(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить уроки", err)
	}
	return lessonsFromRows(rows), nil
}

type CreateLessonInput struct {
	CourseID        uuid.UUID
	SectionID       uuid.UUID
	Title           string
	Content         json.RawMessage
	SourceArticleID *uuid.UUID
	SourceMode      *string
}

func (s *Service) CreateLesson(ctx context.Context, actor Actor, input CreateLessonInput) (Lesson, error) {
	if !actor.canManage() {
		return Lesson{}, forbidden("Недостаточно прав для изменения академии")
	}
	title, err := requiredText(input.Title, "Укажите название урока")
	if err != nil {
		return Lesson{}, err
	}
	if input.SourceMode != nil && *input.SourceMode != "link" && *input.SourceMode != "copy" {
		return Lesson{}, validation("Некорректный режим импорта статьи")
	}
	if len(input.Content) > 0 {
		normalizedContent, normalizeErr := richtext.Normalize(input.Content)
		if normalizeErr != nil {
			return Lesson{}, validation("Некорректное содержимое урока")
		}
		input.Content = normalizedContent
	}

	var sourceArticle *KbArticle
	if input.SourceArticleID != nil {
		if s.kb == nil {
			return Lesson{}, unavailable("Сервис базы знаний временно недоступен", nil)
		}
		article, fetchErr := s.kb.GetArticle(ctx, actor.Token, *input.SourceArticleID)
		if fetchErr != nil {
			return Lesson{}, unavailable("Не удалось получить статью базы знаний", fetchErr)
		}
		sourceArticle = &article
	}
	if input.SourceMode != nil && *input.SourceMode == "link" && sourceArticle == nil {
		return Lesson{}, validation("Для link-урока укажите статью базы знаний")
	}

	content := input.Content
	if input.SourceMode != nil && *input.SourceMode == "link" && sourceArticle != nil {
		content = sourceArticle.Content
	} else if len(content) == 0 && sourceArticle != nil {
		content = sourceArticle.Content
	} else if len(content) == 0 {
		content = defaultLessonContent
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Lesson{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockCourseOrder(ctx, input.CourseID); err != nil {
		return Lesson{}, internal("Не удалось заблокировать порядок курса", err)
	}

	section, err := queries.GetCourseSection(ctx, db.GetCourseSectionParams{
		CompanyID: actor.CompanyID, ID: input.SectionID,
	})
	if err != nil {
		if isNoRows(err) {
			return Lesson{}, notFound("Раздел курса")
		}
		return Lesson{}, internal("Не удалось проверить раздел курса", err)
	}
	if section.CourseID != input.CourseID {
		return Lesson{}, validation("Раздел не принадлежит указанному курсу")
	}
	siblings, err := queries.CountSectionLessons(ctx, db.CountSectionLessonsParams{
		CompanyID: actor.CompanyID, SectionID: input.SectionID,
	})
	if err != nil {
		return Lesson{}, internal("Не удалось получить уроки раздела", err)
	}

	var sourceTitle *string
	if sourceArticle != nil {
		sourceTitle = &sourceArticle.Title
	}
	row, err := queries.CreateLesson(ctx, db.CreateLessonParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: input.CourseID,
		SectionID: input.SectionID, Title: title, Order: int32(siblings),
		Content:            content,
		SourceArticleID:    nullUUID(input.SourceArticleID),
		SourceArticleTitle: nullText(sourceTitle),
		SourceMode:         nullText(input.SourceMode),
	})
	if err != nil {
		return Lesson{}, internal("Не удалось создать урок", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Lesson{}, internal("Не удалось сохранить урок", err)
	}
	return lessonFromRow(row), nil
}

type UpdateLessonInput struct {
	ID              uuid.UUID
	Title           *string
	Content         json.RawMessage
	SourceArticleID *uuid.UUID
	SourceMode      *string
}

func (s *Service) UpdateLesson(ctx context.Context, actor Actor, input UpdateLessonInput) (Lesson, error) {
	if !actor.canManage() {
		return Lesson{}, forbidden("Недостаточно прав для изменения академии")
	}
	if input.SourceMode != nil && *input.SourceMode != "link" && *input.SourceMode != "copy" {
		return Lesson{}, validation("Некорректный режим импорта статьи")
	}
	if len(input.Content) > 0 {
		normalizedContent, normalizeErr := richtext.Normalize(input.Content)
		if normalizeErr != nil {
			return Lesson{}, validation("Некорректное содержимое урока")
		}
		input.Content = normalizedContent
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Lesson{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	current, err := queries.GetLesson(ctx, db.GetLessonParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		if isNoRows(err) {
			return Lesson{}, notFound("Урок")
		}
		return Lesson{}, internal("Не удалось получить урок", err)
	}

	nextMode := textPointer(current.SourceMode)
	if input.SourceMode != nil {
		nextMode = input.SourceMode
	}
	nextArticleID := nullUUIDPointer(current.SourceArticleID)
	if input.SourceArticleID != nil {
		nextArticleID = input.SourceArticleID
	}
	isLink := nextMode != nil && *nextMode == "link"
	// Link lessons replicate article content; direct edits are rejected (§10.2).
	if isLink && len(input.Content) > 0 {
		return Lesson{}, validation("Контент link-урока синхронизируется с базой знаний и не редактируется")
	}

	var sourceArticle *KbArticle
	if input.SourceArticleID != nil || (isLink && nextArticleID != nil) {
		articleID := nextArticleID
		if articleID != nil {
			if s.kb == nil {
				return Lesson{}, unavailable("Сервис базы знаний временно недоступен", nil)
			}
			article, fetchErr := s.kb.GetArticle(ctx, actor.Token, *articleID)
			if fetchErr != nil {
				return Lesson{}, unavailable("Не удалось получить статью базы знаний", fetchErr)
			}
			sourceArticle = &article
		}
	}

	params := db.UpdateLessonParams{CompanyID: actor.CompanyID, ID: input.ID}
	if input.Title != nil {
		title, titleErr := requiredText(*input.Title, "Укажите название урока")
		if titleErr != nil {
			return Lesson{}, titleErr
		}
		params.Title = nullText(&title)
	}
	if input.SourceArticleID != nil {
		params.SetSourceArticle = true
		params.SourceArticleID = nullUUID(input.SourceArticleID)
	}
	if input.SourceMode != nil {
		params.SetSourceMode = true
		params.SourceMode = nullText(input.SourceMode)
	}
	switch {
	case isLink && sourceArticle != nil:
		params.Content = sourceArticle.Content
		params.SetSourceArticleTitle = true
		params.SourceArticleTitle = nullText(&sourceArticle.Title)
	case input.SourceArticleID != nil && sourceArticle != nil && len(input.Content) == 0:
		params.Content = sourceArticle.Content
		params.SetSourceArticleTitle = true
		params.SourceArticleTitle = nullText(&sourceArticle.Title)
	case len(input.Content) > 0:
		params.Content = input.Content
	}

	row, err := queries.UpdateLesson(ctx, params)
	if err != nil {
		return Lesson{}, internal("Не удалось обновить урок", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Lesson{}, internal("Не удалось сохранить урок", err)
	}
	return lessonFromRow(row), nil
}

func (s *Service) DeleteLesson(ctx context.Context, actor Actor, id uuid.UUID) error {
	if !actor.canManage() {
		return forbidden("Недостаточно прав для изменения академии")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	lesson, err := queries.GetLesson(ctx, db.GetLessonParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return notFound("Урок")
		}
		return internal("Не удалось получить урок", err)
	}
	if err = queries.RemoveLessonsFromProgress(ctx, db.RemoveLessonsFromProgressParams{
		CompanyID: actor.CompanyID, CourseID: lesson.CourseID, LessonIds: []uuid.UUID{id},
	}); err != nil {
		return internal("Не удалось обновить прогресс", err)
	}
	if _, err = queries.DeleteLesson(ctx, db.DeleteLessonParams{CompanyID: actor.CompanyID, ID: id}); err != nil {
		return internal("Не удалось удалить урок", err)
	}
	if err = queries.RecomputeCourseProgressAfterLessonDelete(ctx, db.RecomputeCourseProgressAfterLessonDeleteParams{
		CompanyID: actor.CompanyID, CourseID: lesson.CourseID,
	}); err != nil {
		return internal("Не удалось пересчитать прогресс", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось удалить урок", err)
	}
	return nil
}

type MoveLessonInput struct {
	ID        uuid.UUID
	SectionID uuid.UUID
	Order     int32
}

func (s *Service) MoveLesson(ctx context.Context, actor Actor, input MoveLessonInput) (Lesson, error) {
	if !actor.canManage() {
		return Lesson{}, forbidden("Недостаточно прав для изменения академии")
	}
	if input.Order < 0 {
		return Lesson{}, validation("Порядок урока не может быть отрицательным")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Lesson{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	lesson, err := queries.GetLesson(ctx, db.GetLessonParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		if isNoRows(err) {
			return Lesson{}, notFound("Урок")
		}
		return Lesson{}, internal("Не удалось получить урок", err)
	}
	if err = queries.LockCourseOrder(ctx, lesson.CourseID); err != nil {
		return Lesson{}, internal("Не удалось заблокировать порядок курса", err)
	}
	section, err := queries.GetCourseSection(ctx, db.GetCourseSectionParams{
		CompanyID: actor.CompanyID, ID: input.SectionID,
	})
	if err != nil {
		if isNoRows(err) {
			return Lesson{}, notFound("Раздел курса")
		}
		return Lesson{}, internal("Не удалось проверить раздел курса", err)
	}
	if section.CourseID != lesson.CourseID {
		return Lesson{}, validation("Раздел не принадлежит курсу урока")
	}
	oldSectionID := lesson.SectionID

	// Same renumbering as the mock: siblings at or past the slot shift by one.
	siblings, err := queries.GetSectionLessonsForUpdate(ctx, db.GetSectionLessonsForUpdateParams{
		CompanyID: actor.CompanyID, SectionID: input.SectionID,
	})
	if err != nil {
		return Lesson{}, internal("Не удалось получить уроки раздела", err)
	}
	maxOrder := int32(len(siblings))
	if oldSectionID == input.SectionID && maxOrder > 0 {
		maxOrder--
	}
	if input.Order > maxOrder {
		input.Order = maxOrder
	}
	moved, err := queries.MoveLessonRow(ctx, db.MoveLessonRowParams{
		CompanyID: actor.CompanyID, ID: input.ID,
		SectionID: input.SectionID, Order: input.Order,
	})
	if err != nil {
		return Lesson{}, internal("Не удалось переместить урок", err)
	}
	index := int32(0)
	for _, sibling := range siblings {
		if sibling.ID == input.ID {
			continue
		}
		nextOrder := index
		if index >= input.Order {
			nextOrder = index + 1
		}
		if nextOrder != sibling.Order {
			if err = queries.SetLessonOrder(ctx, db.SetLessonOrderParams{
				CompanyID: actor.CompanyID, ID: sibling.ID, Order: nextOrder,
			}); err != nil {
				return Lesson{}, internal("Не удалось перенумеровать уроки", err)
			}
		}
		index++
	}
	if oldSectionID != input.SectionID {
		if err = queries.NormalizeSectionLessonOrder(ctx, db.NormalizeSectionLessonOrderParams{
			CompanyID: actor.CompanyID, SectionID: oldSectionID,
		}); err != nil {
			return Lesson{}, internal("Не удалось перенумеровать исходный раздел", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Lesson{}, internal("Не удалось сохранить порядок уроков", err)
	}
	return lessonFromRow(moved), nil
}
