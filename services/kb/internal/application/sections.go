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
	if actor.Role == "partner" {
		rows, err := db.New(s.pool).ListPartnerVisibleSections(ctx, db.ListPartnerVisibleSectionsParams{
			CompanyID: actor.CompanyID, PartnerID: actor.UserID,
		})
		if err != nil {
			return nil, internal("Не удалось получить доступные разделы", err)
		}
		sections := make([]Section, 0, len(rows))
		for _, row := range rows {
			access, mapErr := accessFromJSON(row.Access)
			if mapErr != nil {
				return nil, mapErr
			}
			sections = append(sections, Section{
				ID: row.ID, CompanyID: row.CompanyID, Name: row.Name,
				ParentID: uuidPointer(row.ParentID), Order: row.Order, Access: access,
				Visibility: row.Visibility, PartnerAccess: PartnerAccessSettings{Mode: "selected"},
				CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			})
		}
		return sections, nil
	}
	sections, byID, err := s.loadSections(ctx, actor.CompanyID)
	if err != nil {
		return nil, err
	}
	return readableSections(actor, sections, byID), nil
}

func readableSections(actor Actor, sections []Section, byID map[uuid.UUID]Section) []Section {
	if domainaccess.CanManage(actor.subject()) {
		return sections
	}
	domainSections := domainIndex(byID)
	allowed := make(map[uuid.UUID]struct{}, len(sections))
	for _, section := range sections {
		effective := domainaccess.EffectiveAccess(section.domain(byID), domainSections)
		if domainaccess.Allowed(actor.subject(), effective) {
			allowed[section.ID] = struct{}{}
		}
	}

	result := make([]Section, 0, len(allowed))
	for _, section := range sections {
		if _, ok := allowed[section.ID]; !ok {
			continue
		}
		if section.ParentID != nil {
			if _, parentAllowed := allowed[*section.ParentID]; !parentAllowed {
				section.ParentID = nil
			}
		}
		result = append(result, section)
	}
	return result
}

type CreateSectionInput struct {
	Name       string
	ParentID   *uuid.UUID
	Access     *AccessSettings
	Visibility *string
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
	visibility := "company"
	if input.Visibility != nil {
		if err = validateSectionVisibility(*input.Visibility); err != nil {
			return Section{}, err
		}
		visibility = *input.Visibility
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
		Access:     accessJSON,
		Visibility: visibility,
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
	ID         uuid.UUID
	Name       *string
	Access     *AccessSettings
	Visibility *string
}

func (s *Service) UpdateSection(ctx context.Context, actor Actor, input UpdateSectionInput) (Section, error) {
	if !domainaccess.CanManage(actor.subject()) {
		return Section{}, forbidden("Недостаточно прав для изменения базы знаний")
	}
	if input.Name == nil && input.Access == nil && input.Visibility == nil {
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
	if input.Visibility != nil {
		if err := validateSectionVisibility(*input.Visibility); err != nil {
			return Section{}, err
		}
		params.Visibility = pgtype.Text{String: *input.Visibility, Valid: true}
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

func validateSectionVisibility(value string) error {
	if value != "public" && value != "company" {
		return validation("Некорректная видимость раздела")
	}
	return nil
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
