package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) GetCourseSections(ctx context.Context, actor Actor, courseID uuid.UUID) ([]CourseSection, error) {
	rows, err := db.New(s.pool).GetCourseSections(ctx, db.GetCourseSectionsParams{
		CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		return nil, internal("Не удалось получить разделы курса", err)
	}
	result := make([]CourseSection, len(rows))
	for index := range rows {
		result[index] = sectionFromRow(rows[index])
	}
	return result, nil
}

type CreateCourseSectionInput struct {
	CourseID uuid.UUID
	Title    string
}

func (s *Service) CreateCourseSection(ctx context.Context, actor Actor, input CreateCourseSectionInput) (CourseSection, error) {
	if !actor.canManage() {
		return CourseSection{}, forbidden("Недостаточно прав для изменения академии")
	}
	title, err := requiredText(input.Title, "Укажите название раздела")
	if err != nil {
		return CourseSection{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseSection{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	if _, err = queries.GetCourse(ctx, db.GetCourseParams{
		CompanyID: actor.CompanyID, ID: input.CourseID,
	}); err != nil {
		if isNoRows(err) {
			return CourseSection{}, notFound("Курс")
		}
		return CourseSection{}, internal("Не удалось проверить курс", err)
	}
	siblings, err := queries.CountCourseSections(ctx, db.CountCourseSectionsParams{
		CompanyID: actor.CompanyID, CourseID: input.CourseID,
	})
	if err != nil {
		return CourseSection{}, internal("Не удалось получить разделы курса", err)
	}
	row, err := queries.CreateCourseSection(ctx, db.CreateCourseSectionParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: input.CourseID,
		Title: title, Order: int32(siblings),
	})
	if err != nil {
		return CourseSection{}, internal("Не удалось создать раздел курса", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseSection{}, internal("Не удалось сохранить раздел курса", err)
	}
	return sectionFromRow(row), nil
}

type UpdateCourseSectionInput struct {
	ID    uuid.UUID
	Title string
}

func (s *Service) UpdateCourseSection(ctx context.Context, actor Actor, input UpdateCourseSectionInput) (CourseSection, error) {
	if !actor.canManage() {
		return CourseSection{}, forbidden("Недостаточно прав для изменения академии")
	}
	title, err := requiredText(input.Title, "Укажите название раздела")
	if err != nil {
		return CourseSection{}, err
	}
	row, err := db.New(s.pool).UpdateCourseSection(ctx, db.UpdateCourseSectionParams{
		CompanyID: actor.CompanyID, ID: input.ID, Title: title,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseSection{}, notFound("Раздел курса")
		}
		return CourseSection{}, internal("Не удалось обновить раздел курса", err)
	}
	return sectionFromRow(row), nil
}

func (s *Service) DeleteCourseSection(ctx context.Context, actor Actor, id uuid.UUID) error {
	if !actor.canManage() {
		return forbidden("Недостаточно прав для изменения академии")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	section, err := queries.GetCourseSection(ctx, db.GetCourseSectionParams{
		CompanyID: actor.CompanyID, ID: id,
	})
	if err != nil {
		if isNoRows(err) {
			return notFound("Раздел курса")
		}
		return internal("Не удалось получить раздел курса", err)
	}
	// Lesson rows and quizzes cascade with the section; completed-lesson marks
	// in progress rows are cleaned explicitly (mirror of removeLessonCascade).
	lessonIDs, err := queries.GetSectionLessonIds(ctx, db.GetSectionLessonIdsParams{
		CompanyID: actor.CompanyID, SectionID: id,
	})
	if err != nil {
		return internal("Не удалось получить уроки раздела", err)
	}
	if len(lessonIDs) > 0 {
		if err = queries.RemoveLessonsFromProgress(ctx, db.RemoveLessonsFromProgressParams{
			CompanyID: actor.CompanyID, CourseID: section.CourseID, LessonIds: lessonIDs,
		}); err != nil {
			return internal("Не удалось обновить прогресс", err)
		}
	}
	if _, err = queries.DeleteCourseSection(ctx, db.DeleteCourseSectionParams{
		CompanyID: actor.CompanyID, ID: id,
	}); err != nil {
		return internal("Не удалось удалить раздел курса", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось удалить раздел курса", err)
	}
	return nil
}
