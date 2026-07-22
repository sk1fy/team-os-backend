package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	domainverification "github.com/sk1fy/team-os-backend/services/academy/internal/domain/externalverification"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type campaignAnalyticsEvent struct {
	CampaignID        uuid.UUID
	EnrollmentID      *uuid.UUID
	ExternalLearnerID *uuid.UUID
	Type              string
	IdempotencyKey    string
	Analytics         CampaignAnalyticsContext
	LessonVersionID   *uuid.UUID
	ProgressPercent   *int32
	CompletionSecs    *int64
	OccurredAt        time.Time
}

func (s *Service) requestPublicCampaignVerification(
	ctx context.Context,
	tx pgx.Tx,
	queries *db.Queries,
	input RequestExternalVerificationInput,
	normalized string,
) (ExternalVerificationChallenge, error) {
	campaign, err := queries.ResolveExternalCampaignByTokenHash(ctx, s.externalTokenHash(input.AccessToken))
	if err != nil {
		return ExternalVerificationChallenge{}, notFound("Внешний доступ")
	}
	if !campaign.CanActivate.Valid || !campaign.CanActivate.Bool {
		return ExternalVerificationChallenge{}, conflict(campaignUnavailableReason(
			campaign.Status, campaign.CourseLifecycleStatus, campaign.CourseDistributionStatus, campaign.CourseVersionStatus,
		))
	}
	now := s.now().UTC()
	if err = s.checkExternalVerificationRate(ctx, queries, campaign.CompanyID, campaign.ID, normalized,
		string(domainverification.PurposeCampaignAccess), input.IPHash, now); err != nil {
		return ExternalVerificationChallenge{}, err
	}
	challengeID := uuid.New()
	code, codeHash, err := s.generateVerificationCode(challengeID.String(), normalized)
	if err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось создать код подтверждения", err)
	}
	sourceID := domainverification.ID(campaign.ID.String())
	aggregate, err := domainverification.New(domainverification.NewParams{
		ID: domainverification.ID(challengeID.String()), CompanyID: domainverification.ID(campaign.CompanyID.String()),
		NormalizedEmail: normalized, Purpose: domainverification.PurposeCampaignAccess, SourceID: &sourceID,
		ClaimedFirstName: input.FirstName, ClaimedLastName: input.LastName,
		CodeHash: codeHash, RequestIPHash: input.IPHash, CreatedAt: now,
	})
	if err != nil {
		return ExternalVerificationChallenge{}, validation(err.Error())
	}
	snapshot := aggregate.Snapshot()
	row, err := queries.CreateExternalVerificationChallenge(ctx, db.CreateExternalVerificationChallengeParams{
		ID: challengeID, CompanyID: campaign.CompanyID, NormalizedEmail: normalized,
		Purpose: string(snapshot.Purpose), SourceID: nullUUID(&campaign.ID),
		ClaimedFirstName: nullText(snapshot.ClaimedFirstName), ClaimedLastName: nullText(snapshot.ClaimedLastName),
		CodeHash: snapshot.CodeHash, RequestIpHash: snapshot.RequestIPHash, ExpiresAt: snapshot.ExpiresAt,
		MaxAttempts: int16(snapshot.MaxAttempts), CreatedAt: now,
	})
	if err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось сохранить код подтверждения", err)
	}
	if _, err = queries.InvalidateOpenExternalChallenges(ctx, db.InvalidateOpenExternalChallengesParams{
		InvalidatedAt: nullTimestamptz(&now), InvalidationReason: pgtype.Text{String: "replaced", Valid: true},
		CompanyID: campaign.CompanyID, NormalizedEmail: normalized, Purpose: string(snapshot.Purpose),
		SourceID: nullUUID(&campaign.ID), ExceptID: challengeID,
	}); err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось заменить предыдущий код", err)
	}
	input.Analytics = s.hydrateCampaignAttribution(ctx, queries, campaign.CompanyID, campaign.ID, input.Analytics)
	for _, eventType := range []string{"form_submitted", "verification_requested"} {
		if err = s.recordCampaignAnalyticsEvent(ctx, queries, campaign.CompanyID, campaignAnalyticsEvent{
			CampaignID: campaign.ID, Type: eventType, IdempotencyKey: eventType + ":" + challengeID.String(),
			Analytics: input.Analytics, OccurredAt: now,
		}); err != nil {
			return ExternalVerificationChallenge{}, err
		}
	}
	encrypted, err := s.encryptExternalVerificationDelivery(challengeID.String(), strings.TrimSpace(input.Email), code)
	if err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось защитить письмо подтверждения", err)
	}
	keyID := s.externalEmailKeyID
	if err = s.emit(ctx, queries, campaign.CompanyID, challengeID, uuid.Nil,
		"teamos.academy.external_email_verification.requested.v1",
		&eventsv1.AcademyExternalEmailVerificationRequestedPayload{
			ChallengeId: challengeID.String(), EncryptedDeliveryPayload: encrypted,
			ExpiresAt: timestamppb.New(row.ExpiresAt), Purpose: row.Purpose, KeyId: &keyID,
		}); err != nil {
		return ExternalVerificationChallenge{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalVerificationChallenge{}, internal("Не удалось сохранить подтверждение email", err)
	}
	return ExternalVerificationChallenge{ID: challengeID, ExpiresAt: row.ExpiresAt}, nil
}

func (s *Service) getPublicCampaignAccess(
	ctx context.Context,
	token string,
	principal *ExternalPrincipal,
	analytics CampaignAnalyticsContext,
) (PublicAcademyAccess, error) {
	row, err := db.New(s.pool).ResolveExternalCampaignByTokenHash(ctx, s.externalTokenHash(token))
	if err != nil {
		if isNoRows(err) {
			return PublicAcademyAccess{}, notFound("Внешний доступ")
		}
		return PublicAcademyAccess{}, internal("Не удалось получить внешнюю кампанию", err)
	}
	outlineRows, err := db.New(s.pool).ListExternalCampaignLandingOutline(ctx, db.ListExternalCampaignLandingOutlineParams{
		CompanyID: row.CompanyID, CampaignID: row.ID,
	})
	if err != nil {
		return PublicAcademyAccess{}, internal("Не удалось получить структуру курса", err)
	}
	available := row.CanActivate.Valid && row.CanActivate.Bool
	var reason *string
	if !available {
		value := campaignUnavailableReason(row.Status, row.CourseLifecycleStatus, row.CourseDistributionStatus, row.CourseVersionStatus)
		reason = &value
	}
	verified := principal != nil && principal.CompanyID == row.CompanyID
	now := s.now().UTC()
	eventTypes := []string{"landing_viewed"}
	if verified {
		eventTypes = append(eventTypes, "return_visit")
	}
	for _, eventType := range eventTypes {
		if analyticsErr := s.recordCampaignAnalyticsEvent(ctx, db.New(s.pool), row.CompanyID, campaignAnalyticsEvent{
			CampaignID: row.ID, ExternalLearnerID: campaignPrincipalLearner(principal, row.CompanyID),
			Type: eventType, IdempotencyKey: eventType + ":" + uuid.NewString(), Analytics: analytics, OccurredAt: now,
		}); analyticsErr != nil {
			s.logger.Warn("campaign landing analytics failed", "campaignId", row.ID, "eventType", eventType, "error", analyticsErr)
		}
	}
	return PublicAcademyAccess{
		Kind: campaignSourceType(row.Purpose), CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		Title: interfaceText(row.CourseTitle, "Курс"), Description: interfaceTextPointer(row.CourseDescription),
		OwnerType: row.OwnerType, OwnerUserID: nullUUIDPointer(row.OwnerUserID),
		DeadlineDays: int32(row.DeadlineDays), Available: available, UnavailableReason: reason,
		EmailVerificationRequired: !verified, Outline: publicCampaignOutlineFromRows(outlineRows),
	}, nil
}

func (s *Service) activatePublicCampaignAccess(
	ctx context.Context,
	principal ExternalPrincipal,
	accessToken, idempotencyKey string,
	analytics CampaignAnalyticsContext,
) (Enrollment, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Enrollment{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	campaign, err := queries.ResolveExternalCampaignByTokenHash(ctx, s.externalTokenHash(accessToken))
	if err != nil || campaign.CompanyID != principal.CompanyID {
		return Enrollment{}, notFound("Внешний доступ")
	}
	if !campaign.CanActivate.Valid || !campaign.CanActivate.Bool {
		return Enrollment{}, conflict(campaignUnavailableReason(
			campaign.Status, campaign.CourseLifecycleStatus, campaign.CourseDistributionStatus, campaign.CourseVersionStatus,
		))
	}
	learner, err := queries.GetExternalLearner(ctx, db.GetExternalLearnerParams{
		CompanyID: principal.CompanyID, ID: principal.LearnerID,
	})
	if err != nil || !learner.EmailVerifiedAt.Valid {
		return Enrollment{}, forbidden("Email внешнего ученика не подтверждён")
	}
	now, allocatedEnrollmentID := s.now().UTC(), uuid.New()
	activated, err := queries.ActivateExternalCampaign(ctx, db.ActivateExternalCampaignParams{
		CompanyID: principal.CompanyID, CampaignID: campaign.ID, EnrollmentID: allocatedEnrollmentID,
		ExternalLearnerID: nullUUID(&principal.LearnerID), ActivatedAt: nullTimestamptz(&now),
	})
	if err != nil {
		if isNoRows(err) {
			return Enrollment{}, conflict("Кампанию сейчас нельзя активировать")
		}
		return Enrollment{}, internal("Не удалось активировать кампанию", err)
	}
	value := enrollmentFromCampaignActivatedRow(activated, campaign.CourseVersionNumber)
	analytics = s.hydrateCampaignAttribution(ctx, queries, campaign.CompanyID, campaign.ID, analytics)
	created := value.ID == allocatedEnrollmentID
	eventType := "return_visit"
	if created {
		eventType = "course_activated"
	}
	analyticsEvent := campaignAnalyticsEvent{
		CampaignID: campaign.ID, EnrollmentID: &value.ID, ExternalLearnerID: &principal.LearnerID,
		Type: eventType, IdempotencyKey: eventType + ":" + strings.TrimSpace(idempotencyKey),
		Analytics: analytics, OccurredAt: now,
	}
	if err = s.recordCampaignAnalyticsEvent(ctx, queries, principal.CompanyID, analyticsEvent); err != nil {
		return Enrollment{}, err
	}
	if created {
		firstLessonEvent := analyticsEvent
		firstLessonEvent.Type = "first_lesson_started"
		firstLessonEvent.IdempotencyKey = "first-lesson:" + value.ID.String()
		firstLessonEvent.LessonVersionID = value.CurrentLessonVersionID
		if err = s.recordCampaignAnalyticsEvent(ctx, queries, principal.CompanyID, firstLessonEvent); err != nil {
			return Enrollment{}, err
		}
		snapshot, loadErr := s.loadEnrollmentAggregate(ctx, queries, principal.CompanyID, value.ID)
		if loadErr == nil {
			externalActor := Actor{CompanyID: principal.CompanyID, UserID: principal.LearnerID, Role: "external"}
			if err = s.emitEnrollmentActivated(ctx, queries, externalActor, snapshot.Snapshot()); err != nil {
				return Enrollment{}, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Enrollment{}, internal("Не удалось сохранить активацию кампании", err)
	}
	return value, nil
}

func (s *Service) recordCampaignAnalyticsEvent(
	ctx context.Context,
	queries *db.Queries,
	companyID uuid.UUID,
	event campaignAnalyticsEvent,
) error {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = s.now().UTC()
	}
	if event.EnrollmentID != nil {
		event.Analytics = s.hydrateCampaignAttributionByEnrollment(
			ctx, queries, companyID, event.CampaignID, *event.EnrollmentID, event.Analytics,
		)
	}
	requestPayload, _ := json.Marshal(struct {
		CampaignID        uuid.UUID
		EnrollmentID      *uuid.UUID
		ExternalLearnerID *uuid.UUID
		Type              string
		IdempotencyKey    string
		LessonVersionID   *uuid.UUID
		ProgressPercent   *int32
		CompletionSecs    *int64
		Analytics         CampaignAnalyticsContext
	}{event.CampaignID, event.EnrollmentID, event.ExternalLearnerID, event.Type, event.IdempotencyKey,
		event.LessonVersionID, event.ProgressPercent, event.CompletionSecs, event.Analytics})
	digest := sha256.Sum256(requestPayload)
	visitorHash := event.Analytics.VisitorHash
	visitorKeyID := (*string)(nil)
	if len(visitorHash) != sha256.Size {
		visitorHash = nil
	} else {
		value := "gateway-visitor-sha256-v1"
		visitorKeyID = &value
	}
	row, err := queries.InsertExternalCampaignAnalyticsEvent(ctx, db.InsertExternalCampaignAnalyticsEventParams{
		ID: uuid.New(), CompanyID: companyID, CampaignID: event.CampaignID,
		EnrollmentID: nullUUID(event.EnrollmentID), ExternalLearnerID: nullUUID(event.ExternalLearnerID),
		EventType: event.Type, EventIdempotencyKey: event.IdempotencyKey, RequestHash: hex.EncodeToString(digest[:]),
		VisitorHash: visitorHash, VisitorHashKeyID: nullText(visitorKeyID),
		UtmSource: nullText(event.Analytics.UTMSource), UtmMedium: nullText(event.Analytics.UTMMedium),
		UtmCampaign: nullText(event.Analytics.UTMCampaign), UtmTerm: nullText(event.Analytics.UTMTerm),
		UtmContent: nullText(event.Analytics.UTMContent), Referrer: nullText(event.Analytics.Referrer),
		LessonVersionID: nullUUID(event.LessonVersionID), ProgressPercent: optionalInt2(event.ProgressPercent),
		CompletionSeconds: optionalInt8(event.CompletionSecs), Metadata: []byte(`{}`),
		OccurredAt: event.OccurredAt, ReceivedAt: s.now().UTC(),
	})
	if err != nil {
		return internal("Не удалось сохранить аналитику кампании", err)
	}
	if row.RequestHash != hex.EncodeToString(digest[:]) {
		return conflict("Ключ идемпотентности аналитики уже использован для другого события")
	}
	return nil
}

func publicCampaignOutlineFromRows(rows []db.ListExternalCampaignLandingOutlineRow) []PublicAcademyOutlineSection {
	result := make([]PublicAcademyOutlineSection, 0)
	indexes := make(map[uuid.UUID]int)
	for _, row := range rows {
		index, ok := indexes[row.SectionVersionID]
		if !ok {
			index = len(result)
			indexes[row.SectionVersionID] = index
			result = append(result, PublicAcademyOutlineSection{
				ID: row.SectionVersionID, Title: row.SectionTitle, Order: row.SectionOrder,
			})
		}
		result[index].Lessons = append(result[index].Lessons, PublicAcademyOutlineLesson{
			ID: row.ID, Title: row.Title, Order: row.LessonOrder, EstimatedMinutes: int4Pointer(row.EstimatedMinutes),
		})
	}
	return result
}

func enrollmentFromCampaignActivatedRow(row db.ActivateExternalCampaignRow, versionNumber int32) Enrollment {
	return Enrollment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		VersionNumber: versionNumber, LearnerType: "external", ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID),
		SourceType: row.SourceType, SourceID: nullUUIDPointer(row.SourceID), AttemptNumber: row.AttemptNumber,
		ProgressStatus: row.ProgressStatus, AccessStatus: row.AccessStatus,
		CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID), ActivatedAt: timestamptzPointer(row.ActivatedAt),
		AccessUntil: timestamptzPointer(row.AccessUntil), StartedAt: timestamptzPointer(row.StartedAt),
		CompletedAt: timestamptzPointer(row.CompletedAt), LastActivityAt: timestamptzPointer(row.LastActivityAt),
		FrozenAt: timestamptzPointer(row.FrozenAt), SuspendedAt: timestamptzPointer(row.SuspendedAt),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func campaignUnavailableReason(status, lifecycle, distribution, versionStatus string) string {
	switch {
	case lifecycle == "deleted":
		return "Курс удалён"
	case distribution == "blocked":
		return "Курс временно заблокирован"
	case lifecycle == "archived":
		return "Курс находится в архиве"
	case distribution == "paused":
		return "Распространение курса приостановлено"
	case status == "paused":
		return "Кампания приостановлена"
	case status == "revoked":
		return "Кампания отозвана"
	case status == "closed":
		return "Кампания закрыта"
	case versionStatus != "published":
		return "Версия курса недоступна"
	default:
		return "Курс сейчас недоступен"
	}
}

func campaignSourceType(purpose string) string {
	if purpose == "partner_promo" {
		return "partner_promo_campaign"
	}
	return "company_candidate_campaign"
}

func campaignPrincipalLearner(principal *ExternalPrincipal, companyID uuid.UUID) *uuid.UUID {
	if principal == nil || principal.CompanyID != companyID {
		return nil
	}
	return &principal.LearnerID
}

func interfaceText(value any, fallback string) string {
	switch converted := value.(type) {
	case string:
		if strings.TrimSpace(converted) != "" {
			return converted
		}
	case []byte:
		if strings.TrimSpace(string(converted)) != "" {
			return string(converted)
		}
	}
	return fallback
}

func interfaceTextPointer(value any) *string {
	converted := interfaceText(value, "")
	if converted == "" {
		return nil
	}
	return &converted
}

func optionalInt2(value *int32) pgtype.Int2 {
	if value == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: int16(*value), Valid: true}
}

func optionalInt8(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

func (s *Service) hydrateCampaignAttribution(
	ctx context.Context,
	queries *db.Queries,
	companyID, campaignID uuid.UUID,
	analytics CampaignAnalyticsContext,
) CampaignAnalyticsContext {
	if len(analytics.VisitorHash) != sha256.Size || analytics.UTMSource != nil || analytics.UTMMedium != nil ||
		analytics.UTMCampaign != nil || analytics.UTMContent != nil || analytics.UTMTerm != nil || analytics.Referrer != nil {
		return analytics
	}
	row, err := queries.GetExternalCampaignAttributionByVisitorHash(ctx, db.GetExternalCampaignAttributionByVisitorHashParams{
		CompanyID: companyID, CampaignID: campaignID, VisitorHash: analytics.VisitorHash,
	})
	if err != nil {
		return analytics
	}
	analytics.UTMSource = textPointer(row.UtmSource)
	analytics.UTMMedium = textPointer(row.UtmMedium)
	analytics.UTMCampaign = textPointer(row.UtmCampaign)
	analytics.UTMContent = textPointer(row.UtmContent)
	analytics.UTMTerm = textPointer(row.UtmTerm)
	analytics.Referrer = textPointer(row.Referrer)
	return analytics
}

func (s *Service) hydrateCampaignAttributionByEnrollment(
	ctx context.Context,
	queries *db.Queries,
	companyID, campaignID, enrollmentID uuid.UUID,
	analytics CampaignAnalyticsContext,
) CampaignAnalyticsContext {
	if len(analytics.VisitorHash) == sha256.Size || analytics.UTMSource != nil || analytics.UTMMedium != nil ||
		analytics.UTMCampaign != nil || analytics.UTMContent != nil || analytics.UTMTerm != nil || analytics.Referrer != nil {
		return analytics
	}
	row, err := queries.GetExternalCampaignAttributionByEnrollment(ctx, db.GetExternalCampaignAttributionByEnrollmentParams{
		CompanyID: companyID, CampaignID: campaignID, EnrollmentID: nullUUID(&enrollmentID),
	})
	if err != nil {
		return analytics
	}
	analytics.VisitorHash = append([]byte(nil), row.VisitorHash...)
	analytics.UTMSource = textPointer(row.UtmSource)
	analytics.UTMMedium = textPointer(row.UtmMedium)
	analytics.UTMCampaign = textPointer(row.UtmCampaign)
	analytics.UTMContent = textPointer(row.UtmContent)
	analytics.UTMTerm = textPointer(row.UtmTerm)
	analytics.Referrer = textPointer(row.Referrer)
	return analytics
}
