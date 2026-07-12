// Package clients contains thin gRPC adapters for the synchronous calls listed
// in §9. The actor's bearer token is forwarded as-is; kb and company enforce
// authorization themselves.
package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

type Kb struct {
	client  kbv1.KbServiceClient
	breaker *circuitBreaker
}

func NewKb(client kbv1.KbServiceClient) *Kb {
	return &Kb{client: client, breaker: newCircuitBreaker()}
}

var _ application.KbClient = (*Kb)(nil)

func (k *Kb) GetArticle(ctx context.Context, token string, id uuid.UUID) (application.KbArticle, error) {
	response, err := callWithResilience(ctx, token, k.breaker, func(callContext context.Context) (*kbv1.GetArticleResponse, error) {
		return k.client.GetArticle(callContext, &kbv1.GetArticleRequest{Id: id.String()})
	})
	if err != nil {
		return application.KbArticle{}, fmt.Errorf("kb.GetArticle: %w", err)
	}
	return articleFromProto(response.GetArticle())
}

func (k *Kb) GetArticlesByIds(ctx context.Context, token string, ids []uuid.UUID) ([]application.KbArticle, error) {
	request := &kbv1.GetArticlesByIdsRequest{Ids: make([]string, len(ids))}
	for index := range ids {
		request.Ids[index] = ids[index].String()
	}
	response, err := callWithResilience(ctx, token, k.breaker, func(callContext context.Context) (*kbv1.GetArticlesByIdsResponse, error) {
		return k.client.GetArticlesByIds(callContext, request)
	})
	if err != nil {
		return nil, fmt.Errorf("kb.GetArticlesByIds: %w", err)
	}
	articles := make([]application.KbArticle, 0, len(response.GetArticles()))
	for _, article := range response.GetArticles() {
		converted, convertErr := articleFromProto(article)
		if convertErr != nil {
			return nil, convertErr
		}
		articles = append(articles, converted)
	}
	return articles, nil
}

func (k *Kb) GetSections(ctx context.Context, token string) ([]application.KbSection, error) {
	response, err := callWithResilience(ctx, token, k.breaker, func(callContext context.Context) (*kbv1.GetSectionsResponse, error) {
		return k.client.GetSections(callContext, &kbv1.GetSectionsRequest{})
	})
	if err != nil {
		return nil, fmt.Errorf("kb.GetSections: %w", err)
	}
	sections := make([]application.KbSection, 0, len(response.GetSections()))
	for _, section := range response.GetSections() {
		id, parseErr := uuid.Parse(section.GetId())
		if parseErr != nil {
			return nil, fmt.Errorf("kb.GetSections: invalid section id %q", section.GetId())
		}
		sections = append(sections, application.KbSection{ID: id, Name: section.GetName()})
	}
	return sections, nil
}

func articleFromProto(article *kbv1.Article) (application.KbArticle, error) {
	if article == nil {
		return application.KbArticle{}, errors.New("kb returned an empty article")
	}
	id, err := uuid.Parse(article.GetId())
	if err != nil {
		return application.KbArticle{}, fmt.Errorf("invalid article id %q", article.GetId())
	}
	sectionID, err := uuid.Parse(article.GetSectionId())
	if err != nil {
		return application.KbArticle{}, fmt.Errorf("invalid article sectionId %q", article.GetSectionId())
	}
	content, err := json.Marshal(article.GetContent().AsMap())
	if err != nil {
		return application.KbArticle{}, fmt.Errorf("encode article content: %w", err)
	}
	return application.KbArticle{
		ID: id, SectionID: sectionID, Title: article.GetTitle(), Content: content,
	}, nil
}
