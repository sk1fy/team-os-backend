package application

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	domainauth "github.com/sk1fy/team-os-backend/services/academy/internal/domain/authorization"
	domaincourse "github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	domainversion "github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
	domaintemplate "github.com/sk1fy/team-os-backend/services/academy/internal/domain/template"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type CreateCourseTemplateInput struct {
	Title       string
	Description *string
	CoverFileID *uuid.UUID
	Sequential  bool
	Content     *CourseTemplateDraftContentInput
}

type UpdateCourseTemplateDraftInput struct {
	TemplateID  uuid.UUID
	Title       *string
	Description *string
	CoverFileID *uuid.UUID
	Sequential  *bool
	Content     *CourseTemplateDraftContentInput
}

func (s *Service) SeedSystemCourseTemplates(ctx context.Context, companyID uuid.UUID) error {
	if companyID == uuid.Nil {
		return validation("Некорректный идентификатор компании")
	}
	if _, err := db.New(s.pool).SeedSystemCourseTemplates(ctx, companyID); err != nil {
		return internal("Не удалось создать системные шаблоны курсов", err)
	}
	return nil
}

func (s *Service) GetCourseTemplates(
	ctx context.Context,
	actor Actor,
	templateType, lifecycle *string,
) ([]CourseTemplate, error) {
	if !domainauth.CanInstantiateTemplate(authorizationActor(actor)) && !actor.canManage() {
		return nil, forbidden("Недостаточно прав для просмотра шаблонов курсов")
	}
	if templateType != nil && *templateType != "system" && *templateType != "company" {
		return nil, validation("Некорректный тип шаблона курса")
	}
	if lifecycle != nil && *lifecycle != "active" && *lifecycle != "archived" {
		return nil, validation("Некорректное состояние шаблона курса")
	}
	queries := db.New(s.pool)
	if _, err := queries.SeedSystemCourseTemplates(ctx, actor.CompanyID); err != nil {
		return nil, internal("Не удалось подготовить системные шаблоны", err)
	}
	rows, err := queries.ListCourseTemplates(ctx, db.ListCourseTemplatesParams{
		CompanyID: actor.CompanyID, TemplateType: nullText(templateType), LifecycleStatus: nullText(lifecycle),
	})
	if err != nil {
		return nil, internal("Не удалось получить шаблоны курсов", err)
	}
	result := make([]CourseTemplate, 0, len(rows))
	for _, row := range rows {
		if domainauth.CanViewCourseTemplate(authorizationActor(actor), templateSnapshotFromRow(row)) {
			result = append(result, courseTemplateFromRow(row))
		}
	}
	return result, nil
}

func (s *Service) GetCourseTemplate(
	ctx context.Context,
	actor Actor,
	templateID uuid.UUID,
	versionID *uuid.UUID,
) (CourseTemplateDetails, error) {
	queries := db.New(s.pool)
	if _, err := queries.SeedSystemCourseTemplates(ctx, actor.CompanyID); err != nil {
		return CourseTemplateDetails{}, internal("Не удалось подготовить системные шаблоны", err)
	}
	row, err := queries.GetCourseTemplate(ctx, db.GetCourseTemplateParams{CompanyID: actor.CompanyID, ID: templateID})
	if err != nil {
		if isNoRows(err) {
			return CourseTemplateDetails{}, notFound("Шаблон курса")
		}
		return CourseTemplateDetails{}, internal("Не удалось получить шаблон курса", err)
	}
	if !domainauth.CanViewCourseTemplate(authorizationActor(actor), templateSnapshotFromRow(row)) {
		return CourseTemplateDetails{}, notFound("Шаблон курса")
	}
	versions, err := queries.ListCourseTemplateVersions(ctx, db.ListCourseTemplateVersionsParams{
		CompanyID: actor.CompanyID, TemplateID: templateID,
	})
	if err != nil {
		return CourseTemplateDetails{}, internal("Не удалось получить версии шаблона", err)
	}
	visibleVersions := versions
	if !actor.canManage() {
		visibleVersions = make([]db.CourseTemplateVersion, 0, len(versions))
		for _, version := range versions {
			if version.Status == "published" {
				visibleVersions = append(visibleVersions, version)
			}
		}
	}
	result := CourseTemplateDetails{
		Template: courseTemplateFromRow(row), Versions: courseTemplateVersionsFromRows(visibleVersions),
	}
	if versionID == nil {
		return result, nil
	}
	selected, err := queries.GetCourseTemplateVersion(ctx, db.GetCourseTemplateVersionParams{
		CompanyID: actor.CompanyID, ID: *versionID,
	})
	if err != nil || selected.TemplateID != templateID || (!actor.canManage() && selected.Status != "published") {
		if err == nil || isNoRows(err) {
			return CourseTemplateDetails{}, notFound("Версия шаблона")
		}
		return CourseTemplateDetails{}, internal("Не удалось получить версию шаблона", err)
	}
	details, err := s.loadCourseTemplateVersionDetails(ctx, queries, selected)
	if err != nil {
		return CourseTemplateDetails{}, err
	}
	result.SelectedVersion = &details
	return result, nil
}

func (s *Service) CreateCourseTemplate(
	ctx context.Context,
	actor Actor,
	input CreateCourseTemplateInput,
) (CourseTemplate, CourseTemplateVersion, error) {
	if !domainauth.CanCreateCompanyTemplate(authorizationActor(actor)) {
		return CourseTemplate{}, CourseTemplateVersion{}, forbidden("Недостаточно прав для создания шаблона курса")
	}
	title, err := requiredText(input.Title, "Укажите название шаблона курса")
	if err != nil {
		return CourseTemplate{}, CourseTemplateVersion{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseTemplate{}, CourseTemplateVersion{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	now := s.now().UTC()
	templateID, versionID := uuid.New(), uuid.New()
	root, err := domaintemplate.NewCompany(
		domaintemplate.ID(templateID.String()), domaintemplate.ID(actor.CompanyID.String()),
		domaintemplate.ID(actor.UserID.String()), now,
	)
	if err != nil {
		return CourseTemplate{}, CourseTemplateVersion{}, validation(err.Error())
	}
	draft, err := domaintemplate.NewDraft(root.Snapshot(), domaintemplate.NewDraftParams{
		ID: domaintemplate.ID(versionID.String()), Number: 1,
		Definition: domainversion.Definition{
			Title: title, Description: input.Description,
			CoverFileID: optionalDomainID(input.CoverFileID), Sequential: input.Sequential,
		},
		CreatedByID: domaintemplate.ID(actor.UserID.String()), CreatedAt: now,
	})
	if err != nil || root.AttachDraft(draft.Snapshot()) != nil {
		return CourseTemplate{}, CourseTemplateVersion{}, validation("Не удалось подготовить черновик шаблона")
	}
	rootRow, err := queries.CreateCompanyCourseTemplate(ctx, db.CreateCompanyCourseTemplateParams{
		ID: templateID, CompanyID: actor.CompanyID, CreatedByID: actor.UserID, CreatedAt: now,
	})
	if err != nil {
		return CourseTemplate{}, CourseTemplateVersion{}, internal("Не удалось создать шаблон курса", err)
	}
	versionRow, err := queries.CreateCourseTemplateVersion(ctx, db.CreateCourseTemplateVersionParams{
		ID: versionID, Number: 1, Title: title, Description: nullText(input.Description),
		CoverFileID: nullUUID(input.CoverFileID), Sequential: input.Sequential,
		CreatedByID: actor.UserID, CreatedAt: now, CompanyID: actor.CompanyID, TemplateID: templateID,
	})
	if err != nil {
		return CourseTemplate{}, CourseTemplateVersion{}, internal("Не удалось создать черновик шаблона", err)
	}
	if affected, setErr := queries.SetCourseTemplateCurrentDraftVersion(ctx, db.SetCourseTemplateCurrentDraftVersionParams{
		CompanyID: actor.CompanyID, TemplateID: templateID, VersionID: versionID,
	}); setErr != nil || affected != 1 {
		return CourseTemplate{}, CourseTemplateVersion{}, internal("Не удалось связать черновик с шаблоном", setErr)
	}
	if input.Content != nil {
		if err = s.replaceCourseTemplateDraftContent(ctx, queries, actor, versionRow, *input.Content); err != nil {
			return CourseTemplate{}, CourseTemplateVersion{}, err
		}
	}
	if err = s.auditTemplate(ctx, queries, actor, "course_template_created", templateID, nil, rootRow); err != nil {
		return CourseTemplate{}, CourseTemplateVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseTemplate{}, CourseTemplateVersion{}, internal("Не удалось сохранить шаблон курса", err)
	}
	createdRoot := courseTemplateFromRow(rootRow)
	createdRoot.CurrentDraftVersionID = &versionID
	return createdRoot, courseTemplateVersionFromRow(versionRow), nil
}

func (s *Service) UpdateCourseTemplateDraft(
	ctx context.Context,
	actor Actor,
	input UpdateCourseTemplateDraftInput,
) (CourseTemplateVersionDetails, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	rootRow, err := queries.GetCourseTemplateForUpdate(ctx, db.GetCourseTemplateForUpdateParams{
		CompanyID: actor.CompanyID, ID: input.TemplateID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseTemplateVersionDetails{}, notFound("Шаблон курса")
		}
		return CourseTemplateVersionDetails{}, internal("Не удалось получить шаблон курса", err)
	}
	if !domainauth.CanEditCompanyTemplate(authorizationActor(actor), templateSnapshotFromRow(rootRow)) {
		return CourseTemplateVersionDetails{}, forbidden("Недостаточно прав для изменения шаблона курса")
	}
	draftRow, err := queries.GetCurrentDraftCourseTemplateVersion(ctx, db.GetCurrentDraftCourseTemplateVersionParams{
		CompanyID: actor.CompanyID, TemplateID: input.TemplateID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseTemplateVersionDetails{}, notFound("Черновик шаблона")
		}
		return CourseTemplateVersionDetails{}, internal("Не удалось получить черновик шаблона", err)
	}
	title, description, cover, sequential := draftRow.Title, textPointer(draftRow.Description), nullUUIDPointer(draftRow.CoverFileID), draftRow.Sequential
	if input.Title != nil {
		title, err = requiredText(*input.Title, "Укажите название шаблона курса")
		if err != nil {
			return CourseTemplateVersionDetails{}, err
		}
	}
	if input.Description != nil {
		description = input.Description
	}
	if input.CoverFileID != nil {
		cover = input.CoverFileID
	}
	if input.Sequential != nil {
		sequential = *input.Sequential
	}
	updated, err := queries.UpdateDraftCourseTemplateVersion(ctx, db.UpdateDraftCourseTemplateVersionParams{
		Title: title, Description: nullText(description), CoverFileID: nullUUID(cover), Sequential: sequential,
		CompanyID: actor.CompanyID, ID: draftRow.ID,
	})
	if err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось обновить черновик шаблона", err)
	}
	if input.Content != nil {
		if err = s.replaceCourseTemplateDraftContent(ctx, queries, actor, updated, *input.Content); err != nil {
			return CourseTemplateVersionDetails{}, err
		}
	}
	details, err := s.loadCourseTemplateVersionDetails(ctx, queries, updated)
	if err != nil {
		return CourseTemplateVersionDetails{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось сохранить черновик шаблона", err)
	}
	return details, nil
}

func (s *Service) CreateCourseTemplateDraft(
	ctx context.Context,
	actor Actor,
	templateID uuid.UUID,
) (CourseTemplateVersionDetails, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	rootRow, err := queries.GetCourseTemplateForUpdate(ctx, db.GetCourseTemplateForUpdateParams{CompanyID: actor.CompanyID, ID: templateID})
	if err != nil {
		if isNoRows(err) {
			return CourseTemplateVersionDetails{}, notFound("Шаблон курса")
		}
		return CourseTemplateVersionDetails{}, internal("Не удалось получить шаблон курса", err)
	}
	if !domainauth.CanEditCompanyTemplate(authorizationActor(actor), templateSnapshotFromRow(rootRow)) {
		return CourseTemplateVersionDetails{}, forbidden("Недостаточно прав для изменения шаблона курса")
	}
	if rootRow.CurrentDraftVersionID.Valid {
		return CourseTemplateVersionDetails{}, conflict("У шаблона уже есть черновик")
	}
	if !rootRow.LatestPublishedVersionID.Valid {
		return CourseTemplateVersionDetails{}, conflict("У шаблона нет опубликованной версии")
	}
	now := s.now().UTC()
	draftRow, err := queries.CreateNextDraftCourseTemplateVersion(ctx, db.CreateNextDraftCourseTemplateVersionParams{
		ID: uuid.New(), CreatedByID: actor.UserID, CreatedAt: now,
		CompanyID: actor.CompanyID, SourceVersionID: rootRow.LatestPublishedVersionID.UUID,
	})
	if err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось создать новую версию шаблона", err)
	}
	clone := db.CloneCourseTemplateVersionSectionsParams{
		TargetVersionID: draftRow.ID, CompanyID: actor.CompanyID, SourceVersionID: rootRow.LatestPublishedVersionID.UUID,
	}
	if _, err = queries.CloneCourseTemplateVersionSections(ctx, clone); err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось скопировать разделы шаблона", err)
	}
	if _, err = queries.CloneCourseTemplateVersionLessons(ctx, db.CloneCourseTemplateVersionLessonsParams(clone)); err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось скопировать уроки шаблона", err)
	}
	if _, err = queries.CloneCourseTemplateVersionQuizzes(ctx, db.CloneCourseTemplateVersionQuizzesParams(clone)); err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось скопировать тесты шаблона", err)
	}
	if affected, setErr := queries.SetCourseTemplateCurrentDraftVersion(ctx, db.SetCourseTemplateCurrentDraftVersionParams{
		CompanyID: actor.CompanyID, TemplateID: templateID, VersionID: draftRow.ID,
	}); setErr != nil || affected != 1 {
		return CourseTemplateVersionDetails{}, internal("Не удалось связать новую версию с шаблоном", setErr)
	}
	details, err := s.loadCourseTemplateVersionDetails(ctx, queries, draftRow)
	if err != nil {
		return CourseTemplateVersionDetails{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось сохранить новую версию шаблона", err)
	}
	return details, nil
}

func (s *Service) PublishCourseTemplateVersion(
	ctx context.Context,
	actor Actor,
	templateID uuid.UUID,
	idempotencyKey string,
) (CourseTemplateVersion, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return CourseTemplateVersion{}, validation("Укажите ключ идемпотентности")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseTemplateVersion{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	previous, err := queries.GetCourseTemplatePublishIdempotency(ctx, db.GetCourseTemplatePublishIdempotencyParams{
		CompanyID: actor.CompanyID, TemplateID: templateID, IdempotencyKey: idempotencyKey,
	})
	if err == nil {
		row, getErr := queries.GetCourseTemplateVersion(ctx, db.GetCourseTemplateVersionParams{CompanyID: actor.CompanyID, ID: previous.TemplateVersionID})
		if getErr != nil {
			return CourseTemplateVersion{}, internal("Не удалось получить результат публикации шаблона", getErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return CourseTemplateVersion{}, internal("Не удалось завершить повторную публикацию", commitErr)
		}
		return courseTemplateVersionFromRow(row), nil
	}
	if !isNoRows(err) {
		return CourseTemplateVersion{}, internal("Не удалось проверить повторную публикацию", err)
	}
	rootRow, err := queries.GetCourseTemplateForUpdate(ctx, db.GetCourseTemplateForUpdateParams{CompanyID: actor.CompanyID, ID: templateID})
	if err != nil {
		if isNoRows(err) {
			return CourseTemplateVersion{}, notFound("Шаблон курса")
		}
		return CourseTemplateVersion{}, internal("Не удалось получить шаблон курса", err)
	}
	rootSnapshot := templateSnapshotFromRow(rootRow)
	if !domainauth.CanPublishCompanyTemplate(authorizationActor(actor), rootSnapshot) {
		return CourseTemplateVersion{}, forbidden("Недостаточно прав для публикации шаблона курса")
	}
	draftRow, err := queries.GetCurrentDraftCourseTemplateVersion(ctx, db.GetCurrentDraftCourseTemplateVersionParams{
		CompanyID: actor.CompanyID, TemplateID: templateID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseTemplateVersion{}, notFound("Черновик шаблона")
		}
		return CourseTemplateVersion{}, internal("Не удалось получить черновик шаблона", err)
	}
	details, err := s.loadCourseTemplateVersionDetails(ctx, queries, draftRow)
	if err != nil {
		return CourseTemplateVersion{}, err
	}
	domainDraft, err := domainTemplateVersion(details)
	if err != nil {
		return CourseTemplateVersion{}, validation(err.Error())
	}
	now := s.now().UTC()
	if err = domainDraft.Publish(rootSnapshot, domaintemplate.PublishParams{
		ActorID: domaintemplate.ID(actor.UserID.String()), At: now,
	}, domainversion.PublicationValidators{
		RichText: richtext.Validate, FileAvailable: func(domainversion.ID) bool { return true },
	}); err != nil {
		return CourseTemplateVersion{}, validation(err.Error())
	}
	snapshot := domainDraft.Snapshot()
	publishedRow, err := queries.PublishCourseTemplateVersion(ctx, db.PublishCourseTemplateVersionParams{
		PublishedByID: nullUUID(&actor.UserID), PublishedAt: pgtype.Timestamptz{Time: now, Valid: true},
		ContentHash: pgtype.Text{String: snapshot.ContentHash, Valid: true},
		CompanyID:   actor.CompanyID, TemplateID: templateID, ID: draftRow.ID,
	})
	if err != nil {
		return CourseTemplateVersion{}, internal("Не удалось опубликовать шаблон курса", err)
	}
	if rootRow.LatestPublishedVersionID.Valid {
		if _, err = queries.RetireCourseTemplateVersion(ctx, db.RetireCourseTemplateVersionParams{
			CompanyID: actor.CompanyID, ID: rootRow.LatestPublishedVersionID.UUID,
		}); err != nil && !isNoRows(err) {
			return CourseTemplateVersion{}, internal("Не удалось архивировать предыдущую версию шаблона", err)
		}
	}
	if affected, setErr := queries.SetCourseTemplatePublishedVersionPointers(ctx, db.SetCourseTemplatePublishedVersionPointersParams{
		CompanyID: actor.CompanyID, TemplateID: templateID, VersionID: draftRow.ID,
	}); setErr != nil || affected != 1 {
		return CourseTemplateVersion{}, internal("Не удалось обновить опубликованную версию шаблона", setErr)
	}
	if _, err = queries.CreateCourseTemplatePublishIdempotency(ctx, db.CreateCourseTemplatePublishIdempotencyParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, TemplateID: templateID,
		IdempotencyKey: idempotencyKey, TemplateVersionID: draftRow.ID, CreatedAt: now,
	}); err != nil {
		return CourseTemplateVersion{}, internal("Не удалось зафиксировать публикацию шаблона", err)
	}
	published := courseTemplateVersionFromRow(publishedRow)
	if err = s.emitTemplateVersionPublished(ctx, queries, actor, courseTemplateFromRow(rootRow), published); err != nil {
		return CourseTemplateVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseTemplateVersion{}, internal("Не удалось сохранить публикацию шаблона", err)
	}
	return published, nil
}

func (s *Service) ArchiveCourseTemplate(ctx context.Context, actor Actor, templateID uuid.UUID) (CourseTemplate, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseTemplate{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	before, err := queries.GetCourseTemplateForUpdate(ctx, db.GetCourseTemplateForUpdateParams{CompanyID: actor.CompanyID, ID: templateID})
	if err != nil {
		if isNoRows(err) {
			return CourseTemplate{}, notFound("Шаблон курса")
		}
		return CourseTemplate{}, internal("Не удалось получить шаблон курса", err)
	}
	root, err := domaintemplate.RehydrateTemplate(templateSnapshotFromRow(before))
	if err != nil {
		return CourseTemplate{}, validation(err.Error())
	}
	if !domainauth.CanArchiveCompanyTemplate(authorizationActor(actor), root.Snapshot()) {
		return CourseTemplate{}, forbidden("Недостаточно прав для архивации шаблона курса")
	}
	if err = root.Archive(); err != nil {
		return CourseTemplate{}, conflict(err.Error())
	}
	now := s.now().UTC()
	row, err := queries.ArchiveCompanyCourseTemplate(ctx, db.ArchiveCompanyCourseTemplateParams{
		ArchivedByID: nullUUID(&actor.UserID), ArchivedAt: pgtype.Timestamptz{Time: now, Valid: true},
		CompanyID: actor.CompanyID, ID: templateID,
	})
	if err != nil {
		return CourseTemplate{}, internal("Не удалось архивировать шаблон курса", err)
	}
	if err = s.auditTemplate(ctx, queries, actor, "course_template_archived", templateID, &before, row); err != nil {
		return CourseTemplate{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseTemplate{}, internal("Не удалось сохранить архивацию шаблона", err)
	}
	return courseTemplateFromRow(row), nil
}

func (s *Service) InstantiateCourseTemplateVersion(
	ctx context.Context,
	actor Actor,
	versionID uuid.UUID,
	idempotencyKey string,
) (CourseTemplateInstantiationResult, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return CourseTemplateInstantiationResult{}, validation("Укажите ключ идемпотентности")
	}
	ownerType, ownerID, ok := domainauth.TemplateInstantiationOwner(authorizationActor(actor))
	if !ok {
		return CourseTemplateInstantiationResult{}, forbidden("Недостаточно прав для применения шаблона курса")
	}
	var ownerUUID *uuid.UUID
	if ownerID != nil {
		parsed, err := uuid.Parse(string(*ownerID))
		if err != nil {
			return CourseTemplateInstantiationResult{}, internal("Не удалось определить владельца курса", err)
		}
		ownerUUID = &parsed
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	locked, err := queries.LockPublishedCourseTemplateVersionForInstantiate(ctx, db.LockPublishedCourseTemplateVersionForInstantiateParams{
		CompanyID: actor.CompanyID, TemplateVersionID: versionID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseTemplateInstantiationResult{}, notFound("Опубликованная версия шаблона")
		}
		return CourseTemplateInstantiationResult{}, internal("Не удалось получить версию шаблона", err)
	}
	idemParams := db.GetCourseTemplateInstantiationIdempotencyParams{
		CompanyID: actor.CompanyID, SourceTemplateID: locked.TemplateID, SourceTemplateVersionID: versionID,
		TargetOwnerType: string(ownerType), TargetOwnerUserID: nullUUID(ownerUUID), IdempotencyKey: idempotencyKey,
	}
	previous, previousErr := queries.GetCourseTemplateInstantiationIdempotency(ctx, idemParams)
	if previousErr == nil {
		result, getErr := loadTemplateInstantiationResult(ctx, queries, actor.CompanyID, previous.TargetCourseID, previous.TargetCourseVersionID)
		if getErr != nil {
			return CourseTemplateInstantiationResult{}, getErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return CourseTemplateInstantiationResult{}, internal("Не удалось завершить повторное применение шаблона", commitErr)
		}
		return result, nil
	}
	if !isNoRows(previousErr) {
		return CourseTemplateInstantiationResult{}, internal("Не удалось проверить повторное применение шаблона", previousErr)
	}
	rootRow, err := queries.GetCourseTemplate(ctx, db.GetCourseTemplateParams{CompanyID: actor.CompanyID, ID: locked.TemplateID})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось получить шаблон курса", err)
	}
	versionRow, err := queries.GetCourseTemplateVersion(ctx, db.GetCourseTemplateVersionParams{CompanyID: actor.CompanyID, ID: versionID})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось получить версию шаблона", err)
	}
	details, err := s.loadCourseTemplateVersionDetails(ctx, queries, versionRow)
	if err != nil {
		return CourseTemplateInstantiationResult{}, err
	}
	domainSource, err := domainTemplateVersion(details)
	if err != nil {
		return CourseTemplateInstantiationResult{}, validation(err.Error())
	}
	if !domainauth.CanInstantiateTemplateVersion(authorizationActor(actor), templateSnapshotFromRow(rootRow), domainSource.Snapshot()) {
		return CourseTemplateInstantiationResult{}, forbidden("Недостаточно прав для применения этой версии шаблона")
	}
	now := s.now().UTC()
	courseID, draftID := uuid.New(), uuid.New()
	plan, err := domaintemplate.Instantiate(domaintemplate.InstantiationParams{
		SourceTemplate: templateSnapshotFromRow(rootRow), SourceVersion: domainSource.Snapshot(),
		DestinationCourseID: domaincourse.ID(courseID.String()), DestinationDraftID: domainversion.ID(draftID.String()),
		TargetOwnerType: ownerType, TargetOwnerUserID: ownerID,
		CreatedByID: domaincourse.ID(actor.UserID.String()), CreatedAt: now,
		MapID: func(domaintemplate.EntityKind, domainversion.ID) domainversion.ID {
			return domainversion.ID(uuid.NewString())
		},
	})
	if err != nil {
		return CourseTemplateInstantiationResult{}, validation(err.Error())
	}
	definition := plan.Draft.Snapshot().Definition
	courseRow, err := queries.CreateOwnedCourse(ctx, db.CreateOwnedCourseParams{
		ID: courseID, CompanyID: actor.CompanyID, Title: definition.Title,
		Description: nullText(definition.Description), Status: "draft", AuthorID: actor.UserID,
		Sequential: definition.Sequential, CreatedAt: now, Visibility: "restricted",
		OwnerType: string(ownerType), OwnerUserID: nullUUID(ownerUUID), CreatedByID: nullUUID(&actor.UserID),
	})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось создать курс из шаблона", err)
	}
	draftRow, err := queries.CreateCourseVersion(ctx, db.CreateCourseVersionParams{
		ID: draftID, CompanyID: actor.CompanyID, CourseID: courseID, Number: 1,
		Title: definition.Title, Description: nullText(definition.Description), CoverFileID: nullUUID(details.Version.CoverFileID),
		Sequential: definition.Sequential, CreatedByID: actor.UserID, CreatedAt: now,
	})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось создать черновик курса из шаблона", err)
	}
	if affected, setErr := queries.SetCourseCurrentDraftVersion(ctx, db.SetCourseCurrentDraftVersionParams{
		UpdatedAt: now, CompanyID: actor.CompanyID, CourseID: courseID, VersionID: draftID,
	}); setErr != nil || affected != 1 {
		return CourseTemplateInstantiationResult{}, internal("Не удалось связать черновик с курсом", setErr)
	}
	instantiate := db.InstantiateCourseTemplateSectionsParams{
		TargetCourseVersionID: draftID, CompanyID: actor.CompanyID, SourceTemplateVersionID: versionID,
	}
	if _, err = queries.InstantiateCourseTemplateSections(ctx, instantiate); err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось скопировать разделы шаблона", err)
	}
	if _, err = queries.InstantiateCourseTemplateLessons(ctx, db.InstantiateCourseTemplateLessonsParams(instantiate)); err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось скопировать уроки шаблона", err)
	}
	if _, err = queries.InstantiateCourseTemplateQuizzes(ctx, db.InstantiateCourseTemplateQuizzesParams(instantiate)); err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось скопировать тесты шаблона", err)
	}
	originID := uuid.New()
	originRow, err := queries.CreateTemplateCourseOrigin(ctx, db.CreateTemplateCourseOriginParams{
		ID: originID, OriginType: string(plan.Origin.Type), SourceTemplateID: nullUUID(&locked.TemplateID),
		SourceTemplateVersionID: nullUUID(&versionID), InstantiatedByID: actor.UserID,
		InstantiatedAt: now, CompanyID: actor.CompanyID, TargetCourseID: courseID,
	})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось сохранить происхождение курса", err)
	}
	if _, err = queries.CreateCourseTemplateInstantiationIdempotency(ctx, db.CreateCourseTemplateInstantiationIdempotencyParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, SourceTemplateID: locked.TemplateID,
		SourceTemplateVersionID: versionID, TargetOwnerType: string(ownerType), TargetOwnerUserID: nullUUID(ownerUUID),
		IdempotencyKey: idempotencyKey, TargetCourseID: courseID, TargetCourseVersionID: draftID,
		OriginID: originID, InstantiatedByID: actor.UserID, InstantiatedAt: now,
	}); err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось зафиксировать применение шаблона", err)
	}
	fileIDs, err := queries.ListCourseTemplateVersionFileIDs(ctx, db.ListCourseTemplateVersionFileIDsParams{
		CompanyID: actor.CompanyID, TemplateVersionID: versionID,
	})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось определить файлы шаблона", err)
	}
	if len(fileIDs) > 0 {
		jobID := uuid.New()
		if _, err = queries.CreateFileCloneJob(ctx, db.CreateFileCloneJobParams{
			ID: jobID, CompanyID: actor.CompanyID, OperationType: "template_instantiate", AggregateID: courseID,
			IdempotencyKey:  "template:" + versionID.String() + ":" + idempotencyKey,
			SourceOwnerType: "template_version", SourceOwnerID: versionID,
			TargetOwnerType: "course_version", TargetOwnerID: draftID, CreatedAt: now,
		}); err != nil {
			return CourseTemplateInstantiationResult{}, internal("Не удалось создать задачу копирования файлов", err)
		}
		if _, err = queries.AddFileCloneJobItems(ctx, db.AddFileCloneJobItemsParams{
			UpdatedAt: now, SourceFileIds: fileIDs, CompanyID: actor.CompanyID, JobID: jobID,
		}); err != nil {
			return CourseTemplateInstantiationResult{}, internal("Не удалось сохранить файлы для копирования", err)
		}
	}
	result := CourseTemplateInstantiationResult{
		Course: courseFromRow(courseRow), Draft: courseVersionFromRow(draftRow), Origin: courseTemplateOriginFromRow(originRow),
	}
	result.Course.CurrentDraftVersionID = &draftID
	if err = s.auditCourse(ctx, queries, actor, "course_instantiated_from_template", nil, result.Course); err != nil {
		return CourseTemplateInstantiationResult{}, err
	}
	if err = s.emitTemplateInstantiated(ctx, queries, actor, courseTemplateFromRow(rootRow), details.Version, result); err != nil {
		return CourseTemplateInstantiationResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось сохранить курс из шаблона", err)
	}
	return result, nil
}

func loadTemplateInstantiationResult(
	ctx context.Context,
	queries *db.Queries,
	companyID, courseID, versionID uuid.UUID,
) (CourseTemplateInstantiationResult, error) {
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: companyID, ID: courseID})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось получить созданный курс", err)
	}
	versionRow, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: companyID, ID: versionID})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось получить созданный черновик", err)
	}
	originRow, err := queries.GetCourseOrigin(ctx, db.GetCourseOriginParams{CompanyID: companyID, TargetCourseID: courseID})
	if err != nil {
		return CourseTemplateInstantiationResult{}, internal("Не удалось получить происхождение курса", err)
	}
	return CourseTemplateInstantiationResult{
		Course: courseFromRow(courseRow), Draft: courseVersionFromRow(versionRow), Origin: courseTemplateOriginFromRow(originRow),
	}, nil
}

func (s *Service) auditTemplate(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	action string,
	templateID uuid.UUID,
	before *db.CourseTemplate,
	after db.CourseTemplate,
) error {
	var beforeState []byte
	if before != nil {
		beforeState, _ = json.Marshal(courseTemplateFromRow(*before))
	}
	afterState, _ := json.Marshal(courseTemplateFromRow(after))
	_, err := queries.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, ActorID: actor.UserID, ActorRole: actor.Role,
		Action: action, AggregateType: "course_template", AggregateID: templateID,
		BeforeState: beforeState, AfterState: afterState, CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return internal("Не удалось сохранить запись аудита шаблона", err)
	}
	return nil
}

func (s *Service) emitTemplateVersionPublished(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	template CourseTemplate,
	version CourseTemplateVersion,
) error {
	payload := &eventsv1.AcademyTemplateVersionPublishedPayload{
		TemplateId: template.ID.String(), TemplateVersionId: version.ID.String(),
		VersionNumber: uint32(max(0, version.Number)), TemplateType: templateTypeToEvent(template.Type),
		SystemTemplateKey: template.SystemTemplateKey, PublishedById: actor.UserID.String(),
	}
	if version.PublishedAt != nil {
		payload.PublishedAt = timestamppb.New(version.PublishedAt.UTC())
	}
	if version.ContentHash != nil {
		payload.ContentHash = *version.ContentHash
	}
	return s.emit(ctx, queries, actor.CompanyID, version.ID, actor.UserID,
		"teamos.academy.template_version.published.v1", payload)
}

func (s *Service) emitTemplateInstantiated(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	template CourseTemplate,
	version CourseTemplateVersion,
	result CourseTemplateInstantiationResult,
) error {
	payload := &eventsv1.AcademyTemplateInstantiatedPayload{
		TemplateId: template.ID.String(), TemplateVersionId: version.ID.String(),
		TemplateVersionNumber: uint32(max(0, version.Number)), TemplateType: templateTypeToEvent(template.Type),
		SystemTemplateKey: template.SystemTemplateKey, TargetCourseId: result.Course.ID.String(),
		TargetDraftVersionId: result.Draft.ID.String(), TargetOwnerType: courseOwnerTypeToEvent(result.Course.OwnerType),
		TargetOwnerUserId: optionalUUIDStringValue(result.Course.OwnerUserID),
		InstantiatedById:  actor.UserID.String(), InstantiatedAt: timestamppb.New(s.now().UTC()),
	}
	return s.emit(ctx, queries, actor.CompanyID, result.Course.ID, actor.UserID,
		"teamos.academy.template.instantiated.v1", payload)
}

func templateTypeToEvent(value string) eventsv1.AcademyCourseTemplateType {
	if value == "system" {
		return eventsv1.AcademyCourseTemplateType_ACADEMY_COURSE_TEMPLATE_TYPE_SYSTEM
	}
	if value == "company" {
		return eventsv1.AcademyCourseTemplateType_ACADEMY_COURSE_TEMPLATE_TYPE_COMPANY
	}
	return eventsv1.AcademyCourseTemplateType_ACADEMY_COURSE_TEMPLATE_TYPE_UNSPECIFIED
}
