package application

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) GetCourses(ctx context.Context, actor Actor) ([]Course, error) {
	queries := db.New(s.pool)
	if actor.Role == "partner" {
		return s.partnerCourses(ctx, queries, actor)
	}
	rows, err := queries.GetCourses(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить курсы", err)
	}
	return coursesFromRows(rows), nil
}

// partnerCourses restricts partners to courses assigned to them directly (§7.2).
func (s *Service) partnerCourses(ctx context.Context, queries *db.Queries, actor Actor) ([]Course, error) {
	assignments, err := queries.GetUserAssignments(ctx, db.GetUserAssignmentsParams{
		CompanyID: actor.CompanyID, UserID: actor.UserID,
	})
	if err != nil {
		return nil, internal("Не удалось получить назначения", err)
	}
	courseIDs := make([]uuid.UUID, 0, len(assignments))
	seen := make(map[uuid.UUID]struct{}, len(assignments))
	for _, assignment := range assignments {
		if _, ok := seen[assignment.CourseID]; ok {
			continue
		}
		seen[assignment.CourseID] = struct{}{}
		courseIDs = append(courseIDs, assignment.CourseID)
	}
	if len(courseIDs) == 0 {
		return []Course{}, nil
	}
	rows, err := queries.GetCoursesByIds(ctx, db.GetCoursesByIdsParams{
		CompanyID: actor.CompanyID, Ids: courseIDs,
	})
	if err != nil {
		return nil, internal("Не удалось получить курсы", err)
	}
	return coursesFromRows(rows), nil
}

func (s *Service) GetCourse(ctx context.Context, actor Actor, id uuid.UUID) (Course, error) {
	row, err := db.New(s.pool).GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return Course{}, notFound("Курс")
		}
		return Course{}, internal("Не удалось получить курс", err)
	}
	return courseFromRow(row), nil
}

type CreateCourseInput struct {
	Title        string
	Description  *string
	Status       *string
	Sequential   *bool
	DeadlineDays *int32
}

func (s *Service) CreateCourse(ctx context.Context, actor Actor, input CreateCourseInput) (Course, error) {
	if !actor.canManage() {
		return Course{}, forbidden("Недостаточно прав для изменения академии")
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
	sequential := true
	if input.Sequential != nil {
		sequential = *input.Sequential
	}
	deadlineDays, err := normalizeDeadlineDays(input.DeadlineDays)
	if err != nil {
		return Course{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Course{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	now := s.now().UTC()
	row, err := queries.CreateCourse(ctx, db.CreateCourseParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Title: title,
		Description: nullText(input.Description), Status: status,
		AuthorID: actor.UserID, Sequential: sequential,
		DeadlineDays: nullInt4(deadlineDays), CreatedAt: now,
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
	if err = tx.Commit(ctx); err != nil {
		return Course{}, internal("Не удалось сохранить курс", err)
	}
	return courseFromRow(row), nil
}

type CreateCourseFromKbInput struct {
	Title        string
	Description  *string
	Sequential   *bool
	DeadlineDays *int32
	Mode         string
	SectionIDs   []uuid.UUID
	ArticleIDs   []uuid.UUID
}

func (s *Service) CreateCourseFromKb(ctx context.Context, actor Actor, input CreateCourseFromKbInput) (Course, error) {
	if !actor.canManage() {
		return Course{}, forbidden("Недостаточно прав для изменения академии")
	}
	title, err := requiredText(input.Title, "Укажите название курса")
	if err != nil {
		return Course{}, err
	}
	if input.Mode != "link" && input.Mode != "copy" {
		return Course{}, validation("Некорректный режим импорта статей")
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
	kbSections, err := s.kb.GetSections(ctx, actor.Token)
	if err != nil {
		return Course{}, unavailable("Не удалось получить разделы базы знаний", err)
	}
	sectionNames := make(map[uuid.UUID]string, len(kbSections))
	for _, section := range kbSections {
		sectionNames[section.ID] = section.Name
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

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Course{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	now := s.now().UTC()
	courseRow, err := queries.CreateCourse(ctx, db.CreateCourseParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Title: title,
		Description: nullText(input.Description), Status: "draft",
		AuthorID: actor.UserID, Sequential: sequential,
		DeadlineDays: nullInt4(deadlineDays), CreatedAt: now,
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
	if err = tx.Commit(ctx); err != nil {
		return Course{}, internal("Не удалось сохранить курс", err)
	}
	return courseFromRow(courseRow), nil
}

type UpdateCourseInput struct {
	ID           uuid.UUID
	Title        *string
	Description  *string
	Status       *string
	Sequential   *bool
	DeadlineDays *int32
}

func (s *Service) UpdateCourse(ctx context.Context, actor Actor, input UpdateCourseInput) (Course, error) {
	if !actor.canManage() {
		return Course{}, forbidden("Недостаточно прав для изменения академии")
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
		if err := validateCourseStatus(*input.Status); err != nil {
			return Course{}, err
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
	row, err := db.New(s.pool).UpdateCourse(ctx, params)
	if err != nil {
		if isNoRows(err) {
			return Course{}, notFound("Курс")
		}
		return Course{}, internal("Не удалось обновить курс", err)
	}
	return courseFromRow(row), nil
}

func (s *Service) DeleteCourse(ctx context.Context, actor Actor, id uuid.UUID) error {
	if !actor.canManage() {
		return forbidden("Недостаточно прав для изменения академии")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	// Lessons, quizzes, assignments and progress are removed by FK cascades;
	// positions.required_course_ids is cleaned by company via this event (§10.1).
	deleted, err := queries.DeleteCourse(ctx, db.DeleteCourseParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		return internal("Не удалось удалить курс", err)
	}
	if deleted == 0 {
		return notFound("Курс")
	}
	if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.academy.course.deleted.v1", map[string]any{
		"courseId": id.String(),
	}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось удалить курс", err)
	}
	return nil
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
