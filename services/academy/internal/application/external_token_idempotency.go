package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

const (
	externalTokenOperationCreatePersonalAccess = "create_personal_access"
	externalTokenOperationRotatePersonalAccess = "rotate_personal_access_token"
	externalTokenOperationRepeatPersonalAccess = "repeat_personal_access"
	externalTokenOperationCreateCampaign       = "create_external_campaign"
	externalTokenOperationRotateCampaign       = "rotate_external_campaign_token"
)

func (s *Service) idempotentExternalToken(
	actor Actor,
	operation string,
	idempotencyKey string,
) (token string, hash []byte, prefix string, err error) {
	if len(s.externalSecret) < 32 {
		return "", nil, "", errors.New("секрет внешних токенов не настроен")
	}
	digest := hmac.New(sha256.New, s.externalSecret)
	_, _ = digest.Write([]byte(
		"academy-external-token\x00" +
			actor.CompanyID.String() + "\x00" +
			actor.UserID.String() + "\x00" +
			operation + "\x00" +
			idempotencyKey,
	))
	token = base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
	hash = s.externalTokenHash(token)
	prefix = token[:min(externalTokenPrefixLength, len(token))]
	return token, hash, prefix, nil
}

func (s *Service) reserveExternalTokenMutationInTx(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	operation string,
	idempotencyKey string,
	request any,
	resultID uuid.UUID,
	now time.Time,
) (db.ExternalTokenMutationIdempotency, bool, error) {
	key, err := normalizeEnrollmentIdempotencyKey(idempotencyKey)
	if err != nil {
		return db.ExternalTokenMutationIdempotency{}, false, err
	}
	reservationID := uuid.New()
	reservation, err := queries.ReserveExternalTokenMutationIdempotency(
		ctx,
		db.ReserveExternalTokenMutationIdempotencyParams{
			ID: reservationID, CompanyID: actor.CompanyID, ActorUserID: actor.UserID,
			Operation: operation, IdempotencyKey: key,
			RequestHash: enrollmentMutationRequestHash(request),
			ResultID:    resultID, CreatedAt: now,
		},
	)
	if err != nil {
		return db.ExternalTokenMutationIdempotency{}, false,
			internal("Не удалось зарезервировать операцию с внешней ссылкой", err)
	}
	if reservation.RequestHash != enrollmentMutationRequestHash(request) {
		return db.ExternalTokenMutationIdempotency{}, false,
			conflict("Ключ идемпотентности уже использован для другого запроса")
	}
	replayed := reservation.ID != reservationID
	if replayed && (!reservation.CompletedAt.Valid || len(reservation.ResponsePayload) == 0) {
		return db.ExternalTokenMutationIdempotency{}, false,
			internal("Сохранённый результат операции с внешней ссылкой не завершён", nil)
	}
	return reservation, replayed, nil
}

func completeExternalTokenMutationInTx(
	ctx context.Context,
	queries *db.Queries,
	companyID uuid.UUID,
	reservationID uuid.UUID,
	response any,
	completedAt time.Time,
) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return internal("Не удалось сохранить результат операции с внешней ссылкой", err)
	}
	_, err = queries.CompleteExternalTokenMutationIdempotency(
		ctx,
		db.CompleteExternalTokenMutationIdempotencyParams{
			ResponsePayload: payload, CompletedAt: nullTimestamptz(&completedAt),
			CompanyID: companyID, ID: reservationID,
		},
	)
	if err != nil {
		return internal("Не удалось завершить операцию с внешней ссылкой", err)
	}
	return nil
}

func decodeExternalTokenMutationReplay[T any](payload []byte) (T, error) {
	var result T
	if len(payload) == 0 {
		return result, internal("Сохранённый ответ операции с внешней ссылкой пуст", nil)
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return result, internal("Не удалось прочитать сохранённый ответ операции с внешней ссылкой", err)
	}
	return result, nil
}

func normalizeExternalTokenKey(value string) (string, error) {
	return normalizeEnrollmentIdempotencyKey(strings.TrimSpace(value))
}
