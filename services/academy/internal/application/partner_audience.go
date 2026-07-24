package application

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

const (
	partnerAudienceNone      = "none"
	partnerAudienceAll       = "all_partners"
	partnerAudienceSelected  = "selected_partners"
	catalogDefaultPageSize   = 20
	catalogMaxPageSize       = 100
	catalogMaxSelectedActors = 500
)

// CatalogCard is a single published company course as shown in the catalog,
// already carrying its aggregates and the caller's enrollment state.
type CatalogCard struct {
	ID                  uuid.UUID
	Title               string
	Description         *string
	CoverURL            *string
	LessonCount         int32
	EstimatedMinutes    int32
	LatestVersionNumber int32
	Enrolled            bool
	EnrollmentID        *uuid.UUID
	ProgressPercent     *int32
}

// CatalogPage is a server-side paginated slice of the catalog.
type CatalogPage struct {
	Items    []CatalogCard
	Page     int32
	PageSize int32
	Total    int64
}

// CatalogQuery holds the catalog filters that are applied in SQL.
type CatalogQuery struct {
	Search   *string
	Page     int32
	PageSize int32
}

// CoursePartnerAudience is the resolved partner access configuration of a course.
type CoursePartnerAudience struct {
	Audience       string
	PartnerUserIDs []uuid.UUID
}

// SetCoursePartnerAudienceInput changes which partners may reach a company course.
type SetCoursePartnerAudienceInput struct {
	CourseID       uuid.UUID
	Audience       string
	PartnerUserIDs []uuid.UUID
}

// GetAcademyCatalog returns the published company-course catalog with filtering,
// pagination and aggregates resolved in SQL. Partners only see courses whose
// audience explicitly grants them access; the default is deny.
func (s *Service) GetAcademyCatalog(ctx context.Context, actor Actor, query CatalogQuery) (CatalogPage, error) {
	if !canReadAcademy(actor) {
		return CatalogPage{}, forbidden("Недостаточно прав для просмотра каталога")
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = catalogDefaultPageSize
	}
	if pageSize > catalogMaxPageSize {
		pageSize = catalogMaxPageSize
	}
	isPartner := actor.Role == "partner"
	var search pgtype.Text
	if query.Search != nil {
		if trimmed := strings.TrimSpace(*query.Search); trimmed != "" {
			search = pgtype.Text{String: trimmed, Valid: true}
		}
	}
	queries := db.New(s.pool)
	total, err := queries.CountCatalogCourses(ctx, db.CountCatalogCoursesParams{
		CompanyID: actor.CompanyID, IsPartner: isPartner, UserID: actor.UserID, Search: search,
	})
	if err != nil {
		return CatalogPage{}, internal("Не удалось получить каталог", err)
	}
	rows, err := queries.ListCatalogCourses(ctx, db.ListCatalogCoursesParams{
		CompanyID: actor.CompanyID, IsPartner: isPartner, UserID: actor.UserID,
		Search: search, PageOffset: (page - 1) * pageSize, PageLimit: pageSize,
	})
	if err != nil {
		return CatalogPage{}, internal("Не удалось получить каталог", err)
	}
	cards := make([]CatalogCard, 0, len(rows))
	courseIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		cards = append(cards, CatalogCard{
			ID: row.ID, Title: row.Title, Description: textPointer(row.Description),
			CoverURL: textPointer(row.CoverUrl), LessonCount: row.LessonCount,
			EstimatedMinutes: row.EstimatedMinutes, LatestVersionNumber: row.LatestVersionNumber,
		})
		courseIDs = append(courseIDs, row.ID)
	}
	if len(courseIDs) > 0 {
		enrollments, enrollErr := queries.ListUserCourseEnrollmentsForCatalog(ctx, db.ListUserCourseEnrollmentsForCatalogParams{
			CompanyID: actor.CompanyID, UserID: nullUUID(&actor.UserID), CourseIds: courseIDs,
		})
		if enrollErr != nil {
			return CatalogPage{}, internal("Не удалось получить прогресс каталога", enrollErr)
		}
		byCourse := make(map[uuid.UUID]db.ListUserCourseEnrollmentsForCatalogRow, len(enrollments))
		for _, enrollment := range enrollments {
			byCourse[enrollment.CourseID] = enrollment
		}
		for index := range cards {
			enrollment, ok := byCourse[cards[index].ID]
			if !ok {
				continue
			}
			enrollmentID, progress := enrollment.EnrollmentID, enrollment.ProgressPercent
			cards[index].Enrolled = true
			cards[index].EnrollmentID = &enrollmentID
			cards[index].ProgressPercent = &progress
		}
	}
	return CatalogPage{Items: cards, Page: page, PageSize: pageSize, Total: total}, nil
}

// GetCoursePartnerAudience returns the partner audience of a company course.
// Only owner/admin may inspect it.
func (s *Service) GetCoursePartnerAudience(ctx context.Context, actor Actor, courseID uuid.UUID) (CoursePartnerAudience, error) {
	queries := db.New(s.pool)
	if _, err := s.requireCoursePartnerAudienceAccess(ctx, queries, actor, courseID); err != nil {
		return CoursePartnerAudience{}, err
	}
	return s.loadCoursePartnerAudience(ctx, queries, actor, courseID)
}

// SetCoursePartnerAudience changes the partner audience of a company course and,
// for selected_partners, replaces the explicit partner list atomically.
func (s *Service) SetCoursePartnerAudience(ctx context.Context, actor Actor, input SetCoursePartnerAudienceInput) (CoursePartnerAudience, error) {
	switch input.Audience {
	case partnerAudienceNone, partnerAudienceAll, partnerAudienceSelected:
	default:
		return CoursePartnerAudience{}, validation("Некорректная аудитория курса")
	}
	members := dedupeUUIDs(input.PartnerUserIDs)
	if input.Audience == partnerAudienceSelected && len(members) == 0 {
		return CoursePartnerAudience{}, validation("Для выбранных партнёров укажите хотя бы одного пользователя")
	}
	if input.Audience != partnerAudienceSelected {
		members = nil
	}
	if len(members) > catalogMaxSelectedActors {
		return CoursePartnerAudience{}, validation("Слишком много выбранных партнёров")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CoursePartnerAudience{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	previous, err := s.requireCoursePartnerAudienceAccess(ctx, queries, actor, input.CourseID)
	if err != nil {
		return CoursePartnerAudience{}, err
	}
	now := s.now().UTC()
	if err = queries.UpsertCoursePartnerAudience(ctx, db.UpsertCoursePartnerAudienceParams{
		CompanyID: actor.CompanyID, CourseID: input.CourseID, Audience: input.Audience, UpdatedAt: now,
	}); err != nil {
		return CoursePartnerAudience{}, internal("Не удалось сохранить аудиторию курса", err)
	}
	if err = queries.DeleteCoursePartnerAudienceMembers(ctx, db.DeleteCoursePartnerAudienceMembersParams{
		CompanyID: actor.CompanyID, CourseID: input.CourseID,
	}); err != nil {
		return CoursePartnerAudience{}, internal("Не удалось обновить список партнёров", err)
	}
	for _, member := range members {
		if err = queries.InsertCoursePartnerAudienceMember(ctx, db.InsertCoursePartnerAudienceMemberParams{
			CompanyID: actor.CompanyID, CourseID: input.CourseID, PartnerUserID: member,
		}); err != nil {
			return CoursePartnerAudience{}, internal("Не удалось сохранить партнёра аудитории", err)
		}
	}
	if err = s.auditMutation(ctx, queries, actor, "course_partner_audience_set", "course", input.CourseID,
		map[string]any{"audience": previous},
		map[string]any{"audience": input.Audience, "partnerUserIds": members}); err != nil {
		return CoursePartnerAudience{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CoursePartnerAudience{}, internal("Не удалось сохранить аудиторию курса", err)
	}
	return CoursePartnerAudience{Audience: input.Audience, PartnerUserIDs: members}, nil
}

// requireCoursePartnerAudienceAccess enforces that the actor may manage the
// partner audience of the target course: owner/admin on an existing company course.
func (s *Service) requireCoursePartnerAudienceAccess(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	courseID uuid.UUID,
) (string, error) {
	if !actor.canManage() {
		return "", forbidden("Недостаточно прав для управления доступом партнёров")
	}
	row, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return "", notFound("Курс")
		}
		return "", internal("Не удалось получить курс", err)
	}
	course := courseFromRow(row)
	if course.LifecycleStatus == "deleted" {
		return "", notFound("Курс")
	}
	if course.OwnerType != "company" {
		return "", validation("Аудитория партнёров доступна только для курсов компании")
	}
	audience, err := queries.GetCoursePartnerAudience(ctx, db.GetCoursePartnerAudienceParams{
		CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		return "", internal("Не удалось получить аудиторию курса", err)
	}
	return audience, nil
}

func (s *Service) loadCoursePartnerAudience(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	courseID uuid.UUID,
) (CoursePartnerAudience, error) {
	audience, err := queries.GetCoursePartnerAudience(ctx, db.GetCoursePartnerAudienceParams{
		CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		return CoursePartnerAudience{}, internal("Не удалось получить аудиторию курса", err)
	}
	members := []uuid.UUID{}
	if audience == partnerAudienceSelected {
		members, err = queries.ListCoursePartnerAudienceMembers(ctx, db.ListCoursePartnerAudienceMembersParams{
			CompanyID: actor.CompanyID, CourseID: courseID,
		})
		if err != nil {
			return CoursePartnerAudience{}, internal("Не удалось получить партнёров аудитории", err)
		}
	}
	return CoursePartnerAudience{Audience: audience, PartnerUserIDs: members}, nil
}

// partnerAudienceAllows reports whether a partner actor may see or take the given
// company course. Non-partner actors are governed by the existing role checks.
func (s *Service) partnerAudienceAllows(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	course Course,
) (bool, error) {
	if actor.Role != "partner" {
		return true, nil
	}
	if course.OwnerType != "company" {
		return false, nil
	}
	audience, err := queries.GetCoursePartnerAudience(ctx, db.GetCoursePartnerAudienceParams{
		CompanyID: actor.CompanyID, CourseID: course.ID,
	})
	if err != nil {
		return false, internal("Не удалось проверить доступ партнёра", err)
	}
	switch audience {
	case partnerAudienceAll:
		return true, nil
	case partnerAudienceSelected:
		allowed, memberErr := queries.CoursePartnerAudienceHasMember(ctx, db.CoursePartnerAudienceHasMemberParams{
			CompanyID: actor.CompanyID, CourseID: course.ID, PartnerUserID: actor.UserID,
		})
		if memberErr != nil {
			return false, internal("Не удалось проверить доступ партнёра", memberErr)
		}
		return allowed, nil
	default:
		return false, nil
	}
}

// partnerAudienceCourseIDs returns the set of company course ids a partner may
// access, resolved in a single query for course listings.
func (s *Service) partnerAudienceCourseIDs(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
) (map[uuid.UUID]struct{}, error) {
	if actor.Role != "partner" {
		return nil, nil
	}
	ids, err := queries.ListPartnerAudienceCourseIDs(ctx, db.ListPartnerAudienceCourseIDsParams{
		CompanyID: actor.CompanyID, PartnerUserID: actor.UserID,
	})
	if err != nil {
		return nil, internal("Не удалось получить доступные партнёру курсы", err)
	}
	allowed := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	return allowed, nil
}

func dedupeUUIDs(values []uuid.UUID) []uuid.UUID {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(values))
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
