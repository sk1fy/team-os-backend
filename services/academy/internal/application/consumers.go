package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/encoding/protojson"
)

// HandleKbArticleUpdated replicates fresh article content into link lessons
// (§10.2). The processed_events check and the update share one transaction.
func (s *Service) HandleKbArticleUpdated(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload eventsv1.KbArticleUpdatedPayload
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode kb.article.updated payload: %w", err)
	}
	articleID, err := uuid.Parse(payload.GetArticleId())
	if err != nil {
		return false, fmt.Errorf("kb.article.updated: invalid articleId %q", payload.GetArticleId())
	}
	if payload.GetContent() == nil {
		return false, fmt.Errorf("kb.article.updated %s: content is empty", payload.GetArticleId())
	}
	content, err := json.Marshal(payload.GetContent().AsMap())
	if err != nil || richtext.Validate(content) != nil {
		return false, fmt.Errorf("kb.article.updated %s: content is not valid TipTap JSON", payload.GetArticleId())
	}
	return s.handleIdempotent(ctx, event, func(queries *db.Queries, companyID uuid.UUID) error {
		_, updateErr := queries.ReplicateLinkedArticle(ctx, db.ReplicateLinkedArticleParams{
			CompanyID: companyID, Content: content,
			NewTitle: payload.GetTitle(), ArticleID: nullUUIDValue(articleID),
		})
		return updateErr
	})
}

// HandleKbArticleDeleted converts link lessons of a removed article to copies,
// keeping the last replicated content (§10.1).
func (s *Service) HandleKbArticleDeleted(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload eventsv1.KbArticleDeletedPayload
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode kb.article.deleted payload: %w", err)
	}
	articleID, err := uuid.Parse(payload.GetArticleId())
	if err != nil {
		return false, fmt.Errorf("kb.article.deleted: invalid articleId %q", payload.GetArticleId())
	}
	return s.handleIdempotent(ctx, event, func(queries *db.Queries, companyID uuid.UUID) error {
		_, updateErr := queries.DetachLinkedArticle(ctx, db.DetachLinkedArticleParams{
			CompanyID: companyID, ArticleID: nullUUIDValue(articleID),
		})
		return updateErr
	})
}

func (s *Service) HandleOrgUserCreated(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload eventsv1.OrgUserCreatedPayload
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode org.user.created payload: %w", err)
	}
	return s.handleOrgUserSnapshot(ctx, event, payload.GetUser())
}

func (s *Service) HandleOrgUserUpdated(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload eventsv1.OrgUserUpdatedPayload
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode org.user.updated payload: %w", err)
	}
	return s.handleOrgUserSnapshot(ctx, event, payload.GetUser())
}

func (s *Service) handleOrgUserSnapshot(
	ctx context.Context,
	event eventbus.Event,
	user *eventsv1.OrgUserSnapshot,
) (bool, error) {
	if user == nil {
		return false, errors.New("org user payload has no user snapshot")
	}
	userID, err := uuid.Parse(user.GetUserId())
	if err != nil {
		return false, fmt.Errorf("org user event: invalid userId %q", user.GetUserId())
	}
	positionIDs, err := parseEventUUIDs(user.GetPositionIds(), "positionId")
	if err != nil {
		return false, err
	}
	departmentIDs, err := parseEventUUIDs(user.GetDepartmentIds(), "departmentId")
	if err != nil {
		return false, err
	}
	active := user.GetStatus() == eventsv1.OrgUserStatus_ORG_USER_STATUS_ACTIVE
	return s.handleIdempotent(ctx, event, func(queries *db.Queries, companyID uuid.UUID) error {
		return queries.RecomputeUserAssignmentMembership(ctx, db.RecomputeUserAssignmentMembershipParams{
			UserID: userID, Active: active, PositionIds: positionIDs,
			DepartmentIds: departmentIDs, CompanyID: companyID,
		})
	})
}

func (s *Service) HandleOrgUserDeactivated(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload eventsv1.OrgUserDeactivatedPayload
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode org.user.deactivated payload: %w", err)
	}
	userID, err := uuid.Parse(payload.GetUserId())
	if err != nil {
		return false, fmt.Errorf("org.user.deactivated: invalid userId %q", payload.GetUserId())
	}
	return s.handleIdempotent(ctx, event, func(queries *db.Queries, companyID uuid.UUID) error {
		return queries.RecomputeUserAssignmentMembership(ctx, db.RecomputeUserAssignmentMembershipParams{
			UserID: userID, Active: false, PositionIds: []uuid.UUID{},
			DepartmentIds: []uuid.UUID{}, CompanyID: companyID,
		})
	})
}

func (s *Service) HandleOrgPositionDeleted(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload eventsv1.OrgPositionDeletedPayload
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode org.position.deleted payload: %w", err)
	}
	positionID, err := uuid.Parse(payload.GetPositionId())
	if err != nil {
		return false, fmt.Errorf("org.position.deleted: invalid positionId %q", payload.GetPositionId())
	}
	affectedUserIDs, err := parseEventUUIDs(payload.GetAffectedUserIds(), "affectedUserId")
	if err != nil {
		return false, err
	}
	return s.handleIdempotent(ctx, event, func(queries *db.Queries, companyID uuid.UUID) error {
		if err := queries.ClearDeletedPositionAssignment(ctx, db.ClearDeletedPositionAssignmentParams{
			CompanyID: companyID, AssigneeID: uuid.NullUUID{UUID: positionID, Valid: true},
		}); err != nil {
			return err
		}
		for _, userID := range affectedUserIDs {
			if err := queries.RecomputeUserAssignmentMembership(ctx, db.RecomputeUserAssignmentMembershipParams{
				UserID: userID, Active: true, PositionIds: []uuid.UUID{},
				DepartmentIds: []uuid.UUID{}, CompanyID: companyID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func parseEventUUIDs(values []string, field string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("org event: invalid %s %q", field, value)
		}
		result = append(result, parsed)
	}
	return result, nil
}

func (s *Service) handleIdempotent(
	ctx context.Context,
	event eventbus.Event,
	apply func(*db.Queries, uuid.UUID) error,
) (bool, error) {
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
	if err = apply(queries, companyID); err != nil {
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
