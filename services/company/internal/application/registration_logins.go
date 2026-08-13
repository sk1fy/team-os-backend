package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const registrationLoginReservationTTL = 30 * time.Minute

func (s *Service) ReserveRegistrationLogin(ctx context.Context) (RegistrationLoginReservation, error) {
	now := s.now().UTC()
	token, err := domainauth.NewOpaqueToken()
	if err != nil {
		return RegistrationLoginReservation{}, internal("Не удалось зарезервировать логин", err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return RegistrationLoginReservation{}, internal("Не удалось начать резервацию логина", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if _, err = queries.DeleteStaleRegistrationLoginReservations(ctx, db.DeleteStaleRegistrationLoginReservationsParams{
		Now: now, ConsumedBefore: pgTimestamp(now.Add(-registrationLoginReservationTTL)),
	}); err != nil {
		return RegistrationLoginReservation{}, internal("Не удалось очистить старые резервации логинов", err)
	}
	expiresAt := now.Add(registrationLoginReservationTTL)
	var row db.RegistrationLoginReservation
	for attempt := 0; attempt < 32; attempt++ {
		row, err = queries.CreateRegistrationLoginReservation(ctx, db.CreateRegistrationLoginReservationParams{
			ID: uuid.New(), TokenHash: domainauth.HashOpaqueToken(token), ExpiresAt: expiresAt, CreatedAt: now,
		})
		if err == nil {
			break
		}
		if !isNoRows(err) {
			return RegistrationLoginReservation{}, internal("Не удалось зарезервировать логин", err)
		}
	}
	if err != nil {
		return RegistrationLoginReservation{}, internal("Свободные логины временно недоступны", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return RegistrationLoginReservation{}, internal("Не удалось завершить резервацию логина", err)
	}
	return RegistrationLoginReservation{Login: row.Login, ReservationToken: token, ExpiresAt: expiresAt}, nil
}

func registrationLoginReservationError(row db.RegistrationLoginReservation, now time.Time) error {
	switch {
	case row.ConsumedAt.Valid:
		return loginReservationConsumed()
	case !row.ExpiresAt.After(now):
		return loginReservationExpired()
	default:
		return nil
	}
}

func normalizeLoginReservationToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 {
		return "", loginReservationInvalid()
	}
	return value, nil
}
