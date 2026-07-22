package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	domainaccess "github.com/sk1fy/team-os-backend/services/kb/internal/domain/access"
	"github.com/sk1fy/team-os-backend/services/kb/internal/storage/db"
)

func (s *Service) GetArticlePartnerPolicy(ctx context.Context, actor Actor, articleID uuid.UUID) (ArticlePartnerPolicy, error) {
	if !domainaccess.CanManage(actor.subject()) {
		return ArticlePartnerPolicy{}, forbidden("Недостаточно прав для просмотра партнёрской политики статьи")
	}
	queries := db.New(s.pool)
	row, err := queries.GetArticle(ctx, db.GetArticleParams{CompanyID: actor.CompanyID, ID: articleID})
	if err != nil {
		if isNoRows(err) {
			return ArticlePartnerPolicy{}, notFound("Статья")
		}
		return ArticlePartnerPolicy{}, internal("Не удалось получить статью", err)
	}
	grants, err := queries.ListArticlePartnerAccessGrants(ctx, db.ListArticlePartnerAccessGrantsParams{
		CompanyID: actor.CompanyID, ArticleID: articleID,
	})
	if err != nil {
		return ArticlePartnerPolicy{}, internal("Не удалось получить партнёрские доступы", err)
	}
	partnerIDs := make([]uuid.UUID, 0, len(grants))
	for _, grant := range grants {
		partnerIDs = append(partnerIDs, grant.PartnerID)
	}
	return ArticlePartnerPolicy{
		ArticleID:   articleID,
		Access:      PartnerAccessSettings{Mode: row.PartnerAccessMode, PartnerIDs: partnerIDs},
		ReusePolicy: row.PartnerReusePolicy, UpdatedAt: row.UpdatedAt,
	}, nil
}

func (s *Service) UpdateArticlePartnerPolicy(
	ctx context.Context,
	actor Actor,
	articleID uuid.UUID,
	access PartnerAccessSettings,
	reusePolicy string,
) (ArticlePartnerPolicy, error) {
	if !domainaccess.CanManage(actor.subject()) {
		return ArticlePartnerPolicy{}, forbidden("Недостаточно прав для изменения партнёрской политики статьи")
	}
	if err := validatePartnerAccess(access); err != nil {
		return ArticlePartnerPolicy{}, err
	}
	if reusePolicy != "not_allowed" && reusePolicy != "copy_allowed" {
		return ArticlePartnerPolicy{}, validation("Некорректная политика повторного использования статьи")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ArticlePartnerPolicy{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if _, err = queries.GetArticleForUpdate(ctx, db.GetArticleForUpdateParams{
		CompanyID: actor.CompanyID, ID: articleID,
	}); err != nil {
		if isNoRows(err) {
			return ArticlePartnerPolicy{}, notFound("Статья")
		}
		return ArticlePartnerPolicy{}, internal("Не удалось получить статью", err)
	}
	now := s.now().UTC()
	if _, err = queries.UpdateArticlePartnerAccessMode(ctx, db.UpdateArticlePartnerAccessModeParams{
		PartnerAccessMode: access.Mode, UpdatedAt: now,
		CompanyID: actor.CompanyID, ID: articleID,
	}); err != nil {
		return ArticlePartnerPolicy{}, internal("Не удалось обновить доступ партнёров", err)
	}
	if _, err = queries.DeleteArticlePartnerAccessGrants(ctx, db.DeleteArticlePartnerAccessGrantsParams{
		CompanyID: actor.CompanyID, ArticleID: articleID,
	}); err != nil {
		return ArticlePartnerPolicy{}, internal("Не удалось обновить список партнёров", err)
	}
	if access.Mode == "selected" {
		if _, err = queries.CreateArticlePartnerAccessGrants(ctx, db.CreateArticlePartnerAccessGrantsParams{
			GrantedByID: actor.UserID, GrantedAt: now, PartnerIds: access.PartnerIDs,
			CompanyID: actor.CompanyID, ArticleID: articleID,
		}); err != nil {
			return ArticlePartnerPolicy{}, internal("Не удалось предоставить доступ партнёрам", err)
		}
	}
	if _, err = queries.UpdateArticlePartnerReusePolicy(ctx, db.UpdateArticlePartnerReusePolicyParams{
		PartnerReusePolicy: reusePolicy, UpdatedAt: now,
		CompanyID: actor.CompanyID, ID: articleID,
	}); err != nil {
		return ArticlePartnerPolicy{}, internal("Не удалось обновить повторное использование статьи", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return ArticlePartnerPolicy{}, internal("Не удалось сохранить партнёрскую политику", err)
	}
	updatedBy := actor.UserID
	return ArticlePartnerPolicy{
		ArticleID: articleID, Access: access, ReusePolicy: reusePolicy,
		UpdatedAt: now, UpdatedByID: &updatedBy,
	}, nil
}

func (s *Service) CheckArticleCourseCopyPermission(
	ctx context.Context,
	actor Actor,
	articleID uuid.UUID,
	requestedVersionID, targetPartnerID *uuid.UUID,
) (ArticleCourseCopyPermission, error) {
	partnerID, err := resolveTargetPartner(actor, targetPartnerID)
	if err != nil {
		return ArticleCourseCopyPermission{}, err
	}
	queries := db.New(s.pool)
	article, err := queries.GetArticle(ctx, db.GetArticleParams{CompanyID: actor.CompanyID, ID: articleID})
	if err != nil {
		if isNoRows(err) {
			return ArticleCourseCopyPermission{}, notFound("Статья")
		}
		return ArticleCourseCopyPermission{}, internal("Не удалось проверить статью", err)
	}
	permission := ArticleCourseCopyPermission{ReusePolicy: article.PartnerReusePolicy}
	if article.Status != "published" {
		permission.DenialReason = "Статья не опубликована"
		return permission, nil
	}
	if _, err = queries.GetPartnerArticle(ctx, db.GetPartnerArticleParams{
		CompanyID: actor.CompanyID, ID: articleID, PartnerID: partnerID,
	}); err != nil {
		if isNoRows(err) {
			permission.DenialReason = "Партнёру не предоставлен доступ к статье"
			return permission, nil
		}
		return ArticleCourseCopyPermission{}, internal("Не удалось проверить доступ к статье", err)
	}
	permission.CanRead = true
	if article.PartnerReusePolicy != "copy_allowed" {
		permission.DenialReason = "Копирование статьи запрещено"
		return permission, nil
	}
	resolvedID, err := s.ensureCurrentArticleVersion(ctx, queries, actor.CompanyID, articleID)
	if err != nil {
		return ArticleCourseCopyPermission{}, err
	}
	permission.ResolvedArticleVersionID = &resolvedID
	if requestedVersionID != nil && *requestedVersionID != resolvedID {
		permission.DenialReason = "Копировать можно только текущую опубликованную версию статьи"
		return permission, nil
	}
	permission.CanCopy = true
	return permission, nil
}

func (s *Service) GetArticleSnapshotForCourseCopy(
	ctx context.Context,
	actor Actor,
	articleID uuid.UUID,
	requestedVersionID, targetPartnerID *uuid.UUID,
) (ArticleSnapshotForCourseCopy, error) {
	partnerID, err := resolveTargetPartner(actor, targetPartnerID)
	if err != nil {
		return ArticleSnapshotForCourseCopy{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ArticleSnapshotForCourseCopy{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	resolvedID, err := s.ensureCurrentArticleVersion(ctx, queries, actor.CompanyID, articleID)
	if err != nil {
		return ArticleSnapshotForCourseCopy{}, err
	}
	if requestedVersionID != nil && *requestedVersionID != resolvedID {
		return ArticleSnapshotForCourseCopy{}, forbidden("Копировать можно только текущую опубликованную версию статьи")
	}
	row, err := queries.GetArticleSnapshotForCourseCopy(ctx, db.GetArticleSnapshotForCourseCopyParams{
		CompanyID: actor.CompanyID, ArticleID: articleID, PartnerID: partnerID,
	})
	if err != nil {
		if isNoRows(err) {
			return ArticleSnapshotForCourseCopy{}, forbidden("Статья недоступна для копирования")
		}
		return ArticleSnapshotForCourseCopy{}, internal("Не удалось получить снимок статьи", err)
	}
	if err = richtext.Validate(row.Content); err != nil {
		return ArticleSnapshotForCourseCopy{}, internal("Опубликованная статья содержит некорректный TipTap JSON", err)
	}
	hashBytes := sha256.Sum256(row.Content)
	contentHash := hex.EncodeToString(hashBytes[:])
	attachments := make([]ArticleSnapshotAttachment, 0)
	fileIDs := make([]uuid.UUID, 0)
	for _, rawID := range richtext.FileIDs(row.Content) {
		fileID, parseErr := uuid.Parse(rawID)
		if parseErr != nil {
			return ArticleSnapshotForCourseCopy{}, internal("Статья содержит некорректную ссылку на файл", parseErr)
		}
		fileIDs = append(fileIDs, fileID)
		attachments = append(attachments, ArticleSnapshotAttachment{FileID: fileID})
	}
	idempotencyKey := fmt.Sprintf("snapshot:%s:%s", row.ArticleVersionID, partnerID)
	grantParams := db.GetArticleSnapshotReuseGrantParams{
		CompanyID: actor.CompanyID, ArticleID: articleID,
		ArticleVersionID: row.ArticleVersionID, PartnerID: partnerID,
		IdempotencyKey: idempotencyKey,
	}
	if _, getErr := queries.GetArticleSnapshotReuseGrant(ctx, grantParams); getErr != nil {
		if !isNoRows(getErr) {
			return ArticleSnapshotForCourseCopy{}, internal("Не удалось проверить выдачу снимка статьи", getErr)
		}
		if _, createErr := queries.CreateArticleSnapshotReuseGrant(ctx, db.CreateArticleSnapshotReuseGrantParams{
			ID: uuid.New(), CompanyID: actor.CompanyID, ArticleID: articleID,
			ArticleVersionID: row.ArticleVersionID, ArticleVersion: row.ArticleVersion,
			PartnerID: partnerID, RequestedByID: actor.UserID, IdempotencyKey: idempotencyKey,
			ContentHash: contentHash, SourceFileIds: fileIDs, GrantedAt: s.now().UTC(),
		}); createErr != nil && !isNoRows(createErr) {
			return ArticleSnapshotForCourseCopy{}, internal("Не удалось зафиксировать выдачу снимка статьи", createErr)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return ArticleSnapshotForCourseCopy{}, internal("Не удалось зафиксировать снимок статьи", err)
	}
	return ArticleSnapshotForCourseCopy{
		ArticleID: row.ArticleID, ArticleVersionID: row.ArticleVersionID,
		Version: row.ArticleVersion, Title: row.Title,
		Content: append([]byte(nil), row.Content...), Attachments: attachments,
		ContentHash: contentHash, CapturedAt: s.now().UTC(),
	}, nil
}

func (s *Service) ensureCurrentArticleVersion(
	ctx context.Context,
	queries *db.Queries,
	companyID, articleID uuid.UUID,
) (uuid.UUID, error) {
	if err := queries.EnsureCurrentArticleVersion(ctx, db.EnsureCurrentArticleVersionParams{
		VersionID: uuid.New(), CompanyID: companyID, ArticleID: articleID,
	}); err != nil {
		return uuid.Nil, internal("Не удалось зафиксировать текущую версию статьи", err)
	}
	row, err := queries.GetArticleSnapshotForCourseCopy(ctx, db.GetArticleSnapshotForCourseCopyParams{
		CompanyID: companyID, ArticleID: articleID, PartnerID: uuid.Nil,
	})
	if err == nil {
		return row.ArticleVersionID, nil
	}
	// Access and reuse may intentionally deny the snapshot. Resolve the exact
	// version through the ordinary version list without bypassing that policy.
	versions, listErr := queries.ListArticleVersions(ctx, db.ListArticleVersionsParams{
		CompanyID: companyID, ArticleID: articleID,
	})
	if listErr != nil {
		return uuid.Nil, internal("Не удалось определить текущую версию статьи", listErr)
	}
	article, getErr := queries.GetArticle(ctx, db.GetArticleParams{CompanyID: companyID, ID: articleID})
	if getErr != nil {
		if isNoRows(getErr) {
			return uuid.Nil, notFound("Статья")
		}
		return uuid.Nil, internal("Не удалось получить статью", getErr)
	}
	for _, version := range versions {
		if version.Version == article.Version {
			return version.ID, nil
		}
	}
	return uuid.Nil, internal("Текущая версия статьи не найдена", pgx.ErrNoRows)
}

func resolveTargetPartner(actor Actor, requested *uuid.UUID) (uuid.UUID, error) {
	if actor.Role == "partner" {
		if requested != nil && *requested != actor.UserID {
			return uuid.Nil, forbidden("Нельзя запрашивать статью для другого партнёра")
		}
		return actor.UserID, nil
	}
	if !domainaccess.CanManage(actor.subject()) {
		return uuid.Nil, forbidden("Недостаточно прав для копирования статьи")
	}
	if requested == nil || *requested == uuid.Nil {
		return uuid.Nil, validation("Укажите партнёра, для которого копируется статья")
	}
	return *requested, nil
}

func validatePartnerAccess(access PartnerAccessSettings) error {
	switch access.Mode {
	case "none", "all":
		if len(access.PartnerIDs) != 0 {
			return validation("Список партнёров допустим только для выбранного доступа")
		}
	case "selected":
		if len(access.PartnerIDs) == 0 {
			return validation("Выберите хотя бы одного партнёра")
		}
		seen := make(map[uuid.UUID]struct{}, len(access.PartnerIDs))
		for _, id := range access.PartnerIDs {
			if id == uuid.Nil {
				return validation("Некорректный идентификатор партнёра")
			}
			if _, exists := seen[id]; exists {
				return validation("Партнёр указан несколько раз")
			}
			seen[id] = struct{}{}
		}
	default:
		return validation("Некорректный режим доступа партнёров")
	}
	return nil
}

func applyArticlePartnerPolicyFields(
	ctx context.Context,
	queries *db.Queries,
	companyID, articleID, actorID uuid.UUID,
	access *PartnerAccessSettings,
	reusePolicy *string,
	updatedAt time.Time,
) error {
	if access != nil {
		if _, err := queries.UpdateArticlePartnerAccessMode(ctx, db.UpdateArticlePartnerAccessModeParams{
			PartnerAccessMode: access.Mode, UpdatedAt: updatedAt,
			CompanyID: companyID, ID: articleID,
		}); err != nil {
			return internal("Не удалось обновить доступ партнёров", err)
		}
		if _, err := queries.DeleteArticlePartnerAccessGrants(ctx, db.DeleteArticlePartnerAccessGrantsParams{
			CompanyID: companyID, ArticleID: articleID,
		}); err != nil {
			return internal("Не удалось обновить список партнёров", err)
		}
		if access.Mode == "selected" {
			if _, err := queries.CreateArticlePartnerAccessGrants(ctx, db.CreateArticlePartnerAccessGrantsParams{
				GrantedByID: actorID, GrantedAt: updatedAt, PartnerIds: access.PartnerIDs,
				CompanyID: companyID, ArticleID: articleID,
			}); err != nil {
				return internal("Не удалось предоставить доступ партнёрам", err)
			}
		}
	}
	if reusePolicy != nil {
		if _, err := queries.UpdateArticlePartnerReusePolicy(ctx, db.UpdateArticlePartnerReusePolicyParams{
			PartnerReusePolicy: *reusePolicy, UpdatedAt: updatedAt,
			CompanyID: companyID, ID: articleID,
		}); err != nil {
			return internal("Не удалось обновить повторное использование статьи", err)
		}
	}
	return nil
}

func articleFromPartnerRow(value db.GetPartnerArticleRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText:               value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		PartnerAccessMode: value.PartnerAccessMode, PartnerReusePolicy: value.PartnerReusePolicy,
	})
}

func articleFromPartnerListRow(value db.ListPartnerArticlesRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText:               value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		PartnerAccessMode: value.PartnerAccessMode, PartnerReusePolicy: value.PartnerReusePolicy,
	})
}

func articleFromPartnerSearchRow(value db.SearchPartnerArticlesRow) Article {
	return articleFromDBRow(articleDBFields{
		ID: value.ID, CompanyID: value.CompanyID, SectionID: value.SectionID,
		Title: value.Title, Content: value.Content, Status: value.Status,
		AuthorID: value.AuthorID, Version: value.Version,
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		PlainText:               value.PlainText, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		PartnerAccessMode: value.PartnerAccessMode, PartnerReusePolicy: value.PartnerReusePolicy,
	})
}
