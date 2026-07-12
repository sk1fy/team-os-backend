package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

type kbArticleUpdatedPayload struct {
	ArticleID string          `json:"articleId"`
	Version   int32           `json:"version"`
	Title     string          `json:"title"`
	Content   json.RawMessage `json:"content"`
}

// HandleKbArticleUpdated replicates fresh article content into link lessons
// (§10.2). The processed_events check and the update share one transaction.
func (s *Service) HandleKbArticleUpdated(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload kbArticleUpdatedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode kb.article.updated payload: %w", err)
	}
	articleID, err := uuid.Parse(payload.ArticleID)
	if err != nil {
		return false, fmt.Errorf("kb.article.updated: invalid articleId %q", payload.ArticleID)
	}
	if len(payload.Content) == 0 || !json.Valid(payload.Content) {
		return false, fmt.Errorf("kb.article.updated %s: content is not valid JSON", payload.ArticleID)
	}
	return s.handleIdempotent(ctx, event, func(queries *db.Queries) error {
		_, updateErr := queries.ReplicateLinkedArticle(ctx, db.ReplicateLinkedArticleParams{
			Content: payload.Content, NewTitle: payload.Title, ArticleID: nullUUIDValue(articleID),
		})
		return updateErr
	})
}

type kbArticleDeletedPayload struct {
	ArticleID string `json:"articleId"`
}

// HandleKbArticleDeleted converts link lessons of a removed article to copies,
// keeping the last replicated content (§10.1).
func (s *Service) HandleKbArticleDeleted(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload kbArticleDeletedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode kb.article.deleted payload: %w", err)
	}
	articleID, err := uuid.Parse(payload.ArticleID)
	if err != nil {
		return false, fmt.Errorf("kb.article.deleted: invalid articleId %q", payload.ArticleID)
	}
	return s.handleIdempotent(ctx, event, func(queries *db.Queries) error {
		_, updateErr := queries.DetachLinkedArticle(ctx, nullUUIDValue(articleID))
		return updateErr
	})
}

func (s *Service) handleIdempotent(ctx context.Context, event eventbus.Event, apply func(*db.Queries) error) (bool, error) {
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return false, fmt.Errorf("invalid eventId %q", event.EventID)
	}
	companyID, err := uuid.Parse(event.CompanyID)
	if err != nil {
		return false, fmt.Errorf("invalid companyId %q", event.CompanyID)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin consumer transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	inserted, err := queries.MarkEventProcessed(ctx, db.MarkEventProcessedParams{
		EventID: eventID, CompanyID: companyID,
	})
	if err != nil {
		return false, fmt.Errorf("mark event processed: %w", err)
	}
	if inserted == 0 {
		return false, nil
	}
	if err = apply(queries); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit consumer transaction: %w", err)
	}
	return true, nil
}

func nullUUIDValue(value uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: value, Valid: true}
}
