package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) GetCourseSections(ctx context.Context, actor Actor, courseID uuid.UUID) ([]CourseSection, error) {
	queries := db.New(s.pool)
	if err := s.requireCourseAccess(ctx, queries, actor, courseID); err != nil {
		return nil, err
	}
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		return nil, internal("Не удалось получить курс", err)
	}
	version, err := s.displayCourseVersion(ctx, queries, actor, courseFromRow(courseRow))
	if err != nil {
		return nil, err
	}
	if version != nil {
		versionRows, versionErr := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{
			CompanyID: actor.CompanyID, CourseVersionID: version.ID,
		})
		if versionErr != nil {
			return nil, internal("Не удалось получить разделы версии курса", versionErr)
		}
		result := make([]CourseSection, len(versionRows))
		for index := range versionRows {
			result[index] = versionSectionAsLegacy(versionRows[index], courseID)
		}
		return result, nil
	}
	rows, err := queries.GetCourseSections(ctx, db.GetCourseSectionsParams{
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
	title, err := requiredText(input.Title, "Укажите название раздела")
	if err != nil {
		return CourseSection{}, err
	}
	currentCourse, err := s.requireCourseEditAccess(ctx, db.New(s.pool), actor, input.CourseID)
	if err != nil {
		return CourseSection{}, err
	}
	if currentCourse.CurrentDraftVersionID != nil {
		section, createErr := s.CreateCourseVersionSection(ctx, actor, *currentCourse.CurrentDraftVersionID, title)
		if createErr != nil {
			return CourseSection{}, createErr
		}
		return CourseSection{ID: section.ID, CompanyID: section.CompanyID, CourseID: input.CourseID, Title: section.Title, Order: section.Order}, nil
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseSection{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if _, err = s.requireCourseEditAccess(ctx, queries, actor, input.CourseID); err != nil {
		return CourseSection{}, err
	}
	if err = queries.LockCourseOrder(ctx, input.CourseID); err != nil {
		return CourseSection{}, internal("Не удалось заблокировать порядок курса", err)
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
	title, err := requiredText(input.Title, "Укажите название раздела")
	if err != nil {
		return CourseSection{}, err
	}
	queries := db.New(s.pool)
	if versionSection, versionErr := queries.GetCourseVersionSection(ctx, db.GetCourseVersionSectionParams{
		CompanyID: actor.CompanyID, ID: input.ID,
	}); versionErr == nil {
		updated, updateErr := s.UpdateCourseVersionSection(ctx, actor, UpdateCourseVersionSectionInput{ID: input.ID, Title: &title})
		if updateErr != nil {
			return CourseSection{}, updateErr
		}
		version, getErr := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: versionSection.CourseVersionID})
		if getErr != nil {
			return CourseSection{}, internal("Не удалось получить версию курса", getErr)
		}
		return CourseSection{ID: updated.ID, CompanyID: updated.CompanyID, CourseID: version.CourseID, Title: updated.Title, Order: updated.Order}, nil
	} else if !isNoRows(versionErr) {
		return CourseSection{}, internal("Не удалось проверить раздел версии", versionErr)
	}
	current, err := queries.GetCourseSection(ctx, db.GetCourseSectionParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		if isNoRows(err) {
			return CourseSection{}, notFound("Раздел курса")
		}
		return CourseSection{}, internal("Не удалось проверить раздел курса", err)
	}
	if _, err = s.requireCourseEditAccess(ctx, queries, actor, current.CourseID); err != nil {
		return CourseSection{}, err
	}
	row, err := queries.UpdateCourseSection(ctx, db.UpdateCourseSectionParams{
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
	rootQueries := db.New(s.pool)
	if _, versionErr := rootQueries.GetCourseVersionSection(ctx, db.GetCourseVersionSectionParams{
		CompanyID: actor.CompanyID, ID: id,
	}); versionErr == nil {
		return s.DeleteCourseVersionSection(ctx, actor, id)
	} else if !isNoRows(versionErr) {
		return internal("Не удалось проверить раздел версии", versionErr)
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
	if _, err = s.requireCourseEditAccess(ctx, queries, actor, section.CourseID); err != nil {
		return err
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
