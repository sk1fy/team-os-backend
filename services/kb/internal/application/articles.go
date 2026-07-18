package application

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	domainaccess "github.com/sk1fy/team-os-backend/services/kb/internal/domain/access"
	"github.com/sk1fy/team-os-backend/services/kb/internal/storage/db"
	"google.golang.org/protobuf/types/known/structpb"
)

func (s *Service) loadSections(ctx context.Context, companyID uuid.UUID) ([]Section, map[uuid.UUID]Section, error) {
	rows, err := db.New(s.pool).ListSections(ctx, companyID)
	if err != nil {
		return nil, nil, internal("Не удалось получить разделы", err)
	}
	sections := make([]Section, 0, len(rows))
	for _, row := range rows {
		section, mapErr := sectionFromDB(row)
		if mapErr != nil {
			return nil, nil, mapErr
		}
		sections = append(sections, section)
	}
	return sections, sectionIndex(sections), nil
}

func (s *Service) canReadArticle(actor Actor, article Article, sections map[uuid.UUID]Section) bool {
	if domainaccess.CanManage(actor.subject()) {
		return true
	}
	if article.Status != "published" {
		return false
	}
	section, ok := sections[article.SectionID]
	if !ok {
		return false
	}
	if section.Visibility == "public" {
		return true
	}
	settings := domainaccess.EffectiveAccess(section.domain(sections), domainIndex(sections))
	return domainaccess.Allowed(actor.subject(), settings)
}

func (s *Service) GetPublicArticle(ctx context.Context, id uuid.UUID) (Article, error) {
	row, err := db.New(s.pool).GetPublicArticle(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return Article{}, notFound("Статья")
		}
		return Article{}, internal("Не удалось получить публичную статью", err)
	}
	return articleFromPublicRow(row), nil
}

func (s *Service) GetArticles(ctx context.Context, actor Actor, sectionID *uuid.UUID) ([]Article, error) {
	_, sections, err := s.loadSections(ctx, actor.CompanyID)
	if err != nil {
		return nil, err
	}
	params := db.ListArticlesParams{CompanyID: actor.CompanyID}
	if sectionID != nil {
		params.SectionID = uuid.NullUUID{UUID: *sectionID, Valid: true}
	}
	rows, err := db.New(s.pool).ListArticles(ctx, params)
	if err != nil {
		return nil, internal("Не удалось получить статьи", err)
	}
	articles := make([]Article, 0, len(rows))
	for _, row := range rows {
		article := articleFromListRow(row)
		if s.canReadArticle(actor, article, sections) {
			articles = append(articles, article)
		}
	}
	return articles, nil
}

func (s *Service) GetArticle(ctx context.Context, actor Actor, id uuid.UUID) (Article, error) {
	_, sections, err := s.loadSections(ctx, actor.CompanyID)
	if err != nil {
		return Article{}, err
	}
	row, err := db.New(s.pool).GetArticle(ctx, db.GetArticleParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return Article{}, notFound("Статья")
		}
		return Article{}, internal("Не удалось получить статью", err)
	}
	article := articleFromGetRow(row)
	if !s.canReadArticle(actor, article, sections) {
		return Article{}, forbidden("Недостаточно прав для просмотра статьи")
	}
	return article, nil
}

func (s *Service) GetArticlesByIDs(ctx context.Context, actor Actor, ids []uuid.UUID) ([]Article, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	_, sections, err := s.loadSections(ctx, actor.CompanyID)
	if err != nil {
		return nil, err
	}
	rows, err := db.New(s.pool).GetArticlesByIDs(ctx, db.GetArticlesByIDsParams{
		CompanyID: actor.CompanyID, Ids: ids,
	})
	if err != nil {
		return nil, internal("Не удалось получить статьи", err)
	}
	articles := make([]Article, 0, len(rows))
	for _, row := range rows {
		article := articleFromIDsRow(row)
		if s.canReadArticle(actor, article, sections) {
			articles = append(articles, article)
		}
	}
	return articles, nil
}

func (s *Service) ArticleExists(ctx context.Context, actor Actor, id uuid.UUID) (bool, error) {
	exists, err := db.New(s.pool).ArticleExists(ctx, db.ArticleExistsParams{
		CompanyID: actor.CompanyID, ID: id,
	})
	if err != nil {
		return false, internal("Не удалось проверить статью", err)
	}
	return exists, nil
}

type CreateArticleInput struct {
	SectionID               uuid.UUID
	Title                   string
	Content                 json.RawMessage
	Status                  string
	RequiresAcknowledgement bool
}

func (s *Service) CreateArticle(ctx context.Context, actor Actor, input CreateArticleInput) (Article, error) {
	if !domainaccess.CanManage(actor.subject()) {
		return Article{}, forbidden("Недостаточно прав для изменения базы знаний")
	}
	title, err := requiredText(input.Title, "Укажите заголовок статьи")
	if err != nil {
		return Article{}, err
	}
	if err = validateArticleStatus(input.Status); err != nil {
		return Article{}, err
	}
	normalizedContent, normalizeErr := richtext.Normalize(input.Content)
	if normalizeErr != nil {
		return Article{}, validation("Некорректное содержимое статьи")
	}
	input.Content = normalizedContent
	plainText := richtext.PlainText(input.Content)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Article{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	if _, err = queries.GetSection(ctx, db.GetSectionParams{
		CompanyID: actor.CompanyID, ID: input.SectionID,
	}); err != nil {
		if isNoRows(err) {
			return Article{}, notFound("Раздел")
		}
		return Article{}, internal("Не удалось проверить раздел", err)
	}

	articleID := uuid.New()
	row, err := queries.CreateArticle(ctx, db.CreateArticleParams{
		ID: articleID, CompanyID: actor.CompanyID, SectionID: input.SectionID,
		Title: title, Content: input.Content, Status: input.Status,
		AuthorID: actor.UserID, Version: 1,
		RequiresAcknowledgement: input.RequiresAcknowledgement, PlainText: plainText,
	})
	if err != nil {
		return Article{}, internal("Не удалось создать статью", err)
	}
	article := articleFromCreateRow(row)

	if input.Status == "published" {
		_, sections, loadErr := s.loadSectionsInTx(ctx, queries, actor.CompanyID)
		if loadErr != nil {
			return Article{}, loadErr
		}
		section := sections[article.SectionID]
		if err = s.emit(ctx, queries, actor.CompanyID, article.ID, actor.UserID, "teamos.kb.article.published.v1", &eventsv1.KbArticlePublishedPayload{
			ArticleId: article.ID.String(), SectionId: article.SectionID.String(),
			Title: article.Title, Version: uint32(article.Version),
			RequiresAcknowledgement: article.RequiresAcknowledgement,
			Audience:                audiencePayload(section, sections),
		}); err != nil {
			return Article{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Article{}, internal("Не удалось сохранить статью", err)
	}
	return article, nil
}

type UpdateArticleInput struct {
	ID                      uuid.UUID
	SectionID               *uuid.UUID
	Title                   *string
	Content                 json.RawMessage
	Status                  *string
	RequiresAcknowledgement *bool
	ExpectedVersion         *int32
}

func (s *Service) UpdateArticle(ctx context.Context, actor Actor, input UpdateArticleInput) (Article, error) {
	if !domainaccess.CanManage(actor.subject()) {
		return Article{}, forbidden("Недостаточно прав для изменения базы знаний")
	}
	if input.SectionID == nil && input.Title == nil && len(input.Content) == 0 &&
		input.Status == nil && input.RequiresAcknowledgement == nil {
		return Article{}, validation("Укажите хотя бы одно поле для обновления")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Article{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	current, err := queries.GetArticleForUpdate(ctx, db.GetArticleForUpdateParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		if isNoRows(err) {
			return Article{}, notFound("Статья")
		}
		return Article{}, internal("Не удалось получить статью", err)
	}
	if input.ExpectedVersion != nil && current.Version != *input.ExpectedVersion {
		return Article{}, conflict("Статья была изменена другим пользователем")
	}

	nextTitle := current.Title
	if input.Title != nil {
		nextTitle, err = requiredText(*input.Title, "Укажите заголовок статьи")
		if err != nil {
			return Article{}, err
		}
	}
	nextContent := append(json.RawMessage(nil), current.Content...)
	if len(input.Content) > 0 {
		normalizedContent, normalizeErr := richtext.Normalize(input.Content)
		if normalizeErr != nil {
			return Article{}, validation("Некорректное содержимое статьи")
		}
		nextContent = append(json.RawMessage(nil), normalizedContent...)
	}
	nextStatus := current.Status
	if input.Status != nil {
		if err = validateArticleStatus(*input.Status); err != nil {
			return Article{}, err
		}
		nextStatus = *input.Status
	}
	nextSectionID := current.SectionID
	if input.SectionID != nil {
		if _, err = queries.GetSection(ctx, db.GetSectionParams{
			CompanyID: actor.CompanyID, ID: *input.SectionID,
		}); err != nil {
			if isNoRows(err) {
				return Article{}, notFound("Раздел")
			}
			return Article{}, internal("Не удалось проверить раздел", err)
		}
		nextSectionID = *input.SectionID
	}
	nextRequiresAck := current.RequiresAcknowledgement
	if input.RequiresAcknowledgement != nil {
		nextRequiresAck = *input.RequiresAcknowledgement
	}

	contentChanged := nextTitle != current.Title || nextStatus != current.Status ||
		!jsonEqual(nextContent, current.Content)
	nextVersion := current.Version
	if contentChanged {
		_, err = queries.CreateArticleVersion(ctx, db.CreateArticleVersionParams{
			ID: uuid.New(), CompanyID: actor.CompanyID, ArticleID: current.ID,
			Version: current.Version, Title: current.Title, Content: current.Content,
			AuthorID: actor.UserID, CreatedAt: s.now().UTC(),
		})
		if err != nil {
			return Article{}, internal("Не удалось сохранить версию статьи", err)
		}
		nextVersion = current.Version + 1
	}

	params := db.UpdateArticleParams{
		CompanyID: actor.CompanyID, ID: input.ID,
	}
	if input.SectionID != nil {
		params.SectionID = uuid.NullUUID{UUID: nextSectionID, Valid: true}
	}
	if input.Title != nil {
		params.Title = pgtype.Text{String: nextTitle, Valid: true}
	}
	if len(input.Content) > 0 || contentChanged {
		params.Content = nextContent
		plainText := richtext.PlainText(nextContent)
		params.PlainText = pgtype.Text{String: plainText, Valid: true}
	}
	if input.Status != nil {
		params.Status = pgtype.Text{String: nextStatus, Valid: true}
	}
	if input.RequiresAcknowledgement != nil {
		params.RequiresAcknowledgement = pgtype.Bool{Bool: nextRequiresAck, Valid: true}
	}
	if contentChanged {
		params.Version = pgtype.Int4{Int32: nextVersion, Valid: true}
	}

	updated, err := queries.UpdateArticle(ctx, params)
	if err != nil {
		return Article{}, internal("Не удалось обновить статью", err)
	}
	article := articleFromUpdateRow(updated)

	_, sections, err := s.loadSectionsInTx(ctx, queries, actor.CompanyID)
	if err != nil {
		return Article{}, err
	}
	section := sections[article.SectionID]

	if current.Status != "published" && article.Status == "published" {
		if err = s.emit(ctx, queries, actor.CompanyID, article.ID, actor.UserID, "teamos.kb.article.published.v1", &eventsv1.KbArticlePublishedPayload{
			ArticleId: article.ID.String(), SectionId: article.SectionID.String(),
			Title: article.Title, Version: uint32(article.Version),
			RequiresAcknowledgement: article.RequiresAcknowledgement,
			Audience:                audiencePayload(section, sections),
		}); err != nil {
			return Article{}, err
		}
	}
	if contentChanged {
		var contentMap map[string]any
		if err = json.Unmarshal(article.Content, &contentMap); err != nil {
			return Article{}, internal("Не удалось сформировать содержимое события", err)
		}
		content, structErr := structpb.NewStruct(contentMap)
		if structErr != nil {
			return Article{}, internal("Не удалось сформировать содержимое события", structErr)
		}
		if err = s.emit(ctx, queries, actor.CompanyID, article.ID, actor.UserID, "teamos.kb.article.updated.v1", &eventsv1.KbArticleUpdatedPayload{
			ArticleId: article.ID.String(), Version: uint32(article.Version),
			Title: article.Title, Content: content,
		}); err != nil {
			return Article{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Article{}, internal("Не удалось сохранить статью", err)
	}
	return article, nil
}

type RollbackArticleInput struct {
	ArticleID       uuid.UUID
	VersionID       uuid.UUID
	ExpectedVersion *int32
}

func (s *Service) RollbackArticle(ctx context.Context, actor Actor, input RollbackArticleInput) (Article, error) {
	if !domainaccess.CanManage(actor.subject()) {
		return Article{}, forbidden("Недостаточно прав для изменения базы знаний")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Article{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	current, err := queries.GetArticleForUpdate(ctx, db.GetArticleForUpdateParams{CompanyID: actor.CompanyID, ID: input.ArticleID})
	if err != nil {
		if isNoRows(err) {
			return Article{}, notFound("Статья")
		}
		return Article{}, internal("Не удалось получить статью", err)
	}
	if input.ExpectedVersion != nil && current.Version != *input.ExpectedVersion {
		return Article{}, conflict("Статья была изменена другим пользователем")
	}
	versionRow, err := queries.GetArticleVersion(ctx, db.GetArticleVersionParams{
		CompanyID: actor.CompanyID, ID: input.VersionID,
	})
	if err != nil {
		if isNoRows(err) {
			return Article{}, notFound("Версия")
		}
		return Article{}, internal("Не удалось получить версию", err)
	}
	if versionRow.ArticleID != input.ArticleID {
		return Article{}, notFound("Версия")
	}

	_, err = queries.CreateArticleVersion(ctx, db.CreateArticleVersionParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, ArticleID: current.ID,
		Version: current.Version, Title: current.Title, Content: current.Content,
		AuthorID: actor.UserID, CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return Article{}, internal("Не удалось сохранить версию статьи", err)
	}
	nextVersion := current.Version + 1
	plainText := richtext.PlainText(versionRow.Content)
	updated, err := queries.UpdateArticle(ctx, db.UpdateArticleParams{
		CompanyID: actor.CompanyID, ID: current.ID,
		Title:     pgtype.Text{String: versionRow.Title, Valid: true},
		Content:   versionRow.Content,
		PlainText: pgtype.Text{String: plainText, Valid: true},
		Version:   pgtype.Int4{Int32: nextVersion, Valid: true},
	})
	if err != nil {
		return Article{}, internal("Не удалось откатить статью", err)
	}
	article := articleFromUpdateRow(updated)

	var contentMap map[string]any
	if err = json.Unmarshal(article.Content, &contentMap); err != nil {
		return Article{}, internal("Не удалось сформировать содержимое события", err)
	}
	content, structErr := structpb.NewStruct(contentMap)
	if structErr != nil {
		return Article{}, internal("Не удалось сформировать содержимое события", structErr)
	}
	if err = s.emit(ctx, queries, actor.CompanyID, article.ID, actor.UserID, "teamos.kb.article.updated.v1", &eventsv1.KbArticleUpdatedPayload{
		ArticleId: article.ID.String(), Version: uint32(article.Version),
		Title: article.Title, Content: content,
	}); err != nil {
		return Article{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Article{}, internal("Не удалось сохранить статью", err)
	}
	return article, nil
}

func (s *Service) GetArticleVersions(ctx context.Context, actor Actor, articleID uuid.UUID) ([]ArticleVersion, error) {
	_, sections, err := s.loadSections(ctx, actor.CompanyID)
	if err != nil {
		return nil, err
	}
	current, err := db.New(s.pool).GetArticle(ctx, db.GetArticleParams{CompanyID: actor.CompanyID, ID: articleID})
	if err != nil {
		if isNoRows(err) {
			return nil, notFound("Статья")
		}
		return nil, internal("Не удалось получить статью", err)
	}
	article := articleFromGetRow(current)
	if !s.canReadArticle(actor, article, sections) {
		return nil, forbidden("Недостаточно прав для просмотра статьи")
	}
	rows, err := db.New(s.pool).ListArticleVersions(ctx, db.ListArticleVersionsParams{
		CompanyID: actor.CompanyID, ArticleID: articleID,
	})
	if err != nil {
		return nil, internal("Не удалось получить версии статьи", err)
	}
	versions := make([]ArticleVersion, len(rows))
	for index, row := range rows {
		versions[index] = versionFromDB(row)
	}
	return versions, nil
}

func (s *Service) GetAcknowledgements(ctx context.Context, actor Actor, articleID uuid.UUID) ([]Acknowledgement, error) {
	_, sections, err := s.loadSections(ctx, actor.CompanyID)
	if err != nil {
		return nil, err
	}
	current, err := db.New(s.pool).GetArticle(ctx, db.GetArticleParams{CompanyID: actor.CompanyID, ID: articleID})
	if err != nil {
		if isNoRows(err) {
			return nil, notFound("Статья")
		}
		return nil, internal("Не удалось получить статью", err)
	}
	article := articleFromGetRow(current)
	if !domainaccess.CanManage(actor.subject()) && !s.canReadArticle(actor, article, sections) {
		return nil, forbidden("Недостаточно прав для просмотра ознакомлений")
	}
	rows, err := db.New(s.pool).ListAcknowledgements(ctx, db.ListAcknowledgementsParams{
		CompanyID: actor.CompanyID, ArticleID: articleID,
	})
	if err != nil {
		return nil, internal("Не удалось получить ознакомления", err)
	}
	acknowledgements := make([]Acknowledgement, len(rows))
	for index, row := range rows {
		acknowledgements[index] = acknowledgementFromDB(row)
	}
	return acknowledgements, nil
}

func (s *Service) AcknowledgeArticle(ctx context.Context, actor Actor, articleID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	current, err := queries.GetArticleForUpdate(ctx, db.GetArticleForUpdateParams{CompanyID: actor.CompanyID, ID: articleID})
	if err != nil {
		if isNoRows(err) {
			return notFound("Статья")
		}
		return internal("Не удалось получить статью", err)
	}
	article := articleFromForUpdateRow(current)
	_, sections, err := s.loadSectionsInTx(ctx, queries, actor.CompanyID)
	if err != nil {
		return err
	}
	if !s.canReadArticle(actor, article, sections) {
		return forbidden("Недостаточно прав для ознакомления со статьёй")
	}
	if article.Status != "published" {
		return validation("Ознакомиться можно только с опубликованной статьёй")
	}
	if !article.RequiresAcknowledgement {
		return validation("Статья не требует ознакомления")
	}
	if err = queries.UpsertAcknowledgement(ctx, db.UpsertAcknowledgementParams{
		CompanyID: actor.CompanyID, ArticleID: articleID,
		UserID: actor.UserID, AcknowledgedAt: s.now().UTC(),
	}); err != nil {
		return internal("Не удалось сохранить ознакомление", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось сохранить ознакомление", err)
	}
	return nil
}

func (s *Service) SearchArticles(ctx context.Context, actor Actor, query string) ([]Article, error) {
	normalized := strings.TrimSpace(query)
	if normalized == "" {
		return nil, validation("Укажите поисковый запрос")
	}
	_, sections, err := s.loadSections(ctx, actor.CompanyID)
	if err != nil {
		return nil, err
	}
	rows, err := db.New(s.pool).SearchArticles(ctx, db.SearchArticlesParams{
		CompanyID: actor.CompanyID, Query: normalized,
	})
	if err != nil {
		return nil, internal("Не удалось выполнить поиск", err)
	}
	articles := make([]Article, 0, len(rows))
	for _, row := range rows {
		article := articleFromSearchRow(row)
		if s.canReadArticle(actor, article, sections) {
			articles = append(articles, article)
		}
	}
	return articles, nil
}

func (s *Service) loadSectionsInTx(ctx context.Context, queries *db.Queries, companyID uuid.UUID) ([]Section, map[uuid.UUID]Section, error) {
	rows, err := queries.ListSections(ctx, companyID)
	if err != nil {
		return nil, nil, internal("Не удалось получить разделы", err)
	}
	sections := make([]Section, 0, len(rows))
	for _, row := range rows {
		section, mapErr := sectionFromDB(row)
		if mapErr != nil {
			return nil, nil, mapErr
		}
		sections = append(sections, section)
	}
	return sections, sectionIndex(sections), nil
}

func validateArticleStatus(status string) error {
	switch status {
	case "draft", "published":
		return nil
	default:
		return validation("Некорректный статус статьи")
	}
}

func jsonEqual(left, right []byte) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	var leftValue, rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	leftJSON, err := json.Marshal(leftValue)
	if err != nil {
		return false
	}
	rightJSON, err := json.Marshal(rightValue)
	if err != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}
