package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/storage/db"
)

var (
	ErrEmailDeliveryBusy             = errors.New("доставка email уже выполняется")
	ErrEmailDeliveryIdentityConflict = errors.New("данные доставки email не согласованы")
)

type ClaimEmailDeliveryInput struct {
	ID, EventID, CompanyID, ChallengeID uuid.UUID
	Purpose                             string
	RecipientFingerprint                []byte
	ExpiresAt, Now, StaleBefore         time.Time
}

type ClaimEmailDeliveryResult struct {
	ShouldSend bool
	Terminal   bool
	Attempts   int32
}

type EmailDeliveryRepository struct {
	queries *db.Queries
}

func NewEmailDeliveryRepository(queries *db.Queries) (*EmailDeliveryRepository, error) {
	if queries == nil {
		return nil, fmt.Errorf("queries обязательны")
	}
	return &EmailDeliveryRepository{queries: queries}, nil
}

func (r *EmailDeliveryRepository) Claim(
	ctx context.Context,
	input ClaimEmailDeliveryInput,
) (ClaimEmailDeliveryResult, error) {
	if err := r.queries.InsertEmailDelivery(ctx, db.InsertEmailDeliveryParams{
		ID: input.ID, EventID: input.EventID, CompanyID: input.CompanyID,
		ChallengeID: input.ChallengeID, Purpose: input.Purpose,
		RecipientFingerprint: input.RecipientFingerprint, ExpiresAt: input.ExpiresAt,
	}); err != nil {
		return ClaimEmailDeliveryResult{}, err
	}
	if err := r.queries.ExpireEmailDelivery(ctx, db.ExpireEmailDeliveryParams{
		NowAt: input.Now, CompanyID: input.CompanyID, ChallengeID: input.ChallengeID,
	}); err != nil {
		return ClaimEmailDeliveryResult{}, err
	}

	claimed, err := r.queries.ClaimEmailDelivery(ctx, db.ClaimEmailDeliveryParams{
		NowAt:     pgtype.Timestamptz{Time: input.Now, Valid: true},
		CompanyID: input.CompanyID, ChallengeID: input.ChallengeID,
		StaleBefore: pgtype.Timestamptz{Time: input.StaleBefore, Valid: true},
	})
	if err == nil {
		if !bytes.Equal(claimed.RecipientFingerprint, input.RecipientFingerprint) {
			return ClaimEmailDeliveryResult{}, ErrEmailDeliveryIdentityConflict
		}
		return ClaimEmailDeliveryResult{ShouldSend: true, Attempts: claimed.Attempts}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ClaimEmailDeliveryResult{}, err
	}

	current, err := r.queries.GetEmailDelivery(ctx, db.GetEmailDeliveryParams{
		CompanyID: input.CompanyID, ChallengeID: input.ChallengeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ClaimEmailDeliveryResult{}, ErrEmailDeliveryIdentityConflict
		}
		return ClaimEmailDeliveryResult{}, err
	}
	if !bytes.Equal(current.RecipientFingerprint, input.RecipientFingerprint) {
		return ClaimEmailDeliveryResult{}, ErrEmailDeliveryIdentityConflict
	}
	switch current.Status {
	case "sent", "expired":
		return ClaimEmailDeliveryResult{Terminal: true, Attempts: current.Attempts}, nil
	case "sending":
		return ClaimEmailDeliveryResult{Attempts: current.Attempts}, ErrEmailDeliveryBusy
	case "failed":
		if current.Attempts >= current.MaxAttempts {
			return ClaimEmailDeliveryResult{Terminal: true, Attempts: current.Attempts}, nil
		}
	}
	return ClaimEmailDeliveryResult{Attempts: current.Attempts}, nil
}

func (r *EmailDeliveryRepository) MarkSent(
	ctx context.Context,
	companyID, challengeID uuid.UUID,
	sentAt time.Time,
) error {
	return r.queries.MarkEmailDeliverySent(ctx, db.MarkEmailDeliverySentParams{
		SentAt:    pgtype.Timestamptz{Time: sentAt, Valid: true},
		CompanyID: companyID, ChallengeID: challengeID,
	})
}

func (r *EmailDeliveryRepository) MarkFailed(
	ctx context.Context,
	companyID, challengeID uuid.UUID,
	errorCode string,
	failedAt time.Time,
) error {
	return r.queries.MarkEmailDeliveryFailed(ctx, db.MarkEmailDeliveryFailedParams{
		ErrorCode: &errorCode, FailedAt: failedAt, CompanyID: companyID, ChallengeID: challengeID,
	})
}
