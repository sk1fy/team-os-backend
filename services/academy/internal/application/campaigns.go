package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	domainauth "github.com/sk1fy/team-os-backend/services/academy/internal/domain/authorization"
	domaincampaign "github.com/sk1fy/team-os-backend/services/academy/internal/domain/externalcampaign"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type CreateExternalCampaignInput struct {
	CourseID        uuid.UUID
	CourseVersionID uuid.UUID
	Name            string
	Purpose         string
	DeadlineDays    int32
}

func (s *Service) CreateExternalCampaign(
	ctx context.Context,
	actor Actor,
	input CreateExternalCampaignInput,
) (ExternalCampaignCreated, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalCampaignCreated{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: input.CourseID})
	if err != nil {
		if isNoRows(err) {
			return ExternalCampaignCreated{}, notFound("Курс")
		}
		return ExternalCampaignCreated{}, internal("Не удалось проверить курс", err)
	}
	versionRow, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: input.CourseVersionID})
	if err != nil || versionRow.CourseID != input.CourseID {
		if isNoRows(err) || err == nil {
			return ExternalCampaignCreated{}, notFound("Версия курса")
		}
		return ExternalCampaignCreated{}, internal("Не удалось проверить версию курса", err)
	}
	content, err := s.loadCourseVersionContent(ctx, queries, versionRow)
	if err != nil {
		return ExternalCampaignCreated{}, err
	}
	domainVersion, err := domainVersionFromContent(content)
	if err != nil {
		return ExternalCampaignCreated{}, internal("Некорректное состояние версии курса", err)
	}
	purpose := domaincampaign.Purpose(input.Purpose)
	course := courseFromRow(courseRow)
	if !domainauth.CanCreateExternalCampaign(
		authorizationActor(actor), authorizationCourse(course), domainVersion.Snapshot(), purpose,
	) {
		return ExternalCampaignCreated{}, forbidden("Недостаточно прав для создания кампании этого типа")
	}
	token, tokenHash, prefix, err := s.generateExternalToken()
	if err != nil {
		return ExternalCampaignCreated{}, internal("Не удалось создать токен кампании", err)
	}
	ownerType := domaincampaign.OwnerType(course.OwnerType)
	var ownerID *domaincampaign.ID
	if course.OwnerUserID != nil {
		value := domaincampaign.ID(course.OwnerUserID.String())
		ownerID = &value
	}
	now, campaignID := s.now().UTC(), uuid.New()
	aggregate, err := domaincampaign.New(domaincampaign.NewParams{
		ID: domaincampaign.ID(campaignID.String()), CompanyID: domaincampaign.ID(actor.CompanyID.String()),
		CourseID: domaincampaign.ID(input.CourseID.String()), CourseVersionID: domaincampaign.ID(input.CourseVersionID.String()),
		OwnerType: ownerType, OwnerUserID: ownerID, Purpose: purpose, Name: input.Name,
		DeadlineDays: int(input.DeadlineDays), TokenHash: tokenHash, TokenPrefix: prefix,
		CreatedByID: domaincampaign.ID(actor.UserID.String()), CreatedAt: now,
	})
	if err != nil {
		return ExternalCampaignCreated{}, validation(err.Error())
	}
	row, err := queries.CreateExternalCampaign(ctx, campaignCreateParams(aggregate.Snapshot()))
	if err != nil {
		return ExternalCampaignCreated{}, internal("Не удалось создать кампанию", err)
	}
	value := campaignFromCreateRow(row)
	if err = s.recordCampaignHistory(ctx, queries, value.CompanyID, value.ID, "created", actor,
		nil, value.Status, nil, &value.TokenPrefix, now); err != nil {
		return ExternalCampaignCreated{}, err
	}
	if err = s.auditCampaign(ctx, queries, actor, "campaign_created", nil, value); err != nil {
		return ExternalCampaignCreated{}, err
	}
	if err = s.emit(ctx, queries, actor.CompanyID, value.ID, actor.UserID,
		"teamos.academy.external_campaign.created.v1", &eventsv1.AcademyExternalCampaignCreatedPayload{
			CampaignId: value.ID.String(), CourseId: value.CourseID.String(),
			CourseVersionId: value.CourseVersionID.String(), OwnerType: campaignOwnerTypeToEvent(value.OwnerType),
			OwnerUserId: optionalUUIDString(value.OwnerUserID), Purpose: campaignPurposeToEvent(value.Purpose),
			CreatedById: actor.UserID.String(), CreatedAt: timestamppb.New(now),
		}); err != nil {
		return ExternalCampaignCreated{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalCampaignCreated{}, internal("Не удалось сохранить кампанию", err)
	}
	return ExternalCampaignCreated{Campaign: value, Token: token}, nil
}

func (s *Service) GetExternalCampaigns(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
) ([]ExternalCampaign, error) {
	partnerID, err := campaignPartnerScope(actor)
	if err != nil {
		return nil, err
	}
	rows, err := db.New(s.pool).ListExternalCampaigns(ctx, db.ListExternalCampaignsParams{
		CompanyID: actor.CompanyID, PartnerOwnerID: nullUUID(partnerID), CourseID: nullUUID(&courseID),
	})
	if err != nil {
		return nil, internal("Не удалось получить кампании", err)
	}
	result := make([]ExternalCampaign, len(rows))
	for index, row := range rows {
		result[index] = campaignFromListRow(row)
	}
	return result, nil
}

func (s *Service) GetExternalCampaign(
	ctx context.Context,
	actor Actor,
	campaignID uuid.UUID,
) (ExternalCampaign, error) {
	partnerID, err := campaignPartnerScope(actor)
	if err != nil {
		return ExternalCampaign{}, err
	}
	row, err := db.New(s.pool).GetExternalCampaign(ctx, db.GetExternalCampaignParams{
		CompanyID: actor.CompanyID, ID: campaignID, PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		if isNoRows(err) {
			return ExternalCampaign{}, notFound("Кампания")
		}
		return ExternalCampaign{}, internal("Не удалось получить кампанию", err)
	}
	value := campaignFromGetRow(row)
	if !domainauth.CanViewExternalCampaign(authorizationActor(actor), campaignDomainSnapshot(value, nil)) {
		return ExternalCampaign{}, notFound("Кампания")
	}
	return value, nil
}

func (s *Service) PauseExternalCampaign(ctx context.Context, actor Actor, campaignID uuid.UUID) (ExternalCampaign, error) {
	return s.changeExternalCampaign(ctx, actor, campaignID, "campaign_paused", func(
		ctx context.Context, queries *db.Queries, aggregate *domaincampaign.Campaign, now time.Time, partnerID *uuid.UUID,
	) error {
		if err := aggregate.Pause(now); err != nil {
			return conflict(err.Error())
		}
		_, err := queries.PauseExternalCampaign(ctx, db.PauseExternalCampaignParams{
			ChangedAt: nullTimestamptz(&now), CompanyID: actor.CompanyID, ID: campaignID,
			PartnerOwnerID: nullUUID(partnerID),
		})
		return err
	})
}

func (s *Service) ResumeExternalCampaign(ctx context.Context, actor Actor, campaignID uuid.UUID) (ExternalCampaign, error) {
	return s.changeExternalCampaign(ctx, actor, campaignID, "campaign_resumed", func(
		ctx context.Context, queries *db.Queries, aggregate *domaincampaign.Campaign, now time.Time, partnerID *uuid.UUID,
	) error {
		snapshot := aggregate.Snapshot()
		courseID, _ := uuid.Parse(string(snapshot.CourseID))
		versionID, _ := uuid.Parse(string(snapshot.CourseVersionID))
		courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
		if err != nil {
			return err
		}
		versionRow, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: versionID})
		if err != nil {
			return err
		}
		content, err := s.loadCourseVersionContent(ctx, queries, versionRow)
		if err != nil {
			return err
		}
		domainVersion, err := domainVersionFromContent(content)
		if err != nil {
			return err
		}
		if err = aggregate.Resume(authorizationCourse(courseFromRow(courseRow)), domainVersion.Snapshot(), now); err != nil {
			return conflict(err.Error())
		}
		_, err = queries.ResumeExternalCampaign(ctx, db.ResumeExternalCampaignParams{
			ChangedAt: now, CompanyID: actor.CompanyID, ID: campaignID, PartnerOwnerID: nullUUID(partnerID),
		})
		return err
	})
}

func (s *Service) RotateExternalCampaignToken(
	ctx context.Context,
	actor Actor,
	campaignID uuid.UUID,
) (ExternalCampaignCreated, error) {
	token, tokenHash, prefix, err := s.generateExternalToken()
	if err != nil {
		return ExternalCampaignCreated{}, internal("Не удалось создать новый токен кампании", err)
	}
	value, err := s.changeExternalCampaign(ctx, actor, campaignID, "campaign_token_rotated", func(
		ctx context.Context, queries *db.Queries, aggregate *domaincampaign.Campaign, now time.Time, partnerID *uuid.UUID,
	) error {
		if err := aggregate.RotateToken(tokenHash, prefix, now); err != nil {
			return conflict(err.Error())
		}
		_, err := queries.RotateExternalCampaignToken(ctx, db.RotateExternalCampaignTokenParams{
			TokenHash: tokenHash, TokenPrefix: prefix, RotatedAt: nullTimestamptz(&now),
			CompanyID: actor.CompanyID, ID: campaignID, PartnerOwnerID: nullUUID(partnerID),
		})
		return err
	})
	if err != nil {
		return ExternalCampaignCreated{}, err
	}
	return ExternalCampaignCreated{Campaign: value, Token: token}, nil
}

func (s *Service) RevokeExternalCampaign(ctx context.Context, actor Actor, campaignID uuid.UUID) (ExternalCampaign, error) {
	return s.changeExternalCampaign(ctx, actor, campaignID, "campaign_revoked", func(
		ctx context.Context, queries *db.Queries, aggregate *domaincampaign.Campaign, now time.Time, partnerID *uuid.UUID,
	) error {
		if err := aggregate.Revoke(now); err != nil {
			return conflict(err.Error())
		}
		_, err := queries.RevokeExternalCampaign(ctx, db.RevokeExternalCampaignParams{
			ChangedAt: nullTimestamptz(&now), CompanyID: actor.CompanyID, ID: campaignID,
			PartnerOwnerID: nullUUID(partnerID),
		})
		return err
	})
}

type campaignChange func(context.Context, *db.Queries, *domaincampaign.Campaign, time.Time, *uuid.UUID) error

func (s *Service) changeExternalCampaign(
	ctx context.Context,
	actor Actor,
	campaignID uuid.UUID,
	action string,
	change campaignChange,
) (ExternalCampaign, error) {
	partnerID, err := campaignPartnerScope(actor)
	if err != nil {
		return ExternalCampaign{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalCampaign{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetExternalCampaignForUpdate(ctx, db.GetExternalCampaignForUpdateParams{
		CompanyID: actor.CompanyID, ID: campaignID, PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		if isNoRows(err) {
			return ExternalCampaign{}, notFound("Кампания")
		}
		return ExternalCampaign{}, internal("Не удалось заблокировать кампанию", err)
	}
	aggregate, err := domaincampaign.Rehydrate(campaignSnapshotFromDB(row))
	if err != nil {
		return ExternalCampaign{}, internal("Некорректное состояние кампании", err)
	}
	beforeSnapshot := aggregate.Snapshot()
	if !domainauth.CanManageExternalCampaign(authorizationActor(actor), beforeSnapshot) {
		return ExternalCampaign{}, forbidden("Недостаточно прав для изменения кампании")
	}
	now := s.now().UTC()
	if err = change(ctx, queries, aggregate, now, partnerID); err != nil {
		var applicationErr *Error
		if errors.As(err, &applicationErr) {
			return ExternalCampaign{}, err
		}
		if isNoRows(err) {
			return ExternalCampaign{}, conflict("Кампанию нельзя изменить в текущем состоянии")
		}
		return ExternalCampaign{}, internal("Не удалось изменить кампанию", err)
	}
	afterSnapshot := aggregate.Snapshot()
	after := campaignFromSnapshot(afterSnapshot)
	eventType := campaignHistoryEventType(action)
	if err = s.recordCampaignHistory(ctx, queries, actor.CompanyID, campaignID, eventType, actor,
		stringPointer(string(beforeSnapshot.Status)), string(afterSnapshot.Status),
		stringPointer(beforeSnapshot.TokenPrefix), stringPointer(afterSnapshot.TokenPrefix), now); err != nil {
		return ExternalCampaign{}, err
	}
	before := campaignFromSnapshot(beforeSnapshot)
	if err = s.auditCampaign(ctx, queries, actor, action, &before, after); err != nil {
		return ExternalCampaign{}, err
	}
	if beforeSnapshot.Status != afterSnapshot.Status {
		if err = s.emit(ctx, queries, actor.CompanyID, campaignID, actor.UserID,
			"teamos.academy.external_campaign.status_changed.v1", &eventsv1.AcademyExternalCampaignStatusChangedPayload{
				CampaignId: campaignID.String(), CourseId: after.CourseID.String(),
				PreviousStatus: campaignStatusToEvent(string(beforeSnapshot.Status)),
				Status:         campaignStatusToEvent(after.Status), ChangedById: actor.UserID.String(),
				ChangedAt: timestamppb.New(now),
			}); err != nil {
			return ExternalCampaign{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return ExternalCampaign{}, internal("Не удалось сохранить изменение кампании", err)
	}
	return after, nil
}

func campaignPartnerScope(actor Actor) (*uuid.UUID, error) {
	if actor.canManage() {
		return nil, nil
	}
	if actor.Role == "partner" {
		return &actor.UserID, nil
	}
	return nil, forbidden("Недостаточно прав для просмотра кампаний")
}

func campaignCreateParams(value domaincampaign.Snapshot) db.CreateExternalCampaignParams {
	id, _ := uuid.Parse(string(value.ID))
	companyID, _ := uuid.Parse(string(value.CompanyID))
	courseID, _ := uuid.Parse(string(value.CourseID))
	versionID, _ := uuid.Parse(string(value.CourseVersionID))
	creatorID, _ := uuid.Parse(string(value.CreatedByID))
	var ownerID *uuid.UUID
	if value.OwnerUserID != nil {
		parsed, _ := uuid.Parse(string(*value.OwnerUserID))
		ownerID = &parsed
	}
	return db.CreateExternalCampaignParams{
		ID: id, CompanyID: companyID, CourseID: courseID, CourseVersionID: versionID,
		OwnerType: string(value.OwnerType), OwnerUserID: nullUUID(ownerID), Purpose: string(value.Purpose),
		Name: value.Name, DeadlineDays: int16(value.DeadlineDays), TokenHash: value.TokenHash,
		TokenPrefix: value.TokenPrefix, CreatedByID: creatorID, CreatedAt: value.CreatedAt,
	}
}

func campaignSnapshotFromDB(row db.ExternalCampaign) domaincampaign.Snapshot {
	var ownerID *domaincampaign.ID
	if row.OwnerUserID.Valid {
		value := domaincampaign.ID(row.OwnerUserID.UUID.String())
		ownerID = &value
	}
	return domaincampaign.Snapshot{
		ID: domaincampaign.ID(row.ID.String()), CompanyID: domaincampaign.ID(row.CompanyID.String()),
		CourseID: domaincampaign.ID(row.CourseID.String()), CourseVersionID: domaincampaign.ID(row.CourseVersionID.String()),
		OwnerType: domaincampaign.OwnerType(row.OwnerType), OwnerUserID: ownerID,
		Purpose: domaincampaign.Purpose(row.Purpose), Name: row.Name, DeadlineDays: int(row.DeadlineDays),
		Status: domaincampaign.Status(row.Status), TokenHash: append([]byte(nil), row.TokenHash...),
		TokenPrefix: row.TokenPrefix, CreatedByID: domaincampaign.ID(row.CreatedByID.String()),
		CreatedAt: row.CreatedAt, PausedAt: timestamptzPointer(row.PausedAt),
		TokenRotatedAt: timestamptzPointer(row.TokenRotatedAt), RevokedAt: timestamptzPointer(row.RevokedAt),
		ClosedAt: timestamptzPointer(row.ClosedAt), UpdatedAt: row.UpdatedAt,
	}
}

func campaignDomainSnapshot(value ExternalCampaign, tokenHash []byte) domaincampaign.Snapshot {
	var ownerID *domaincampaign.ID
	if value.OwnerUserID != nil {
		converted := domaincampaign.ID(value.OwnerUserID.String())
		ownerID = &converted
	}
	return domaincampaign.Snapshot{
		ID: domaincampaign.ID(value.ID.String()), CompanyID: domaincampaign.ID(value.CompanyID.String()),
		CourseID: domaincampaign.ID(value.CourseID.String()), CourseVersionID: domaincampaign.ID(value.CourseVersionID.String()),
		OwnerType: domaincampaign.OwnerType(value.OwnerType), OwnerUserID: ownerID,
		Purpose: domaincampaign.Purpose(value.Purpose), Name: value.Name, DeadlineDays: int(value.DeadlineDays),
		Status: domaincampaign.Status(value.Status), TokenHash: tokenHash, TokenPrefix: value.TokenPrefix,
		CreatedByID: domaincampaign.ID(value.CreatedByID.String()), CreatedAt: value.CreatedAt, UpdatedAt: value.CreatedAt,
		PausedAt: value.PausedAt, RevokedAt: value.RevokedAt,
	}
}

func campaignFromSnapshot(value domaincampaign.Snapshot) ExternalCampaign {
	id, _ := uuid.Parse(string(value.ID))
	companyID, _ := uuid.Parse(string(value.CompanyID))
	courseID, _ := uuid.Parse(string(value.CourseID))
	versionID, _ := uuid.Parse(string(value.CourseVersionID))
	creatorID, _ := uuid.Parse(string(value.CreatedByID))
	var ownerID *uuid.UUID
	if value.OwnerUserID != nil {
		parsed, _ := uuid.Parse(string(*value.OwnerUserID))
		ownerID = &parsed
	}
	return ExternalCampaign{
		ID: id, CompanyID: companyID, CourseID: courseID, CourseVersionID: versionID,
		OwnerType: string(value.OwnerType), OwnerUserID: ownerID, Purpose: string(value.Purpose),
		Name: value.Name, DeadlineDays: int32(value.DeadlineDays), Status: string(value.Status),
		TokenPrefix: value.TokenPrefix, CreatedByID: creatorID, CreatedAt: value.CreatedAt,
		PausedAt: value.PausedAt, RevokedAt: value.RevokedAt,
	}
}

func campaignFromCreateRow(row db.CreateExternalCampaignRow) ExternalCampaign {
	return ExternalCampaign{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		OwnerType: row.OwnerType, OwnerUserID: nullUUIDPointer(row.OwnerUserID), Purpose: row.Purpose,
		Name: row.Name, DeadlineDays: int32(row.DeadlineDays), Status: row.Status, TokenPrefix: row.TokenPrefix,
		CreatedByID: row.CreatedByID, CreatedAt: row.CreatedAt, PausedAt: timestamptzPointer(row.PausedAt),
		RevokedAt: timestamptzPointer(row.RevokedAt),
	}
}

func campaignFromGetRow(row db.GetExternalCampaignRow) ExternalCampaign {
	return ExternalCampaign{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		OwnerType: row.OwnerType, OwnerUserID: nullUUIDPointer(row.OwnerUserID), Purpose: row.Purpose,
		Name: row.Name, DeadlineDays: int32(row.DeadlineDays), Status: row.Status, TokenPrefix: row.TokenPrefix,
		CreatedByID: row.CreatedByID, CreatedAt: row.CreatedAt, PausedAt: timestamptzPointer(row.PausedAt),
		RevokedAt: timestamptzPointer(row.RevokedAt),
	}
}

func campaignFromListRow(row db.ListExternalCampaignsRow) ExternalCampaign {
	return ExternalCampaign{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		OwnerType: row.OwnerType, OwnerUserID: nullUUIDPointer(row.OwnerUserID), Purpose: row.Purpose,
		Name: row.Name, DeadlineDays: int32(row.DeadlineDays), Status: row.Status, TokenPrefix: row.TokenPrefix,
		CreatedByID: row.CreatedByID, CreatedAt: row.CreatedAt, PausedAt: timestamptzPointer(row.PausedAt),
		RevokedAt: timestamptzPointer(row.RevokedAt),
	}
}

func (s *Service) recordCampaignHistory(
	ctx context.Context,
	queries *db.Queries,
	companyID, campaignID uuid.UUID,
	eventType string,
	actor Actor,
	previousStatus *string,
	currentStatus string,
	previousPrefix, currentPrefix *string,
	occurredAt time.Time,
) error {
	_, err := queries.InsertExternalCampaignHistory(ctx, db.InsertExternalCampaignHistoryParams{
		ID: uuid.New(), CompanyID: companyID, CampaignID: campaignID, EventType: eventType,
		ActorType: "internal", ActorID: nullUUID(&actor.UserID), PreviousStatus: nullText(previousStatus),
		CurrentStatus: currentStatus, PreviousTokenPrefix: nullText(previousPrefix),
		CurrentTokenPrefix: nullText(currentPrefix), Details: []byte(`{}`), OccurredAt: occurredAt,
	})
	if err != nil {
		return internal("Не удалось сохранить историю кампании", err)
	}
	return nil
}

func (s *Service) auditCampaign(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	action string,
	before *ExternalCampaign,
	after ExternalCampaign,
) error {
	var beforeState []byte
	if before != nil {
		beforeState, _ = json.Marshal(before)
	}
	afterState, _ := json.Marshal(after)
	_, err := queries.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, ActorID: actor.UserID, ActorRole: actor.Role,
		Action: action, AggregateType: "external_campaign", AggregateID: after.ID,
		BeforeState: beforeState, AfterState: afterState, CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return internal("Не удалось сохранить аудит кампании", err)
	}
	return nil
}

func campaignHistoryEventType(action string) string {
	switch action {
	case "campaign_paused":
		return "paused"
	case "campaign_resumed":
		return "resumed"
	case "campaign_revoked":
		return "revoked"
	case "campaign_token_rotated":
		return "token_rotated"
	default:
		return action
	}
}

func campaignOwnerTypeToEvent(value string) eventsv1.AcademyCourseOwnerType {
	if value == "partner" {
		return eventsv1.AcademyCourseOwnerType_ACADEMY_COURSE_OWNER_TYPE_PARTNER
	}
	return eventsv1.AcademyCourseOwnerType_ACADEMY_COURSE_OWNER_TYPE_COMPANY
}

func campaignPurposeToEvent(value string) eventsv1.AcademyExternalCampaignPurpose {
	if value == "partner_promo" {
		return eventsv1.AcademyExternalCampaignPurpose_ACADEMY_EXTERNAL_CAMPAIGN_PURPOSE_PARTNER_PROMO
	}
	return eventsv1.AcademyExternalCampaignPurpose_ACADEMY_EXTERNAL_CAMPAIGN_PURPOSE_COMPANY_CANDIDATE
}

func campaignStatusToEvent(value string) eventsv1.AcademyExternalCampaignStatus {
	switch value {
	case "active":
		return eventsv1.AcademyExternalCampaignStatus_ACADEMY_EXTERNAL_CAMPAIGN_STATUS_ACTIVE
	case "paused":
		return eventsv1.AcademyExternalCampaignStatus_ACADEMY_EXTERNAL_CAMPAIGN_STATUS_PAUSED
	case "revoked":
		return eventsv1.AcademyExternalCampaignStatus_ACADEMY_EXTERNAL_CAMPAIGN_STATUS_REVOKED
	case "closed":
		return eventsv1.AcademyExternalCampaignStatus_ACADEMY_EXTERNAL_CAMPAIGN_STATUS_CLOSED
	default:
		return eventsv1.AcademyExternalCampaignStatus_ACADEMY_EXTERNAL_CAMPAIGN_STATUS_UNSPECIFIED
	}
}

func optionalUUIDString(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}

func stringPointer(value string) *string { return &value }
