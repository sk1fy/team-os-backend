package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
)

var fixtureNamespace = uuid.MustParse("7c29d63e-2954-5e23-9cba-7b950e8cc1a8")

type Summary struct {
	CompanyID        string
	Sections         int
	Articles         int
	Versions         int
	Acknowledgements int
}

type Fixtures struct {
	CompanyID        string
	Sections         []SectionFixture
	Articles         []ArticleFixture
	ArticleVersions  []ArticleVersionFixture
	Acknowledgements []AcknowledgementFixture
}

type SectionFixture struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	ParentID *string         `json:"parentId"`
	Order    int32           `json:"order"`
	Access   json.RawMessage `json:"access"`
}

type ArticleFixture struct {
	ID                      string          `json:"id"`
	SectionID               string          `json:"sectionId"`
	Title                   string          `json:"title"`
	Content                 json.RawMessage `json:"content"`
	Status                  string          `json:"status"`
	AuthorID                string          `json:"authorId"`
	Version                 int32           `json:"version"`
	RequiresAcknowledgement bool            `json:"requiresAcknowledgement"`
	CreatedAt               string          `json:"createdAt"`
	UpdatedAt               string          `json:"updatedAt"`
}

type ArticleVersionFixture struct {
	ID        string          `json:"id"`
	ArticleID string          `json:"articleId"`
	Version   int32           `json:"version"`
	Title     string          `json:"title"`
	Content   json.RawMessage `json:"content"`
	AuthorID  string          `json:"authorId"`
	CreatedAt string          `json:"createdAt"`
}

type AcknowledgementFixture struct {
	ArticleID      string `json:"articleId"`
	UserID         string `json:"userId"`
	AcknowledgedAt string `json:"acknowledgedAt"`
}

func Run(ctx context.Context, pool *pgxpool.Pool, directory string) (Summary, error) {
	if pool == nil {
		return Summary{}, errors.New("соединение с PostgreSQL не задано")
	}
	fixtures, err := Load(directory)
	if err != nil {
		return Summary{}, err
	}
	dataset, err := Normalize(fixtures)
	if err != nil {
		return Summary{}, fmt.Errorf("проверить фикстуры: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Summary{}, fmt.Errorf("начать seed-транзакцию: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := Apply(ctx, tx, dataset); err != nil {
		return Summary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("зафиксировать seed-транзакцию: %w", err)
	}
	return Summary{
		CompanyID: dataset.CompanyID.String(),
		Sections:  len(dataset.Sections), Articles: len(dataset.Articles),
		Versions: len(dataset.Versions), Acknowledgements: len(dataset.Acknowledgements),
	}, nil
}

func Load(directory string) (Fixtures, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return Fixtures{}, errors.New("директория фикстур не задана")
	}
	var fixtures Fixtures
	if raw, err := os.ReadFile(filepath.Join(directory, "company.json")); err == nil {
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Fixtures{}, fmt.Errorf("company.json: %w", err)
		}
		fixtures.CompanyID = payload.ID
	}
	if fixtures.CompanyID == "" {
		if err := readWrapped(directory, []string{"fixtures.json", "seed.json", "manifest.json"}, func(key string, raw json.RawMessage) error {
			switch key {
			case "company":
				var payload struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(raw, &payload); err != nil {
					return err
				}
				fixtures.CompanyID = payload.ID
			case "articleSections", "sections":
				return json.Unmarshal(raw, &fixtures.Sections)
			case "articles":
				return json.Unmarshal(raw, &fixtures.Articles)
			case "articleVersions":
				return json.Unmarshal(raw, &fixtures.ArticleVersions)
			case "acknowledgements":
				return json.Unmarshal(raw, &fixtures.Acknowledgements)
			}
			return nil
		}); err != nil {
			return Fixtures{}, err
		}
	}
	for _, name := range []struct {
		file string
		keys []string
		load func([]byte) error
	}{
		{"article-sections.json", []string{"articleSections", "sections"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Sections) }},
		{"article_sections.json", []string{"articleSections", "sections"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Sections) }},
		{"articles.json", []string{"articles"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Articles) }},
		{"article-versions.json", []string{"articleVersions"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.ArticleVersions) }},
		{"article_versions.json", []string{"articleVersions"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.ArticleVersions) }},
		{"acknowledgements.json", []string{"acknowledgements"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Acknowledgements) }},
	} {
		_ = readEntityFile(filepath.Join(directory, name.file), name.keys, name.load)
	}
	if fixtures.CompanyID == "" {
		return Fixtures{}, errors.New("company.id не найден в фикстурах")
	}
	if len(fixtures.Sections) == 0 {
		return Fixtures{}, errors.New("articleSections не найдены в фикстурах")
	}
	if len(fixtures.Articles) == 0 {
		return Fixtures{}, errors.New("articles не найдены в фикстурах")
	}
	return fixtures, nil
}

func readEntityFile(path string, keys []string, load func([]byte) error) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var direct any
	if err := json.Unmarshal(raw, &direct); err != nil {
		return err
	}
	switch value := direct.(type) {
	case []any:
		return load(raw)
	case map[string]any:
		for _, key := range keys {
			if nested, ok := value[key]; ok {
				encoded, encodeErr := json.Marshal(nested)
				if encodeErr != nil {
					return encodeErr
				}
				return load(encoded)
			}
		}
	}
	return load(raw)
}

func readWrapped(directory string, names []string, handle func(string, json.RawMessage) error) error {
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			continue
		}
		var manifest map[string]json.RawMessage
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return fmt.Errorf("разобрать %s: %w", name, err)
		}
		for key, value := range manifest {
			if err := handle(key, value); err != nil {
				return fmt.Errorf("%s.%s: %w", name, key, err)
			}
		}
		return nil
	}
	return errors.New("manifest fixtures not found")
}

type Dataset struct {
	CompanyID        uuid.UUID
	Sections         []sectionRow
	Articles         []articleRow
	Versions         []versionRow
	Acknowledgements []ackRow
}

type sectionRow struct {
	ID, CompanyID uuid.UUID
	Name          string
	ParentID      *uuid.UUID
	Order         int32
	Access        []byte
}

type articleRow struct {
	ID, CompanyID, SectionID, AuthorID uuid.UUID
	Title, Status, PlainText           string
	Content                            []byte
	Version                            int32
	RequiresAcknowledgement            bool
	CreatedAt, UpdatedAt               time.Time
}

type versionRow struct {
	ID, CompanyID, ArticleID, AuthorID uuid.UUID
	Version                            int32
	Title                              string
	Content                            []byte
	CreatedAt                          time.Time
}

type ackRow struct {
	CompanyID, ArticleID, UserID uuid.UUID
	AcknowledgedAt               time.Time
}

func Normalize(fixtures Fixtures) (Dataset, error) {
	companyID, err := MapID(fixtures.CompanyID)
	if err != nil {
		return Dataset{}, fmt.Errorf("company.id: %w", err)
	}
	dataset := Dataset{CompanyID: companyID}
	for _, fixture := range fixtures.Sections {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("section %s: %w", fixture.ID, mapErr)
		}
		var parentID *uuid.UUID
		if fixture.ParentID != nil {
			parsed, parseErr := MapID(*fixture.ParentID)
			if parseErr != nil {
				return Dataset{}, fmt.Errorf("section %s parentId: %w", fixture.ID, parseErr)
			}
			parentID = &parsed
		}
		access := fixture.Access
		if len(access) == 0 {
			access = []byte(`{"scope":"company","departmentIds":[],"positionIds":[],"userIds":[]}`)
		}
		dataset.Sections = append(dataset.Sections, sectionRow{
			ID: id, CompanyID: companyID, Name: fixture.Name,
			ParentID: parentID, Order: fixture.Order, Access: access,
		})
	}
	for _, fixture := range fixtures.Articles {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("article %s: %w", fixture.ID, mapErr)
		}
		sectionID, mapErr := MapID(fixture.SectionID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("article %s sectionId: %w", fixture.ID, mapErr)
		}
		authorID, mapErr := MapID(fixture.AuthorID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("article %s authorId: %w", fixture.ID, mapErr)
		}
		createdAt, mapErr := parseTimestamp(fixture.CreatedAt)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("article %s createdAt: %w", fixture.ID, mapErr)
		}
		updatedAt, mapErr := parseTimestamp(fixture.UpdatedAt)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("article %s updatedAt: %w", fixture.ID, mapErr)
		}
		dataset.Articles = append(dataset.Articles, articleRow{
			ID: id, CompanyID: companyID, SectionID: sectionID, AuthorID: authorID,
			Title: fixture.Title, Content: fixture.Content, Status: fixture.Status,
			Version: fixture.Version, RequiresAcknowledgement: fixture.RequiresAcknowledgement,
			PlainText: richtext.PlainText(fixture.Content),
			CreatedAt: createdAt, UpdatedAt: updatedAt,
		})
	}
	for _, fixture := range fixtures.ArticleVersions {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("articleVersion %s: %w", fixture.ID, mapErr)
		}
		articleID, mapErr := MapID(fixture.ArticleID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("articleVersion %s articleId: %w", fixture.ID, mapErr)
		}
		authorID, mapErr := MapID(fixture.AuthorID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("articleVersion %s authorId: %w", fixture.ID, mapErr)
		}
		createdAt, mapErr := parseTimestamp(fixture.CreatedAt)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("articleVersion %s createdAt: %w", fixture.ID, mapErr)
		}
		dataset.Versions = append(dataset.Versions, versionRow{
			ID: id, CompanyID: companyID, ArticleID: articleID, AuthorID: authorID,
			Version: fixture.Version, Title: fixture.Title, Content: fixture.Content,
			CreatedAt: createdAt,
		})
	}
	for _, fixture := range fixtures.Acknowledgements {
		articleID, mapErr := MapID(fixture.ArticleID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("acknowledgement articleId: %w", mapErr)
		}
		userID, mapErr := MapID(fixture.UserID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("acknowledgement userId: %w", mapErr)
		}
		acknowledgedAt, mapErr := parseTimestamp(fixture.AcknowledgedAt)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("acknowledgement acknowledgedAt: %w", mapErr)
		}
		dataset.Acknowledgements = append(dataset.Acknowledgements, ackRow{
			CompanyID: companyID, ArticleID: articleID, UserID: userID,
			AcknowledgedAt: acknowledgedAt,
		})
	}
	return dataset, nil
}

func MapID(value string) (uuid.UUID, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return uuid.Nil, errors.New("пустой ID")
	}
	if parsed, err := uuid.Parse(normalized); err == nil {
		return parsed, nil
	}
	return uuid.NewSHA1(fixtureNamespace, []byte(normalized)), nil
}

func parseTimestamp(value string) (time.Time, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return time.Time{}, errors.New("пустая дата")
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("неподдерживаемый формат даты %q", value)
}

func Apply(ctx context.Context, tx pgx.Tx, dataset Dataset) error {
	if _, err := tx.Exec(ctx, `DELETE FROM acknowledgements WHERE company_id = $1`, dataset.CompanyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM article_versions WHERE company_id = $1`, dataset.CompanyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM articles WHERE company_id = $1`, dataset.CompanyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sections WHERE company_id = $1`, dataset.CompanyID); err != nil {
		return err
	}
	for _, section := range dataset.Sections {
		if _, err := tx.Exec(ctx, `
			INSERT INTO sections (id, company_id, name, parent_id, "order", access, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, now(), now())`,
			section.ID, section.CompanyID, section.Name, section.ParentID, section.Order, section.Access,
		); err != nil {
			return fmt.Errorf("вставить section %s: %w", section.ID, err)
		}
	}
	for _, article := range dataset.Articles {
		if _, err := tx.Exec(ctx, `
			INSERT INTO articles (
				id, company_id, section_id, title, content, status, author_id, version,
				requires_acknowledgement, plain_text, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			article.ID, article.CompanyID, article.SectionID, article.Title, article.Content,
			article.Status, article.AuthorID, article.Version, article.RequiresAcknowledgement,
			article.PlainText, article.CreatedAt, article.UpdatedAt,
		); err != nil {
			return fmt.Errorf("вставить article %s: %w", article.ID, err)
		}
	}
	for _, version := range dataset.Versions {
		if _, err := tx.Exec(ctx, `
			INSERT INTO article_versions (id, company_id, article_id, version, title, content, author_id, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			version.ID, version.CompanyID, version.ArticleID, version.Version,
			version.Title, version.Content, version.AuthorID, version.CreatedAt,
		); err != nil {
			return fmt.Errorf("вставить article_version %s: %w", version.ID, err)
		}
	}
	for _, acknowledgement := range dataset.Acknowledgements {
		if _, err := tx.Exec(ctx, `
			INSERT INTO acknowledgements (company_id, article_id, user_id, acknowledged_at)
			VALUES ($1,$2,$3,$4)`,
			acknowledgement.CompanyID, acknowledgement.ArticleID,
			acknowledgement.UserID, acknowledgement.AcknowledgedAt,
		); err != nil {
			return fmt.Errorf("вставить acknowledgement: %w", err)
		}
	}
	return nil
}
