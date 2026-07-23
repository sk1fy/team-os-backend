package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

const (
	enrollmentOperationCompleteLesson = "complete_lesson"
	enrollmentOperationSubmitQuiz     = "submit_quiz"
)

func normalizeEnrollmentIdempotencyKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if len([]byte(key)) < 8 || len([]byte(key)) > 255 {
		return "", validation("Ключ идемпотентности должен содержать от 8 до 255 байт")
	}
	return key, nil
}

func enrollmentMutationRequestHash(value any) string {
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func (s *Service) reserveEnrollmentMutationInTx(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	enrollmentID uuid.UUID,
	operation, idempotencyKey, requestHash string,
	now time.Time,
) (db.EnrollmentMutationIdempotency, error) {
	reservation, err := queries.ReserveEnrollmentMutationIdempotency(ctx, db.ReserveEnrollmentMutationIdempotencyParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, EnrollmentID: enrollmentID,
		ActorUserID: actor.UserID, Operation: operation, IdempotencyKey: idempotencyKey,
		RequestHash: requestHash, CreatedAt: now,
	})
	if err != nil {
		return db.EnrollmentMutationIdempotency{}, internal("Не удалось зарезервировать операцию прохождения", err)
	}
	if reservation.EnrollmentID != enrollmentID || reservation.RequestHash != requestHash {
		return db.EnrollmentMutationIdempotency{}, conflict(
			"Ключ идемпотентности уже использован для другого запроса",
		)
	}
	return reservation, nil
}
