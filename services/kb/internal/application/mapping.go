package application

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	domainaccess "github.com/sk1fy/team-os-backend/services/kb/internal/domain/access"
	"github.com/sk1fy/team-os-backend/services/kb/internal/storage/db"
)

func sectionFromDB(value db.Section) (Section, error) {
	access, err := accessFromJSON(value.Access)
	if err != nil {
		return Section{}, err
	}
	return Section{
		ID: value.ID, CompanyID: value.CompanyID, Name: value.Name,
		ParentID: uuidPointer(value.ParentID), Order: value.Order, Access: access,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}, nil
}

type articleDBFields struct {
	ID                      uuid.UUID
	CompanyID               uuid.UUID
	SectionID               uuid.UUID
	Title                   string
	Content                 []byte
	Status                  string
	AuthorID                uuid.UUID
	Version                 int32
	RequiresAcknowledgement bool
	PlainText               string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func articleFromDBRow(value articleDBFields) Article {
	return Article{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: append(json.RawMessage(nil), value.Content...),
		Status: value.Status, AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText: value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func articleFromGetRow(value db.GetArticleRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText: value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	})
}

func articleFromCreateRow(value db.CreateArticleRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText: value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	})
}

func articleFromUpdateRow(value db.UpdateArticleRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText: value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	})
}

func articleFromListRow(value db.ListArticlesRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText: value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	})
}

func articleFromSearchRow(value db.SearchArticlesRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText: value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	})
}

func articleFromIDsRow(value db.GetArticlesByIDsRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText: value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	})
}

func versionFromDB(value db.ArticleVersion) ArticleVersion {
	return ArticleVersion{
		ID: value.ID, CompanyID: value.CompanyID, ArticleID: value.ArticleID,
		Version: value.Version, Title: value.Title,
		Content: append(json.RawMessage(nil), value.Content...),
		AuthorID: value.AuthorID, CreatedAt: value.CreatedAt,
	}
}

func acknowledgementFromDB(value db.ListAcknowledgementsRow) Acknowledgement {
	return Acknowledgement{
		ArticleID: value.ArticleID, UserID: value.UserID, AcknowledgedAt: value.AcknowledgedAt,
	}
}

func accessFromJSON(raw []byte) (AccessSettings, error) {
	var payload struct {
		Scope         string   `json:"scope"`
		DepartmentIDs []string `json:"departmentIds"`
		PositionIDs   []string `json:"positionIds"`
		UserIDs       []string `json:"userIds"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return AccessSettings{}, fmt.Errorf("разобрать access: %w", err)
	}
	scope := domainaccess.Scope(payload.Scope)
	if scope != domainaccess.ScopeCompany && scope != domainaccess.ScopeCustom {
		return AccessSettings{}, validation("Некорректные настройки доступа")
	}
	departmentIDs, err := parseUUIDList(payload.DepartmentIDs)
	if err != nil {
		return AccessSettings{}, validation("Некорректные departmentIds")
	}
	positionIDs, err := parseUUIDList(payload.PositionIDs)
	if err != nil {
		return AccessSettings{}, validation("Некорректные positionIds")
	}
	userIDs, err := parseUUIDList(payload.UserIDs)
	if err != nil {
		return AccessSettings{}, validation("Некорректные userIds")
	}
	return AccessSettings{
		Scope: scope, DepartmentIDs: departmentIDs,
		PositionIDs: positionIDs, UserIDs: userIDs,
	}, nil
}

func accessToJSON(settings AccessSettings) ([]byte, error) {
	return json.Marshal(struct {
		Scope         string   `json:"scope"`
		DepartmentIDs []string `json:"departmentIds"`
		PositionIDs   []string `json:"positionIds"`
		UserIDs       []string `json:"userIds"`
	}{
		Scope: string(settings.Scope),
		DepartmentIDs: uuidStrings(settings.DepartmentIDs),
		PositionIDs:   uuidStrings(settings.PositionIDs),
		UserIDs:       uuidStrings(settings.UserIDs),
	})
}

func defaultAccessSettings() AccessSettings {
	return AccessSettings{
		Scope: domainaccess.ScopeCompany,
	}
}

func parseUUIDList(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func uuidPointer(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := value.UUID
	return &result
}

func sectionIndex(sections []Section) map[uuid.UUID]Section {
	result := make(map[uuid.UUID]Section, len(sections))
	for _, section := range sections {
		result[section.ID] = section
	}
	return result
}

func audiencePayload(section Section, sections map[uuid.UUID]Section) map[string]any {
	effective := domainaccess.EffectiveAccess(section.domain(sections), domainIndex(sections))
	return map[string]any{
		"scope": audienceScopeValue(effective.Scope),
		"departmentIds": uuidStrings(effective.DepartmentIDs),
		"positionIds":   uuidStrings(effective.PositionIDs),
		"userIds":       uuidStrings(effective.UserIDs),
	}
}

func domainIndex(sections map[uuid.UUID]Section) map[uuid.UUID]domainaccess.Section {
	result := make(map[uuid.UUID]domainaccess.Section, len(sections))
	for id, section := range sections {
		result[id] = section.domain(sections)
	}
	return result
}

func audienceScopeValue(scope domainaccess.Scope) string {
	switch scope {
	case domainaccess.ScopeCustom:
		return "ARTICLE_AUDIENCE_SCOPE_CUSTOM"
	default:
		return "ARTICLE_AUDIENCE_SCOPE_COMPANY"
	}
}