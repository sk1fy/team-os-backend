package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	domainauth "github.com/sk1fy/team-os-backend/services/academy/internal/domain/authorization"
	domaincourse "github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	domainlearner "github.com/sk1fy/team-os-backend/services/academy/internal/domain/externallearner"
	domainverification "github.com/sk1fy/team-os-backend/services/academy/internal/domain/externalverification"
	domainaccess "github.com/sk1fy/team-os-backend/services/academy/internal/domain/personalaccess"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const externalSessionTTL = 30 * 24 * time.Hour

type CreateExternalPersonalAccessInput struct {
	CourseID        uuid.UUID
	CourseVersionID uuid.UUID
	Email           string
	FirstName       *string
	LastName        *string
	DeadlineDays    int32
}

func (s *Service) CreateExternalPersonalAccess(
	ctx context.Context,
	actor Actor,
	input CreateExternalPersonalAccessInput,
) (ExternalPersonalAccessCreated, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: input.CourseID})
	if err != nil {
		if isNoRows(err) {
			return ExternalPersonalAccessCreated{}, notFound("Курс")
		}
		return ExternalPersonalAccessCreated{}, internal("Не удалось проверить курс", err)
	}
	versionRow, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: input.CourseVersionID})
	if err != nil || versionRow.CourseID != input.CourseID {
		if isNoRows(err) || err == nil {
			return ExternalPersonalAccessCreated{}, notFound("Версия курса")
		}
		return ExternalPersonalAccessCreated{}, internal("Не удалось проверить версию курса", err)
	}
	content, err := s.loadCourseVersionContent(ctx, queries, versionRow)
	if err != nil {
		return ExternalPersonalAccessCreated{}, err
	}
	domainVersion, err := domainVersionFromContent(content)
	if err != nil || !domainauth.CanCreatePersonalAccess(authorizationActor(actor), authorizationCourse(courseFromRow(courseRow)), domainVersion.Snapshot()) {
		return ExternalPersonalAccessCreated{}, forbidden("Создать персональный доступ можно только к опубликованной версии собственного партнёрского курса")
	}
	token, tokenHash, prefix, err := s.generateExternalToken()
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось создать токен доступа", err)
	}
	now, accessID := s.now().UTC(), uuid.New()
	aggregate, err := domainaccess.New(domainaccess.NewParams{
		ID: domainaccess.ID(accessID.String()), CompanyID: domainaccess.ID(actor.CompanyID.String()),
		CourseID: domainaccess.ID(input.CourseID.String()), CourseVersionID: domainaccess.ID(input.CourseVersionID.String()),
		PartnerOwnerID: domainaccess.ID(actor.UserID.String()), ExpectedEmail: input.Email,
		RecipientFirstName: input.FirstName, RecipientLastName: input.LastName, DeadlineDays: int(input.DeadlineDays),
		TokenHash: tokenHash, TokenPrefix: prefix, IssuanceIdempotencyKey: uuid.NewString(),
		IssuedByID: domainaccess.ID(actor.UserID.String()), IssuedAt: now,
	})
	if err != nil {
		return ExternalPersonalAccessCreated{}, validation(err.Error())
	}
	createdRow, err := queries.CreateExternalPersonalAccess(ctx, personalAccessCreateParams(aggregate.Snapshot()))
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось создать персональный доступ", err)
	}
	if err = s.recordPersonalAccessHistory(ctx, queries, actor.CompanyID, createdRow.ID, createdRow.ExternalLearnerID,
		createdRow.EnrollmentID, "created", "internal_user", &actor.UserID, nil, nil, &createdRow.TokenPrefix, now); err != nil {
		return ExternalPersonalAccessCreated{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось сохранить персональный доступ", err)
	}
	return ExternalPersonalAccessCreated{Access: personalAccessFromDB(createdRow), Token: token}, nil
}

func (s *Service) GetExternalPersonalAccesses(ctx context.Context, actor Actor, courseID uuid.UUID) ([]ExternalPersonalAccess, error) {
	if actor.Role != "partner" && !actor.canManage() {
		return nil, forbidden("Недостаточно прав для просмотра персональных доступов")
	}
	partnerID := (*uuid.UUID)(nil)
	if actor.Role == "partner" {
		partnerID = &actor.UserID
	}
	rows, err := db.New(s.pool).ListExternalPersonalAccesses(ctx, db.ListExternalPersonalAccessesParams{
		CompanyID: actor.CompanyID, PartnerOwnerID: nullUUID(partnerID), CourseID: nullUUID(&courseID),
	})
	if err != nil {
		return nil, internal("Не удалось получить персональные доступы", err)
	}
	result := make([]ExternalPersonalAccess, len(rows))
	for index, row := range rows {
		result[index] = personalAccessFromListRow(row)
	}
	return result, nil
}

func (s *Service) GetExternalPersonalAccess(ctx context.Context, actor Actor, accessID uuid.UUID) (ExternalPersonalAccess, error) {
	row, err := db.New(s.pool).GetExternalPersonalAccess(ctx, db.GetExternalPersonalAccessParams{CompanyID: actor.CompanyID, ID: accessID})
	if err != nil {
		if isNoRows(err) {
			return ExternalPersonalAccess{}, notFound("Персональный доступ")
		}
		return ExternalPersonalAccess{}, internal("Не удалось получить персональный доступ", err)
	}
	if !actor.canManage() && (actor.Role != "partner" || row.PartnerOwnerID != actor.UserID) {
		return ExternalPersonalAccess{}, notFound("Персональный доступ")
	}
	return personalAccessFromGetRow(row), nil
}

func (s *Service) RotateExternalPersonalAccessToken(ctx context.Context, actor Actor, accessID uuid.UUID) (ExternalPersonalAccessCreated, error) {
	token, tokenHash, prefix, err := s.generateExternalToken()
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось создать новый токен", err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	current, aggregate, err := s.requireManagedPersonalAccess(ctx, queries, actor, accessID)
	if err != nil {
		return ExternalPersonalAccessCreated{}, err
	}
	now := s.now().UTC()
	if err = aggregate.RotateToken(tokenHash, prefix, now); err != nil {
		return ExternalPersonalAccessCreated{}, conflict(err.Error())
	}
	row, err := queries.RotateExternalPersonalAccessToken(ctx, db.RotateExternalPersonalAccessTokenParams{
		TokenHash: tokenHash, TokenPrefix: prefix, RotatedAt: nullTimestamptz(&now),
		CompanyID: actor.CompanyID, ID: accessID, PartnerOwnerID: actor.UserID,
	})
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось заменить токен", err)
	}
	if err = s.recordPersonalAccessHistory(ctx, queries, actor.CompanyID, accessID, row.ExternalLearnerID, row.EnrollmentID,
		"token_rotated", "internal_user", &actor.UserID, nil, &current.TokenPrefix, &row.TokenPrefix, now); err != nil {
		return ExternalPersonalAccessCreated{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось сохранить новый токен", err)
	}
	return ExternalPersonalAccessCreated{Access: personalAccessFromRotateRow(row), Token: token}, nil
}

func (s *Service) ExtendExternalPersonalAccess(ctx context.Context, actor Actor, accessID uuid.UUID, days int32) (ExternalPersonalAccess, error) {
	if days < 1 || days > 7 {
		return ExternalPersonalAccess{}, validation("Срок продления должен быть от одного до семи дней")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalPersonalAccess{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	current, aggregate, err := s.requireManagedPersonalAccess(ctx, queries, actor, accessID)
	if err != nil {
		return ExternalPersonalAccess{}, err
	}
	now := s.now().UTC()
	if err = aggregate.SetDeadlineDays(int(days), now); err != nil {
		return ExternalPersonalAccess{}, conflict(err.Error())
	}
	var before *time.Time
	if current.EnrollmentID.Valid {
		enrollment, getErr := queries.GetEnrollmentForUpdate(ctx, db.GetEnrollmentForUpdateParams{CompanyID: actor.CompanyID, ID: current.EnrollmentID.UUID})
		if getErr == nil {
			before = timestamptzPointer(enrollment.AccessUntil)
		}
	}
	extended, err := queries.ExtendExternalPersonalAccess(ctx, db.ExtendExternalPersonalAccessParams{
		ExtensionDays: days, ExtendedAt: now, CompanyID: actor.CompanyID,
		PersonalAccessID: accessID, PartnerOwnerID: actor.UserID,
	})
	if err != nil {
		if isNoRows(err) {
			return ExternalPersonalAccess{}, conflict("Персональный доступ нельзя продлить в текущем состоянии")
		}
		return ExternalPersonalAccess{}, internal("Не удалось продлить персональный доступ", err)
	}
	after := timestamptzPointer(extended.AccessUntil)
	if err = s.recordPersonalAccessHistory(ctx, queries, actor.CompanyID, accessID, current.ExternalLearnerID, current.EnrollmentID,
		"extended", "internal_user", &actor.UserID, nil, nil, nil, now, before, after); err != nil {
		return ExternalPersonalAccess{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalPersonalAccess{}, internal("Не удалось сохранить продление", err)
	}
	return s.GetExternalPersonalAccess(ctx, actor, accessID)
}

func (s *Service) RevokeExternalPersonalAccess(ctx context.Context, actor Actor, accessID uuid.UUID) (ExternalPersonalAccess, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalPersonalAccess{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	_, aggregate, err := s.requireManagedPersonalAccess(ctx, queries, actor, accessID)
	if err != nil {
		return ExternalPersonalAccess{}, err
	}
	now := s.now().UTC()
	if err = aggregate.Revoke(now); err != nil {
		return ExternalPersonalAccess{}, conflict(err.Error())
	}
	row, err := queries.RevokeExternalPersonalAccess(ctx, db.RevokeExternalPersonalAccessParams{
		RevokedAt: nullTimestamptz(&now), CompanyID: actor.CompanyID, ID: accessID, PartnerOwnerID: actor.UserID,
	})
	if err != nil {
		return ExternalPersonalAccess{}, internal("Не удалось отозвать персональный доступ", err)
	}
	if err = s.recordPersonalAccessHistory(ctx, queries, actor.CompanyID, accessID, row.ExternalLearnerID, row.EnrollmentID,
		"revoked", "internal_user", &actor.UserID, nil, nil, nil, now); err != nil {
		return ExternalPersonalAccess{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalPersonalAccess{}, internal("Не удалось сохранить отзыв доступа", err)
	}
	return personalAccessFromRevokeRow(row), nil
}

func (s *Service) RepeatExternalPersonalAccess(ctx context.Context, actor Actor, accessID uuid.UUID) (ExternalPersonalAccessCreated, error) {
	token, tokenHash, prefix, err := s.generateExternalToken()
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось создать токен повторного прохождения", err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	current, aggregate, err := s.requireManagedPersonalAccess(ctx, queries, actor, accessID)
	if err != nil {
		return ExternalPersonalAccessCreated{}, err
	}
	completed := false
	if current.EnrollmentID.Valid {
		enrollment, getErr := queries.GetEnrollmentForUpdate(ctx, db.GetEnrollmentForUpdateParams{CompanyID: actor.CompanyID, ID: current.EnrollmentID.UUID})
		if getErr != nil {
			return ExternalPersonalAccessCreated{}, internal("Не удалось проверить предыдущее прохождение", getErr)
		}
		completed = enrollment.ProgressStatus == "completed"
	}
	now, newID := s.now().UTC(), uuid.New()
	repeat, err := aggregate.PlanRepeat(domainaccess.RepeatParams{
		ID: domainaccess.ID(newID.String()), TokenHash: tokenHash, TokenPrefix: prefix,
		IssuanceIdempotencyKey: uuid.NewString(), IssuedAt: now, PreviousCompleted: completed,
	})
	if err != nil {
		return ExternalPersonalAccessCreated{}, conflict(err.Error())
	}
	row, err := queries.CreateExternalPersonalAccess(ctx, personalAccessCreateParams(repeat.Snapshot()))
	if err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось создать повторный доступ", err)
	}
	if err = s.recordPersonalAccessHistory(ctx, queries, actor.CompanyID, row.ID, row.ExternalLearnerID, row.EnrollmentID,
		"repeat_created", "internal_user", &actor.UserID, nil, nil, &row.TokenPrefix, now); err != nil {
		return ExternalPersonalAccessCreated{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalPersonalAccessCreated{}, internal("Не удалось сохранить повторный доступ", err)
	}
	return ExternalPersonalAccessCreated{Access: personalAccessFromDB(row), Token: token}, nil
}

func (s *Service) GetPublicAcademyAccess(
	ctx context.Context,
	token string,
	principal *ExternalPrincipal,
	analytics CampaignAnalyticsContext,
) (PublicAcademyAccess, error) {
	row, err := db.New(s.pool).ResolveExternalPersonalAccessByTokenHash(ctx, db.ResolveExternalPersonalAccessByTokenHashParams{
		Now: nullTimestamptzPointer(s.now().UTC()), TokenHash: s.externalTokenHash(token),
	})
	if err != nil {
		if isNoRows(err) {
			return s.getPublicCampaignAccess(ctx, token, principal, analytics)
		}
		return PublicAcademyAccess{}, internal("Не удалось получить внешний доступ", err)
	}
	available := row.LifecycleStatus == "active" && row.DistributionStatus != "blocked" && row.CourseVersionStatus == "published"
	var reason *string
	if !available {
		value := "Курс сейчас недоступен"
		reason = &value
	}
	outlineRows, err := db.New(s.pool).ListExternalAccessLandingOutline(ctx, db.ListExternalAccessLandingOutlineParams{
		CompanyID: row.CompanyID, PersonalAccessID: row.ID,
	})
	if err != nil {
		return PublicAcademyAccess{}, internal("Не удалось получить структуру курса", err)
	}
	outline := publicOutlineFromRows(outlineRows)
	verified := false
	if principal != nil && principal.CompanyID == row.CompanyID {
		learner, learnerErr := db.New(s.pool).GetExternalLearner(ctx, db.GetExternalLearnerParams{
			CompanyID: principal.CompanyID, ID: principal.LearnerID,
		})
		verified = learnerErr == nil && learner.NormalizedEmail == row.NormalizedExpectedEmail
	}
	return PublicAcademyAccess{
		Kind: "personal", CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		Title: row.CourseVersionTitle, Description: textPointer(row.CourseVersionDescription), OwnerType: "partner",
		OwnerUserID: &row.PartnerOwnerID, DeadlineDays: int32(row.DeadlineDays), Available: available,
		UnavailableReason: reason, EmailVerificationRequired: !verified, Outline: outline,
	}, nil
}

func (s *Service) RequestPublicAcademyVerification(
	ctx context.Context,
	input RequestExternalVerificationInput,
) (ExternalVerificationChallenge, error) {
	normalized := domainlearner.NormalizeEmail(input.Email)
	if !domainlearner.ValidEmail(normalized) {
		return ExternalVerificationChallenge{}, validation("Некорректный email")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	access, err := queries.ResolveExternalPersonalAccessByTokenHash(ctx, db.ResolveExternalPersonalAccessByTokenHashParams{
		Now: nullTimestamptzPointer(s.now().UTC()), TokenHash: s.externalTokenHash(input.AccessToken),
	})
	if isNoRows(err) {
		return s.requestPublicCampaignVerification(ctx, tx, queries, input, normalized)
	}
	if err != nil || normalized != access.NormalizedExpectedEmail {
		return ExternalVerificationChallenge{}, notFound("Внешний доступ")
	}
	now := s.now().UTC()
	if err = s.checkExternalVerificationRate(ctx, queries, access.CompanyID, access.ID, normalized,
		string(domainverification.PurposePersonalAccess), input.IPHash, now); err != nil {
		return ExternalVerificationChallenge{}, err
	}
	challengeID := uuid.New()
	code, codeHash, err := s.generateVerificationCode(challengeID.String(), normalized)
	if err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось создать код подтверждения", err)
	}
	sourceID := domainverification.ID(access.ID.String())
	aggregate, err := domainverification.New(domainverification.NewParams{
		ID: domainverification.ID(challengeID.String()), CompanyID: domainverification.ID(access.CompanyID.String()),
		NormalizedEmail: normalized, Purpose: domainverification.PurposePersonalAccess, SourceID: &sourceID,
		ClaimedFirstName: input.FirstName, ClaimedLastName: input.LastName,
		CodeHash: codeHash, RequestIPHash: input.IPHash, CreatedAt: now,
	})
	if err != nil {
		return ExternalVerificationChallenge{}, validation(err.Error())
	}
	snapshot := aggregate.Snapshot()
	row, err := queries.CreateExternalVerificationChallenge(ctx, db.CreateExternalVerificationChallengeParams{
		ID: challengeID, CompanyID: access.CompanyID, NormalizedEmail: normalized, Purpose: string(snapshot.Purpose),
		SourceID: nullUUID(&access.ID), ClaimedFirstName: nullText(snapshot.ClaimedFirstName),
		ClaimedLastName: nullText(snapshot.ClaimedLastName), CodeHash: snapshot.CodeHash,
		RequestIpHash: snapshot.RequestIPHash, ExpiresAt: snapshot.ExpiresAt,
		MaxAttempts: int16(snapshot.MaxAttempts), CreatedAt: now,
	})
	if err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось сохранить код подтверждения", err)
	}
	_, err = queries.InvalidateOpenExternalChallenges(ctx, db.InvalidateOpenExternalChallengesParams{
		InvalidatedAt: nullTimestamptz(&now), InvalidationReason: pgtype.Text{String: "replaced", Valid: true},
		CompanyID: access.CompanyID, NormalizedEmail: normalized, Purpose: string(snapshot.Purpose),
		SourceID: nullUUID(&access.ID), ExceptID: challengeID,
	})
	if err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось заменить предыдущий код", err)
	}
	encrypted, err := s.encryptExternalVerificationDelivery(challengeID.String(), strings.TrimSpace(input.Email), code)
	if err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось защитить письмо подтверждения", err)
	}
	keyID := s.externalEmailKeyID
	payload := &eventsv1.AcademyExternalEmailVerificationRequestedPayload{
		ChallengeId: challengeID.String(), EncryptedDeliveryPayload: encrypted,
		ExpiresAt: timestamppb.New(row.ExpiresAt), Purpose: row.Purpose, KeyId: &keyID,
	}
	if err = s.emit(ctx, queries, access.CompanyID, challengeID, uuid.Nil,
		"teamos.academy.external_email_verification.requested.v1", payload); err != nil {
		return ExternalVerificationChallenge{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось сохранить подтверждение email", err)
	}
	return ExternalVerificationChallenge{ID: challengeID, ExpiresAt: row.ExpiresAt}, nil
}

func (s *Service) ConfirmPublicAcademyVerification(ctx context.Context, challengeID uuid.UUID, code string) (ExternalVerificationConfirmed, error) {
	if !domainverification.ValidSixDigitCode(code) {
		return ExternalVerificationConfirmed{}, validation("Неверный код подтверждения")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalVerificationConfirmed{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.ResolveExternalVerificationChallengeForUpdate(ctx, challengeID)
	if err != nil {
		return ExternalVerificationConfirmed{}, validation("Код подтверждения недействителен")
	}
	aggregate, err := rehydrateVerification(row)
	if err != nil {
		return ExternalVerificationConfirmed{}, internal("Некорректное состояние подтверждения", err)
	}
	now := s.now().UTC()
	candidateHash := s.verificationCodeHash(challengeID.String(), row.NormalizedEmail, code)
	if err = aggregate.Confirm(candidateHash, now); err != nil {
		if errors.Is(err, domainverification.ErrCodeMismatch) || errors.Is(err, domainverification.ErrChallengeAttemptsUsed) {
			_, _ = queries.RecordExternalVerificationFailure(ctx, db.RecordExternalVerificationFailureParams{
				AttemptedAt: nullTimestamptz(&now), CompanyID: row.CompanyID, ID: challengeID,
			})
			_ = tx.Commit(ctx)
		}
		return ExternalVerificationConfirmed{}, validation("Код подтверждения недействителен")
	}
	if _, err = queries.ConsumeExternalVerificationChallenge(ctx, db.ConsumeExternalVerificationChallengeParams{
		ConsumedAt: nullTimestamptz(&now), CompanyID: row.CompanyID, ID: challengeID,
	}); err != nil {
		return ExternalVerificationConfirmed{}, validation("Код подтверждения недействителен")
	}
	learnerRow, err := queries.UpsertExternalLearner(ctx, db.UpsertExternalLearnerParams{
		ID: uuid.New(), CompanyID: row.CompanyID, Email: row.NormalizedEmail, NormalizedEmail: row.NormalizedEmail,
		FirstName: row.ClaimedFirstName, LastName: row.ClaimedLastName,
		EmailVerifiedAt: nullTimestamptz(&now), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return ExternalVerificationConfirmed{}, internal("Не удалось сохранить внешнего ученика", err)
	}
	if row.Purpose == string(domainverification.PurposePersonalAccess) && row.SourceID.Valid {
		if _, err = queries.BindExternalPersonalAccessLearner(ctx, db.BindExternalPersonalAccessLearnerParams{
			ExternalLearnerID: nullUUID(&learnerRow.ID), UpdatedAt: now, CompanyID: row.CompanyID,
			ID: row.SourceID.UUID, NormalizedEmail: row.NormalizedEmail,
		}); err != nil {
			return ExternalVerificationConfirmed{}, validation("Email не соответствует внешнему доступу")
		}
	}
	if row.Purpose == string(domainverification.PurposeCampaignAccess) && row.SourceID.Valid {
		campaign, campaignErr := queries.GetExternalCampaignForUpdate(ctx, db.GetExternalCampaignForUpdateParams{
			CompanyID: row.CompanyID, ID: row.SourceID.UUID,
		})
		if campaignErr != nil || campaign.Status != "active" {
			return ExternalVerificationConfirmed{}, conflict("Кампания больше недоступна")
		}
		if err = s.recordCampaignAnalyticsEvent(ctx, queries, row.CompanyID, campaignAnalyticsEvent{
			CampaignID: campaign.ID, ExternalLearnerID: &learnerRow.ID, Type: "email_verified",
			IdempotencyKey: "email-verified:" + challengeID.String(), OccurredAt: now,
		}); err != nil {
			return ExternalVerificationConfirmed{}, err
		}
	}
	sessionToken, sessionHash, _, err := s.generateExternalToken()
	if err != nil {
		return ExternalVerificationConfirmed{}, internal("Не удалось создать внешнюю сессию", err)
	}
	expiresAt := now.Add(externalSessionTTL)
	if _, err = queries.CreateExternalSession(ctx, db.CreateExternalSessionParams{
		ID: uuid.New(), CompanyID: row.CompanyID, ExternalLearnerID: learnerRow.ID,
		TokenHash: sessionHash, ExpiresAt: expiresAt, CreatedAt: nullTimestamptz(&now),
	}); err != nil {
		return ExternalVerificationConfirmed{}, internal("Не удалось сохранить внешнюю сессию", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalVerificationConfirmed{}, internal("Не удалось подтвердить email", err)
	}
	return ExternalVerificationConfirmed{
		LearnerID: learnerRow.ID, VerifiedAt: now, SessionToken: sessionToken, SessionExpiresAt: expiresAt,
	}, nil
}

func (s *Service) AuthenticateExternalSession(ctx context.Context, token string) (ExternalPrincipal, error) {
	if strings.TrimSpace(token) == "" {
		return ExternalPrincipal{}, &Error{Kind: ErrorUnauthenticated, Message: "Требуется внешняя сессия"}
	}
	now := s.now().UTC()
	row, err := db.New(s.pool).ResolveExternalSessionByTokenHash(ctx, db.ResolveExternalSessionByTokenHashParams{
		TokenHash: s.externalTokenHash(token), Now: now,
	})
	if err != nil {
		return ExternalPrincipal{}, &Error{Kind: ErrorUnauthenticated, Message: "Внешняя сессия недействительна или истекла"}
	}
	_, _ = db.New(s.pool).TouchExternalSession(ctx, db.TouchExternalSessionParams{
		LastUsedAt: nullTimestamptz(&now), CompanyID: row.CompanyID, ID: row.ID,
	})
	return ExternalPrincipal{CompanyID: row.CompanyID, LearnerID: row.ExternalLearnerID, SessionID: row.ID, ExpiresAt: row.ExpiresAt}, nil
}

func (s *Service) ActivatePublicAcademyAccess(
	ctx context.Context,
	principal ExternalPrincipal,
	accessToken, idempotencyKey string,
	analytics CampaignAnalyticsContext,
) (Enrollment, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Enrollment{}, validation("Требуется ключ идемпотентности")
	}
	if _, resolveErr := db.New(s.pool).ResolveExternalPersonalAccessByTokenHash(ctx, db.ResolveExternalPersonalAccessByTokenHashParams{
		Now: nullTimestamptzPointer(s.now().UTC()), TokenHash: s.externalTokenHash(accessToken),
	}); isNoRows(resolveErr) {
		return s.activatePublicCampaignAccess(ctx, principal, accessToken, idempotencyKey, analytics)
	} else if resolveErr != nil {
		return Enrollment{}, internal("Не удалось проверить внешний доступ", resolveErr)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Enrollment{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	access, err := queries.ResolveExternalPersonalAccessByTokenHash(ctx, db.ResolveExternalPersonalAccessByTokenHashParams{
		Now: nullTimestamptzPointer(s.now().UTC()), TokenHash: s.externalTokenHash(accessToken),
	})
	if err != nil || access.CompanyID != principal.CompanyID {
		return Enrollment{}, notFound("Внешний доступ")
	}
	learner, err := queries.GetExternalLearner(ctx, db.GetExternalLearnerParams{CompanyID: principal.CompanyID, ID: principal.LearnerID})
	if err != nil || learner.NormalizedEmail != access.NormalizedExpectedEmail {
		return Enrollment{}, forbidden("Внешняя сессия принадлежит другому email")
	}
	now, enrollmentID := s.now().UTC(), uuid.New()
	if _, err = queries.BindExternalPersonalAccessLearner(ctx, db.BindExternalPersonalAccessLearnerParams{
		ExternalLearnerID: nullUUID(&principal.LearnerID), UpdatedAt: now, CompanyID: principal.CompanyID,
		ID: access.ID, NormalizedEmail: learner.NormalizedEmail,
	}); err != nil {
		return Enrollment{}, forbidden("Внешний доступ принадлежит другому ученику")
	}
	activated, err := queries.ActivateExternalPersonalAccess(ctx, db.ActivateExternalPersonalAccessParams{
		CompanyID: principal.CompanyID, PersonalAccessID: access.ID,
		ExternalLearnerID: nullUUID(&principal.LearnerID), EnrollmentID: enrollmentID,
		ActivatedAt: nullTimestamptz(&now),
	})
	if err != nil {
		if isNoRows(err) {
			return Enrollment{}, conflict("Внешний доступ сейчас нельзя активировать")
		}
		return Enrollment{}, internal("Не удалось активировать внешний доступ", err)
	}
	value := enrollmentFromActivatedRow(activated, access.CourseVersionNumber)
	if err = s.recordPersonalAccessHistory(ctx, queries, principal.CompanyID, access.ID,
		nullUUID(&principal.LearnerID), nullUUID(&value.ID), "activated", "external_learner", &principal.LearnerID,
		&idempotencyKey, nil, nil, now); err != nil {
		return Enrollment{}, err
	}
	snapshot, loadErr := s.loadEnrollmentAggregate(ctx, queries, principal.CompanyID, value.ID)
	if loadErr == nil {
		externalActor := Actor{CompanyID: principal.CompanyID, UserID: principal.LearnerID, Role: "external"}
		if err = s.emitEnrollmentActivated(ctx, queries, externalActor, snapshot.Snapshot()); err != nil {
			return Enrollment{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Enrollment{}, internal("Не удалось сохранить активацию", err)
	}
	return value, nil
}

func (s *Service) GetExternalLearners(ctx context.Context, actor Actor) ([]ExternalLearner, error) {
	partnerID, err := externalReportPartnerFilter(actor)
	if err != nil {
		return nil, err
	}
	rows, err := db.New(s.pool).ListExternalLearnersForReport(ctx, db.ListExternalLearnersForReportParams{
		CompanyID: actor.CompanyID, PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		return nil, internal("Не удалось получить внешних учеников", err)
	}
	result := make([]ExternalLearner, len(rows))
	for index, row := range rows {
		result[index] = ExternalLearner{ID: row.ID, CompanyID: row.CompanyID, Email: row.Email,
			NormalizedEmail: row.NormalizedEmail, FirstName: textPointer(row.FirstName), LastName: textPointer(row.LastName),
			Phone: textPointer(row.Phone), EmailVerifiedAt: timestamptzPointer(row.EmailVerifiedAt), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	}
	return result, nil
}

func (s *Service) GetExternalLearner(ctx context.Context, actor Actor, learnerID uuid.UUID) (ExternalLearner, error) {
	partnerID, err := externalReportPartnerFilter(actor)
	if err != nil {
		return ExternalLearner{}, err
	}
	row, err := db.New(s.pool).GetExternalLearnerForReport(ctx, db.GetExternalLearnerForReportParams{
		CompanyID: actor.CompanyID, LearnerID: learnerID, PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		return ExternalLearner{}, notFound("Внешний ученик")
	}
	return ExternalLearner{ID: row.ID, CompanyID: row.CompanyID, Email: row.Email, NormalizedEmail: row.NormalizedEmail,
		FirstName: textPointer(row.FirstName), LastName: textPointer(row.LastName), Phone: textPointer(row.Phone),
		EmailVerifiedAt: timestamptzPointer(row.EmailVerifiedAt), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

func (s *Service) GetExternalLearnerEnrollments(ctx context.Context, actor Actor, learnerID uuid.UUID) ([]Enrollment, error) {
	partnerID, err := externalReportPartnerFilter(actor)
	if err != nil {
		return nil, err
	}
	rows, err := db.New(s.pool).ListExternalLearnerEnrollmentsForReport(ctx, db.ListExternalLearnerEnrollmentsForReportParams{
		CompanyID: actor.CompanyID, LearnerID: nullUUID(&learnerID), PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		return nil, internal("Не удалось получить прохождения внешнего ученика", err)
	}
	result := make([]Enrollment, len(rows))
	for index, row := range rows {
		result[index] = enrollmentFromExternalReportRow(row, learnerID)
	}
	return result, nil
}

func externalReportPartnerFilter(actor Actor) (*uuid.UUID, error) {
	if actor.canManage() {
		return nil, nil
	}
	if actor.Role == "partner" {
		return &actor.UserID, nil
	}
	return nil, forbidden("Недостаточно прав для просмотра внешних учеников")
}

func (s *Service) checkExternalVerificationRate(
	ctx context.Context, queries *db.Queries, companyID, sourceID uuid.UUID, normalized, purpose string,
	ipHash []byte, now time.Time,
) error {
	since := now.Add(-time.Hour)
	emailCount, err := queries.CountRecentExternalChallengesByEmail(ctx, db.CountRecentExternalChallengesByEmailParams{
		CompanyID: companyID, NormalizedEmail: normalized, Since: since,
	})
	if err != nil {
		return internal("Не удалось проверить лимит подтверждений", err)
	}
	sourceCount, err := queries.CountRecentExternalChallengesBySource(ctx, db.CountRecentExternalChallengesBySourceParams{
		CompanyID: companyID, Purpose: purpose, SourceID: nullUUID(&sourceID), Since: since,
	})
	if err != nil {
		return internal("Не удалось проверить лимит доступа", err)
	}
	if emailCount >= 5 || sourceCount >= 10 {
		return &Error{Kind: ErrorUnavailable, Message: "Слишком много запросов кода, попробуйте позже"}
	}
	if len(ipHash) == 32 {
		count, countErr := queries.CountRecentExternalChallengesByIPHash(ctx, db.CountRecentExternalChallengesByIPHashParams{
			CompanyID: companyID, RequestIpHash: ipHash, Since: since,
		})
		if countErr != nil {
			return internal("Не удалось проверить лимит источника", countErr)
		}
		if count >= 20 {
			return &Error{Kind: ErrorUnavailable, Message: "Слишком много запросов кода, попробуйте позже"}
		}
	}
	return nil
}

func (s *Service) ExternalIPHash(raw string) []byte {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return s.externalTokenHash("ip\x00" + strings.TrimSpace(raw))
}

func (s *Service) requireManagedPersonalAccess(
	ctx context.Context, queries *db.Queries, actor Actor, accessID uuid.UUID,
) (db.ExternalPersonalAccess, *domainaccess.Access, error) {
	row, err := queries.GetExternalPersonalAccessForUpdate(ctx, db.GetExternalPersonalAccessForUpdateParams{
		CompanyID: actor.CompanyID, ID: accessID,
	})
	if err != nil {
		if isNoRows(err) {
			return db.ExternalPersonalAccess{}, nil, notFound("Персональный доступ")
		}
		return db.ExternalPersonalAccess{}, nil, internal("Не удалось заблокировать персональный доступ", err)
	}
	aggregate, err := domainaccess.Rehydrate(personalAccessSnapshot(row))
	if err != nil {
		return db.ExternalPersonalAccess{}, nil, internal("Некорректное состояние персонального доступа", err)
	}
	if !domainauth.CanManagePersonalAccess(authorizationActor(actor), aggregate.Snapshot()) {
		return db.ExternalPersonalAccess{}, nil, forbidden("Изменять персональный доступ может только партнёр-владелец курса")
	}
	return row, aggregate, nil
}

func personalAccessSnapshot(row db.ExternalPersonalAccess) domainaccess.Snapshot {
	return domainaccess.Snapshot{
		ID: domainaccess.ID(row.ID.String()), CompanyID: domainaccess.ID(row.CompanyID.String()),
		CourseID: domainaccess.ID(row.CourseID.String()), CourseVersionID: domainaccess.ID(row.CourseVersionID.String()),
		PartnerOwnerID: domainaccess.ID(row.PartnerOwnerID.String()), ExternalLearnerID: personalOptionalID(row.ExternalLearnerID),
		ExpectedEmail: row.ExpectedEmail, NormalizedExpectedEmail: row.NormalizedExpectedEmail,
		RecipientFirstName: textPointer(row.RecipientFirstName), RecipientLastName: textPointer(row.RecipientLastName),
		DeadlineDays: int(row.DeadlineDays), Status: domainaccess.Status(row.Status), TokenHash: row.TokenHash,
		TokenPrefix: row.TokenPrefix, EnrollmentID: personalOptionalID(row.EnrollmentID),
		RootAccessID: domainaccess.ID(row.RootAccessID.String()), RepeatOfAccessID: personalOptionalID(row.RepeatOfAccessID),
		AttemptNumber: int(row.AttemptNumber), IssuanceIdempotencyKey: row.IssuanceIdempotencyKey,
		IssuedByID: domainaccess.ID(row.IssuedByID.String()), IssuedAt: row.IssuedAt,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), TokenRotatedAt: timestamptzPointer(row.TokenRotatedAt),
		RevokedAt: timestamptzPointer(row.RevokedAt), ClosedAt: timestamptzPointer(row.ClosedAt), UpdatedAt: row.UpdatedAt,
	}
}

func personalOptionalID(value uuid.NullUUID) *domainaccess.ID {
	if !value.Valid {
		return nil
	}
	result := domainaccess.ID(value.UUID.String())
	return &result
}

func personalAccessCreateParams(value domainaccess.Snapshot) db.CreateExternalPersonalAccessParams {
	id, _ := uuid.Parse(string(value.ID))
	companyID, _ := uuid.Parse(string(value.CompanyID))
	courseID, _ := uuid.Parse(string(value.CourseID))
	versionID, _ := uuid.Parse(string(value.CourseVersionID))
	partnerID, _ := uuid.Parse(string(value.PartnerOwnerID))
	issuerID, _ := uuid.Parse(string(value.IssuedByID))
	rootID, _ := uuid.Parse(string(value.RootAccessID))
	return db.CreateExternalPersonalAccessParams{
		ID: id, CompanyID: companyID, CourseID: courseID, CourseVersionID: versionID, PartnerOwnerID: partnerID,
		ExternalLearnerID: nullUUID(personalUUID(value.ExternalLearnerID)), ExpectedEmail: value.ExpectedEmail,
		NormalizedExpectedEmail: value.NormalizedExpectedEmail, RecipientFirstName: nullText(value.RecipientFirstName),
		RecipientLastName: nullText(value.RecipientLastName), DeadlineDays: int16(value.DeadlineDays),
		TokenHash: value.TokenHash, TokenPrefix: value.TokenPrefix, RootAccessID: rootID,
		RepeatOfAccessID: nullUUID(personalUUID(value.RepeatOfAccessID)), AttemptNumber: int32(value.AttemptNumber),
		IssuanceIdempotencyKey: value.IssuanceIdempotencyKey, IssuedByID: issuerID, IssuedAt: value.IssuedAt,
	}
}

func personalUUID(value *domainaccess.ID) *uuid.UUID {
	if value == nil {
		return nil
	}
	parsed, err := uuid.Parse(string(*value))
	if err != nil {
		return nil
	}
	return &parsed
}

func personalAccessFromDB(row db.ExternalPersonalAccess) ExternalPersonalAccess {
	return ExternalPersonalAccess{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		PartnerOwnerID: row.PartnerOwnerID, ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID),
		ExpectedEmail: row.ExpectedEmail, RecipientFirstName: textPointer(row.RecipientFirstName),
		RecipientLastName: textPointer(row.RecipientLastName), DeadlineDays: int32(row.DeadlineDays),
		Status: row.Status, TokenPrefix: row.TokenPrefix, EnrollmentID: nullUUIDPointer(row.EnrollmentID),
		IssuedByID: row.IssuedByID, IssuedAt: row.IssuedAt, ActivatedAt: timestamptzPointer(row.ActivatedAt), RevokedAt: timestamptzPointer(row.RevokedAt),
	}
}

func personalAccessFromGetRow(row db.GetExternalPersonalAccessRow) ExternalPersonalAccess {
	return ExternalPersonalAccess{ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		CourseVersionID: row.CourseVersionID, PartnerOwnerID: row.PartnerOwnerID,
		ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID), ExpectedEmail: row.ExpectedEmail,
		RecipientFirstName: textPointer(row.RecipientFirstName), RecipientLastName: textPointer(row.RecipientLastName),
		DeadlineDays: int32(row.DeadlineDays), Status: row.Status, TokenPrefix: row.TokenPrefix,
		EnrollmentID: nullUUIDPointer(row.EnrollmentID), IssuedByID: row.IssuedByID, IssuedAt: row.IssuedAt,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), RevokedAt: timestamptzPointer(row.RevokedAt)}
}

func personalAccessFromListRow(row db.ListExternalPersonalAccessesRow) ExternalPersonalAccess {
	return ExternalPersonalAccess{ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		CourseVersionID: row.CourseVersionID, PartnerOwnerID: row.PartnerOwnerID,
		ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID), ExpectedEmail: row.ExpectedEmail,
		RecipientFirstName: textPointer(row.RecipientFirstName), RecipientLastName: textPointer(row.RecipientLastName),
		DeadlineDays: int32(row.DeadlineDays), Status: row.Status, TokenPrefix: row.TokenPrefix,
		EnrollmentID: nullUUIDPointer(row.EnrollmentID), IssuedByID: row.IssuedByID, IssuedAt: row.IssuedAt,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), RevokedAt: timestamptzPointer(row.RevokedAt)}
}

func personalAccessFromRotateRow(row db.RotateExternalPersonalAccessTokenRow) ExternalPersonalAccess {
	return ExternalPersonalAccess{ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		CourseVersionID: row.CourseVersionID, PartnerOwnerID: row.PartnerOwnerID,
		ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID), ExpectedEmail: row.ExpectedEmail,
		RecipientFirstName: textPointer(row.RecipientFirstName), RecipientLastName: textPointer(row.RecipientLastName),
		DeadlineDays: int32(row.DeadlineDays), Status: row.Status, TokenPrefix: row.TokenPrefix,
		EnrollmentID: nullUUIDPointer(row.EnrollmentID), IssuedByID: row.IssuedByID, IssuedAt: row.IssuedAt,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), RevokedAt: timestamptzPointer(row.RevokedAt)}
}

func personalAccessFromRevokeRow(row db.RevokeExternalPersonalAccessRow) ExternalPersonalAccess {
	return ExternalPersonalAccess{ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		CourseVersionID: row.CourseVersionID, PartnerOwnerID: row.PartnerOwnerID,
		ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID), ExpectedEmail: row.ExpectedEmail,
		RecipientFirstName: textPointer(row.RecipientFirstName), RecipientLastName: textPointer(row.RecipientLastName),
		DeadlineDays: int32(row.DeadlineDays), Status: row.Status, TokenPrefix: row.TokenPrefix,
		EnrollmentID: nullUUIDPointer(row.EnrollmentID), IssuedByID: row.IssuedByID, IssuedAt: row.IssuedAt,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), RevokedAt: timestamptzPointer(row.RevokedAt)}
}

func (s *Service) recordPersonalAccessHistory(
	ctx context.Context, queries *db.Queries, companyID, accessID uuid.UUID,
	learnerID, enrollmentID uuid.NullUUID, eventType, actorType string, actorID *uuid.UUID,
	idempotencyKey, previousPrefix, currentPrefix *string, occurredAt time.Time,
	times ...*time.Time,
) error {
	var before, after *time.Time
	if len(times) > 0 {
		before = times[0]
	}
	if len(times) > 1 {
		after = times[1]
	}
	_, err := queries.CreateExternalPersonalAccessHistory(ctx, db.CreateExternalPersonalAccessHistoryParams{
		ID: uuid.New(), CompanyID: companyID, PersonalAccessID: accessID, ExternalLearnerID: learnerID,
		EnrollmentID: enrollmentID, EventType: eventType, ActorType: actorType, ActorID: nullUUID(actorID),
		IdempotencyKey: nullText(idempotencyKey), PreviousTokenPrefix: nullText(previousPrefix),
		CurrentTokenPrefix: nullText(currentPrefix), AccessUntilBefore: nullTimestamptz(before),
		AccessUntilAfter: nullTimestamptz(after), Details: []byte(`{}`), OccurredAt: occurredAt,
	})
	if err != nil {
		return internal("Не удалось сохранить историю персонального доступа", err)
	}
	return nil
}

func rehydrateVerification(row db.ExternalVerificationChallenge) (*domainverification.Challenge, error) {
	var sourceID *domainverification.ID
	if row.SourceID.Valid {
		value := domainverification.ID(row.SourceID.UUID.String())
		sourceID = &value
	}
	var reason *domainverification.InvalidationReason
	if row.InvalidationReason.Valid {
		value := domainverification.InvalidationReason(row.InvalidationReason.String)
		reason = &value
	}
	return domainverification.Rehydrate(domainverification.Snapshot{
		ID: domainverification.ID(row.ID.String()), CompanyID: domainverification.ID(row.CompanyID.String()),
		NormalizedEmail: row.NormalizedEmail, Purpose: domainverification.Purpose(row.Purpose), SourceID: sourceID,
		ClaimedFirstName: textPointer(row.ClaimedFirstName), ClaimedLastName: textPointer(row.ClaimedLastName),
		CodeHash: row.CodeHash, RequestIPHash: row.RequestIpHash, ExpiresAt: row.ExpiresAt,
		Attempts: int(row.Attempts), MaxAttempts: int(row.MaxAttempts), ConsumedAt: timestamptzPointer(row.ConsumedAt),
		InvalidatedAt: timestamptzPointer(row.InvalidatedAt), InvalidationReason: reason, CreatedAt: row.CreatedAt,
	})
}

func publicOutlineFromRows(rows []db.ListExternalAccessLandingOutlineRow) []PublicAcademyOutlineSection {
	result := make([]PublicAcademyOutlineSection, 0)
	indexes := make(map[uuid.UUID]int)
	for _, row := range rows {
		index, ok := indexes[row.SectionVersionID]
		if !ok {
			index = len(result)
			indexes[row.SectionVersionID] = index
			result = append(result, PublicAcademyOutlineSection{ID: row.SectionVersionID, Title: row.SectionTitle, Order: row.SectionOrder})
		}
		result[index].Lessons = append(result[index].Lessons, PublicAcademyOutlineLesson{
			ID: row.ID, Title: row.Title, Order: row.LessonOrder, EstimatedMinutes: int4Pointer(row.EstimatedMinutes),
		})
	}
	return result
}

func enrollmentFromActivatedRow(row db.ActivateExternalPersonalAccessRow, versionNumber int32) Enrollment {
	return Enrollment{ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		VersionNumber: versionNumber, LearnerType: "external", ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID),
		SourceType: row.SourceType, SourceID: nullUUIDPointer(row.SourceID), AttemptNumber: row.AttemptNumber,
		ProgressStatus: row.ProgressStatus, AccessStatus: row.AccessStatus,
		CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID), ActivatedAt: timestamptzPointer(row.ActivatedAt),
		AccessUntil: timestamptzPointer(row.AccessUntil), StartedAt: timestamptzPointer(row.StartedAt),
		CompletedAt: timestamptzPointer(row.CompletedAt), LastActivityAt: timestamptzPointer(row.LastActivityAt),
		FrozenAt: timestamptzPointer(row.FrozenAt), SuspendedAt: timestamptzPointer(row.SuspendedAt), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func enrollmentFromExternalReportRow(row db.ListExternalLearnerEnrollmentsForReportRow, learnerID uuid.UUID) Enrollment {
	return Enrollment{ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		VersionNumber: row.CourseVersionNumber, LearnerType: "external", ExternalLearnerID: &learnerID,
		SourceType: row.SourceType, SourceID: nullUUIDPointer(row.SourceID), AttemptNumber: row.AttemptNumber,
		ProgressStatus: row.ProgressStatus, AccessStatus: row.AccessStatus,
		CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID), ProgressPercent: row.ProgressPercent,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), AccessUntil: timestamptzPointer(row.AccessUntil),
		StartedAt: timestamptzPointer(row.StartedAt), CompletedAt: timestamptzPointer(row.CompletedAt),
		LastActivityAt: timestamptzPointer(row.LastActivityAt), FrozenAt: timestamptzPointer(row.FrozenAt),
		SuspendedAt: timestamptzPointer(row.SuspendedAt), CreatedAt: row.CreatedAt}
}

func nullTimestamptzPointer(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

var _ = domaincourse.Course{}
