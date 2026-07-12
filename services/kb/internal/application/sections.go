package application

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	domainaccess "github.com/sk1fy/team-os-backend/services/kb/internal/domain/access"
	"github.com/sk1fy/team-os-backend/services/kb/internal/storage/db"
)

func (s *Service) GetSections(ctx context.Context, actor Actor) ([]Section, error) {
	queries := db.New(s.pool)
	rows, err := queries.ListSections(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить разделы", err)
	}
	sections := make([]Section, 0, len(rows))
	for _, row := range rows {
		section, mapErr := sectionFromDB(row)
		if mapErr != nil {
			return nil, mapErr
		}
		sections = append(sections, section)
	}
	return sections, nil
}

type CreateSectionInput struct {
	Name     string
	ParentID *uuid.UUID
	Access   *AccessSettings
}

func (s *Service) CreateSection(ctx context.Context, actor Actor, input CreateSectionInput) (Section, error) {
	if !domainaccess.CanManage(actor.subject()) {
		return Section{}, forbidden("Недостаточно прав для изменения базы знаний")
	}
	name, err := requiredText(input.Name, "Укажите название раздела")
	if err != nil {
		return Section{}, err
	}
	access := defaultAccessSettings()
	if input.Access != nil {
		access = *input.Access
	}
	accessJSON, err := accessToJSON(access)
	if err != nil {
		return Section{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Section{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	if input.ParentID != nil {
		if _, err = queries.GetSection(ctx, db.GetSectionParams{
			CompanyID: actor.CompanyID, ID: *input.ParentID,
		}); err != nil {
			if isNoRows(err) {
				return Section{}, notFound("Раздел")
			}
			return Section{}, internal("Не удалось проверить родительский раздел", err)
		}
	}
	siblingCount, err := queries.CountSectionSiblings(ctx, db.CountSectionSiblingsParams{
		CompanyID: actor.CompanyID, ParentID: nullableUUID(input.ParentID),
	})
	if err != nil {
		return Section{}, internal("Не удалось определить порядок раздела", err)
	}

	row, err := queries.CreateSection(ctx, db.CreateSectionParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Name: name,
		ParentID: nullableUUID(input.ParentID), Order: siblingCount,
		Access: accessJSON,
	})
	if err != nil {
		return Section{}, internal("Не удалось создать раздел", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Section{}, internal("Не удалось сохранить раздел", err)
	}
	return sectionFromDB(row)
}

type UpdateSectionInput struct {
	ID     uuid.UUID
	Name   *string
	Access *AccessSettings
}

func (s *Service) UpdateSection(ctx context.Context, actor Actor, input UpdateSectionInput) (Section, error) {
	if !domainaccess.CanManage(actor.subject()) {
		return Section{}, forbidden("Недостаточно прав для изменения базы знаний")
	}
	if input.Name == nil && input.Access == nil {
		return Section{}, validation("Укажите хотя бы одно поле для обновления")
	}
	params := db.UpdateSectionParams{CompanyID: actor.CompanyID, ID: input.ID}
	if input.Name != nil {
		name, err := requiredText(*input.Name, "Укажите название раздела")
		if err != nil {
			return Section{}, err
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if input.Access != nil {
		accessJSON, err := accessToJSON(*input.Access)
		if err != nil {
			return Section{}, err
		}
		params.Access = accessJSON
	}
	row, err := db.New(s.pool).UpdateSection(ctx, params)
	if err != nil {
		if isNoRows(err) {
			return Section{}, notFound("Раздел")
		}
		return Section{}, internal("Не удалось обновить раздел", err)
	}
	return sectionFromDB(row)
}

func (s *Service) DeleteSection(ctx context.Context, actor Actor, id uuid.UUID) error {
	if !domainaccess.CanManage(actor.subject()) {
		return forbidden("Недостаточно прав для изменения базы знаний")
	}
	queries := db.New(s.pool)
	childCount, err := queries.CountChildSections(ctx, db.CountChildSectionsParams{
		CompanyID: actor.CompanyID, ParentID: uuid.NullUUID{UUID: id, Valid: true},
	})
	if err != nil {
		return internal("Не удалось проверить подразделы", err)
	}
	if childCount > 0 {
		return validation("Нельзя удалить раздел с вложенными подразделами.")
	}
	articleCount, err := queries.CountSectionArticles(ctx, db.CountSectionArticlesParams{
		CompanyID: actor.CompanyID, SectionID: id,
	})
	if err != nil {
		return internal("Не удалось проверить статьи раздела", err)
	}
	if articleCount > 0 {
		return validation("Нельзя удалить раздел со статьями.")
	}
	if err = queries.DeleteSection(ctx, db.DeleteSectionParams{CompanyID: actor.CompanyID, ID: id}); err != nil {
		return internal("Не удалось удалить раздел", err)
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

func nullableUUID(value *uuid.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *value, Valid: true}
}