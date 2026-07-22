package application

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	domainauth "github.com/sk1fy/team-os-backend/services/academy/internal/domain/authorization"
	domaincourse "github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	domaincopy "github.com/sk1fy/team-os-backend/services/academy/internal/domain/coursecopy"
	domainversion "github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) GetPartnerCourseGroups(
	ctx context.Context,
	actor Actor,
	lifecycle, distribution *string,
) ([]PartnerCourseGroup, error) {
	if !actor.canManage() {
		return nil, forbidden("Недостаточно прав для просмотра курсов партнёров")
	}
	ownerType := "partner"
	courses, err := s.GetCourses(ctx, actor, CourseFilters{
		OwnerType: &ownerType, LifecycleStatus: lifecycle, DistributionStatus: distribution,
	})
	if err != nil {
		return nil, err
	}
	groups := make([]PartnerCourseGroup, 0)
	indexes := make(map[uuid.UUID]int)
	for _, course := range courses {
		if course.OwnerUserID == nil {
			continue
		}
		index, exists := indexes[*course.OwnerUserID]
		if !exists {
			index = len(groups)
			indexes[*course.OwnerUserID] = index
			groups = append(groups, PartnerCourseGroup{PartnerID: *course.OwnerUserID, Courses: []Course{}})
		}
		groups[index].Courses = append(groups[index].Courses, course)
	}
	return groups, nil
}

func (s *Service) GetPartnerCoursesReport(
	ctx context.Context,
	actor Actor,
	partnerID uuid.UUID,
) (PartnerCoursesReport, error) {
	if !actor.canManage() && (actor.Role != "partner" || actor.UserID != partnerID) {
		return PartnerCoursesReport{}, forbidden("Недостаточно прав для просмотра отчёта партнёра")
	}
	queries := db.New(s.pool)
	ownerType := "partner"
	rows, err := queries.GetCoursesFiltered(ctx, db.GetCoursesFilteredParams{
		CompanyID: actor.CompanyID, OwnerType: nullText(&ownerType), OwnerUserID: nullUUID(&partnerID),
	})
	if err != nil {
		return PartnerCoursesReport{}, internal("Не удалось получить курсы партнёра", err)
	}
	courses := make(map[uuid.UUID]Course, len(rows))
	report := PartnerCoursesReport{PartnerID: partnerID, Courses: []PartnerCourseOperationalReport{}}
	for _, row := range rows {
		course := courseFromRow(row)
		if !domainauth.CanViewEnrollmentReport(authorizationActor(actor), authorizationCourse(course)) {
			return PartnerCoursesReport{}, forbidden("Недостаточно прав для просмотра отчёта партнёра")
		}
		courses[course.ID] = course
		report.Summary.TotalCourses++
		switch course.LifecycleStatus {
		case "active":
			report.Summary.ActiveCourses++
		case "archived":
			report.Summary.ArchivedCourses++
		case "deleted":
			report.Summary.DeletedCourses++
		}
		if course.DistributionStatus == "paused" {
			report.Summary.PausedCourses++
		}
		if course.DistributionStatus == "blocked" {
			report.Summary.BlockedCourses++
		}
	}
	if len(courses) == 0 {
		return PartnerCoursesReport{}, notFound("Партнёрские курсы")
	}
	rowsReport, err := queries.ListPartnerOwnedCourseReports(ctx, db.ListPartnerOwnedCourseReportsParams{
		CompanyID: actor.CompanyID, OwnerUserID: nullUUID(&partnerID),
	})
	if err != nil {
		return PartnerCoursesReport{}, internal("Не удалось построить отчёт партнёра", err)
	}
	indexes := make(map[uuid.UUID]int)
	weightedProgress := make(map[uuid.UUID]int64)
	for _, row := range rowsReport {
		index, exists := indexes[row.CourseID]
		if !exists {
			course, ok := courses[row.CourseID]
			if !ok {
				continue
			}
			index = len(report.Courses)
			indexes[row.CourseID] = index
			report.Courses = append(report.Courses, PartnerCourseOperationalReport{Course: course})
		}
		item := &report.Courses[index]
		item.VersionCount++
		item.EnrollmentCount += row.EnrollmentCount
		item.CompletedEnrollmentCount += row.CompletedEnrollmentCount
		active := row.EnrollmentCount - row.CompletedEnrollmentCount - row.SuspendedEnrollmentCount
		if active > 0 {
			item.ActiveEnrollmentCount += active
		}
		weightedProgress[row.CourseID] += int64(row.AverageProgressPercent) * int64(row.EnrollmentCount)
	}
	for courseID, index := range indexes {
		if report.Courses[index].EnrollmentCount > 0 {
			report.Courses[index].AverageProgressPercent = int32(
				weightedProgress[courseID] / int64(report.Courses[index].EnrollmentCount),
			)
		}
	}
	externalRows, err := queries.ListScopedExternalEnrollmentsForReport(ctx, db.ListScopedExternalEnrollmentsForReportParams{
		CompanyID: actor.CompanyID, PartnerOwnerID: nullUUID(&partnerID),
	})
	if err != nil {
		return PartnerCoursesReport{}, internal("Не удалось получить внешние прохождения партнёра", err)
	}
	externalByCourse := make(map[uuid.UUID][]Enrollment)
	for _, enrollment := range scopedEnrollmentsFromRows(externalRows) {
		externalByCourse[enrollment.CourseID] = append(externalByCourse[enrollment.CourseID], enrollment)
	}
	report.ExternalCourses = make([]CourseExternalReport, 0, len(rows))
	for _, row := range rows {
		report.ExternalCourses = append(report.ExternalCourses, CourseExternalReport{
			CourseID: row.ID, Enrollments: externalByCourse[row.ID],
		})
	}
	return report, nil
}

func (s *Service) GetCourseVersionPreview(
	ctx context.Context,
	actor Actor,
	courseID, versionID uuid.UUID,
) (CourseVersionPreview, error) {
	queries := db.New(s.pool)
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return CourseVersionPreview{}, notFound("Курс")
		}
		return CourseVersionPreview{}, internal("Не удалось получить курс для предпросмотра", err)
	}
	course := courseFromRow(courseRow)
	if !domainauth.CanPreviewPartnerCourse(authorizationActor(actor), authorizationCourse(course)) {
		return CourseVersionPreview{}, forbidden("Недостаточно прав для предпросмотра партнёрского курса")
	}
	versionRow, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: actor.CompanyID, ID: versionID})
	if err != nil || versionRow.CourseID != courseID || versionRow.Status != "published" {
		if isNoRows(err) || err == nil {
			return CourseVersionPreview{}, notFound("Опубликованная версия курса")
		}
		return CourseVersionPreview{}, internal("Не удалось получить версию для предпросмотра", err)
	}
	content, err := s.loadCourseVersionContent(ctx, queries, versionRow)
	if err != nil {
		return CourseVersionPreview{}, err
	}
	return CourseVersionPreview{Course: overlayCourseWithVersion(course, versionRow), Version: content}, nil
}

func (s *Service) SubmitCoursePreviewQuizAttempt(
	ctx context.Context,
	actor Actor,
	courseID, versionID, quizID uuid.UUID,
	answers []EnrollmentQuizAnswer,
) (CoursePreviewQuizAttemptResult, error) {
	preview, err := s.GetCourseVersionPreview(ctx, actor, courseID, versionID)
	if err != nil {
		return CoursePreviewQuizAttemptResult{}, err
	}
	for _, quiz := range preview.Version.Quizzes {
		if quiz.ID != quizID {
			continue
		}
		score, pendingReview, evaluateErr := evaluateEnrollmentQuiz(quiz.Questions, answers)
		if evaluateErr != nil {
			return CoursePreviewQuizAttemptResult{}, evaluateErr
		}
		return CoursePreviewQuizAttemptResult{
			QuizVersionID: quizID, Score: int32(score),
			Passed: !pendingReview && int32(score) >= quiz.PassingScore, PendingReview: pendingReview,
		}, nil
	}
	return CoursePreviewQuizAttemptResult{}, notFound("Тест версии курса")
}

func (s *Service) PausePartnerCourseDistribution(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
	reason string,
) (CourseRestriction, error) {
	return s.applyPartnerCourseRestriction(ctx, actor, courseID, "pause", reason)
}

func (s *Service) BlockPartnerCourse(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
	reason string,
) (CourseRestriction, error) {
	return s.applyPartnerCourseRestriction(ctx, actor, courseID, "block", reason)
}

func (s *Service) applyPartnerCourseRestriction(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
	typeValue, reason string,
) (CourseRestriction, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 2000 {
		return CourseRestriction{}, validation("Укажите причину ограничения курса")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseRestriction{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return CourseRestriction{}, notFound("Курс")
		}
		return CourseRestriction{}, internal("Не удалось получить курс", err)
	}
	before := courseFromRow(row)
	allowed := domainauth.CanPausePartnerDistribution(authorizationActor(actor), authorizationCourse(before))
	if typeValue == "block" {
		allowed = domainauth.CanBlockPartnerCourse(authorizationActor(actor), authorizationCourse(before))
	}
	if !allowed {
		return CourseRestriction{}, forbidden("Недостаточно прав для ограничения партнёрского курса")
	}
	now := s.now().UTC()
	restrictionRow, err := queries.CreateCourseRestriction(ctx, db.CreateCourseRestrictionParams{
		ID: uuid.New(), RestrictionType: typeValue, Reason: reason, CreatedByID: actor.UserID,
		CreatedAt: now, CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseRestriction{}, conflict("Ограничение этого типа уже действует")
		}
		return CourseRestriction{}, internal("Не удалось сохранить ограничение курса", err)
	}
	afterRow, err := queries.RefreshPartnerCourseDistributionStatus(ctx, db.RefreshPartnerCourseDistributionStatusParams{
		UpdatedAt: now, CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		return CourseRestriction{}, internal("Не удалось обновить состояние распространения курса", err)
	}
	after := courseFromRow(afterRow)
	if typeValue == "block" {
		if _, err = queries.SuspendCourseEnrollmentsForBlock(ctx, db.SuspendCourseEnrollmentsForBlockParams{
			SuspendedAt: nullTimestamptz(&now), CompanyID: actor.CompanyID, CourseID: courseID,
		}); err != nil {
			return CourseRestriction{}, internal("Не удалось приостановить прохождения курса", err)
		}
	}
	if err = s.auditCourse(ctx, queries, actor, restrictionAuditAction(typeValue), &before, after); err != nil {
		return CourseRestriction{}, err
	}
	restriction := courseRestrictionFromRow(restrictionRow)
	if err = s.emitRestrictionApplied(ctx, queries, actor, after, restriction); err != nil {
		return CourseRestriction{}, err
	}
	if err = s.emitDistributionChanged(ctx, queries, actor, before, after, reason); err != nil {
		return CourseRestriction{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseRestriction{}, internal("Не удалось сохранить ограничение курса", err)
	}
	return restriction, nil
}

func (s *Service) ResolvePartnerCourseRestriction(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
	reason string,
) (CourseRestriction, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 2000 {
		return CourseRestriction{}, validation("Укажите причину снятия ограничения")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CourseRestriction{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return CourseRestriction{}, notFound("Курс")
		}
		return CourseRestriction{}, internal("Не удалось получить курс", err)
	}
	before := courseFromRow(row)
	if !domainauth.CanResolvePartnerRestriction(authorizationActor(actor), authorizationCourse(before)) {
		return CourseRestriction{}, forbidden("Недостаточно прав для снятия ограничения курса")
	}
	active, err := queries.GetActiveCourseRestrictionForUpdate(ctx, db.GetActiveCourseRestrictionForUpdateParams{
		CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		if isNoRows(err) {
			return CourseRestriction{}, conflict("У курса нет действующего ограничения")
		}
		return CourseRestriction{}, internal("Не удалось получить ограничение курса", err)
	}
	now := s.now().UTC()
	resolvedRow, err := queries.ResolveCourseRestriction(ctx, db.ResolveCourseRestrictionParams{
		ResolvedByID: nullUUID(&actor.UserID), ResolvedAt: nullTimestamptz(&now),
		ResolutionReason: nullText(&reason), CompanyID: actor.CompanyID, CourseID: courseID, ID: active.ID,
	})
	if err != nil {
		return CourseRestriction{}, internal("Не удалось снять ограничение курса", err)
	}
	afterRow, err := queries.RefreshPartnerCourseDistributionStatus(ctx, db.RefreshPartnerCourseDistributionStatusParams{
		UpdatedAt: now, CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		return CourseRestriction{}, internal("Не удалось обновить состояние распространения курса", err)
	}
	after := courseFromRow(afterRow)
	if active.RestrictionType == "block" && after.DistributionStatus != "blocked" {
		if _, err = queries.RestoreCourseEnrollmentsAfterBlock(ctx, db.RestoreCourseEnrollmentsAfterBlockParams{
			ResolvedAt: now, CompanyID: actor.CompanyID, CourseID: courseID,
		}); err != nil {
			return CourseRestriction{}, internal("Не удалось восстановить прохождения курса", err)
		}
	}
	if err = s.auditCourse(ctx, queries, actor, "restriction_resolved", &before, after); err != nil {
		return CourseRestriction{}, err
	}
	resolved := courseRestrictionFromRow(resolvedRow)
	if err = s.emitRestrictionResolved(ctx, queries, actor, after, resolved); err != nil {
		return CourseRestriction{}, err
	}
	if err = s.emitDistributionChanged(ctx, queries, actor, before, after, reason); err != nil {
		return CourseRestriction{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CourseRestriction{}, internal("Не удалось снять ограничение курса", err)
	}
	return resolved, nil
}

func (s *Service) GetCourseRestrictions(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
) ([]CourseRestriction, error) {
	queries := db.New(s.pool)
	row, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return nil, notFound("Курс")
		}
		return nil, internal("Не удалось получить курс", err)
	}
	course := courseFromRow(row)
	if !domainauth.CanViewPartnerCourse(authorizationActor(actor), authorizationCourse(course)) {
		return nil, forbidden("Недостаточно прав для просмотра ограничений курса")
	}
	rows, err := queries.ListCourseRestrictions(ctx, db.ListCourseRestrictionsParams{
		CompanyID: actor.CompanyID, CourseID: courseID,
	})
	if err != nil {
		return nil, internal("Не удалось получить ограничения курса", err)
	}
	result := make([]CourseRestriction, len(rows))
	for index := range rows {
		result[index] = courseRestrictionFromRow(rows[index])
	}
	return result, nil
}

func (s *Service) CopyPartnerCourseVersionToCompany(
	ctx context.Context,
	actor Actor,
	courseID, versionID uuid.UUID,
	idempotencyKey string,
) (PartnerCourseCopyResult, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len([]byte(idempotencyKey)) > 512 {
		return PartnerCourseCopyResult{}, validation("Некорректный ключ идемпотентности")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	idempotencyParams := db.GetPartnerCourseCopyIdempotencyParams{
		CompanyID: actor.CompanyID, SourceCourseID: courseID,
		SourceCourseVersionID: versionID, IdempotencyKey: idempotencyKey,
	}
	previous, previousErr := queries.GetPartnerCourseCopyIdempotency(ctx, idempotencyParams)
	if previousErr == nil {
		result, loadErr := s.loadPartnerCourseCopyResult(ctx, queries, actor.CompanyID, previous.TargetCourseID, previous.TargetCourseVersionID)
		if loadErr != nil {
			return PartnerCourseCopyResult{}, loadErr
		}
		if err = tx.Commit(ctx); err != nil {
			return PartnerCourseCopyResult{}, internal("Не удалось завершить повторное копирование", err)
		}
		return result, nil
	}
	if !isNoRows(previousErr) {
		return PartnerCourseCopyResult{}, internal("Не удалось проверить повторное копирование", previousErr)
	}
	sourceCourseRow, err := queries.GetCourseForUpdate(ctx, db.GetCourseForUpdateParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return PartnerCourseCopyResult{}, notFound("Партнёрский курс")
		}
		return PartnerCourseCopyResult{}, internal("Не удалось получить партнёрский курс", err)
	}
	sourceCourse := courseFromRow(sourceCourseRow)
	if !domainauth.CanCopyPartnerCourse(authorizationActor(actor), authorizationCourse(sourceCourse)) {
		return PartnerCourseCopyResult{}, forbidden("Недостаточно прав для копирования партнёрского курса")
	}
	source, err := queries.LockPublishedPartnerCourseVersionForCopy(ctx, db.LockPublishedPartnerCourseVersionForCopyParams{
		CompanyID: actor.CompanyID, CourseID: courseID, VersionID: versionID,
	})
	if err != nil {
		if isNoRows(err) {
			return PartnerCourseCopyResult{}, notFound("Опубликованная версия партнёрского курса")
		}
		return PartnerCourseCopyResult{}, internal("Не удалось получить версию для копирования", err)
	}
	if !source.OwnerUserID.Valid {
		return PartnerCourseCopyResult{}, internal("У партнёрского курса отсутствует владелец", nil)
	}
	now := s.now().UTC()
	targetCourseID, targetVersionID, originID := uuid.New(), uuid.New(), uuid.New()
	sourceVersionRow, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{
		CompanyID: actor.CompanyID, ID: versionID,
	})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось получить содержимое версии для копирования", err)
	}
	sourceContent, err := s.loadCourseVersionContent(ctx, queries, sourceVersionRow)
	if err != nil {
		return PartnerCourseCopyResult{}, err
	}
	domainSource, err := domainVersionFromContent(sourceContent)
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Некорректное содержимое версии для копирования", err)
	}
	copyPlan, err := domaincopy.CopyPartnerVersion(domaincopy.Params{
		SourceCourse: authorizationCourse(sourceCourse), SourceVersion: domainSource.Snapshot(),
		DestinationCourseID: domaincourse.ID(targetCourseID.String()), DestinationVersionID: domainversion.ID(targetVersionID.String()),
		CreatedByID: domaincourse.ID(actor.UserID.String()), CreatedAt: now,
		MapID: func(domaincopy.EntityKind, domainversion.ID) domainversion.ID {
			return domainversion.ID(uuid.NewString())
		},
	})
	if err != nil {
		return PartnerCourseCopyResult{}, validation(err.Error())
	}
	draftSnapshot := copyPlan.Draft.Snapshot()
	draftDefinition := draftSnapshot.Definition
	// Physical copies are created asynchronously. Persist source IDs until the
	// file saga atomically rewrites them to the Files service results.
	sourceDefinition := domainSource.Snapshot().Definition
	draftDefinition.CoverFileID = sourceDefinition.CoverFileID
	sourceFilesByStableKey := make(map[string][]domainversion.ID, len(sourceDefinition.Lessons))
	for _, lesson := range sourceDefinition.Lessons {
		sourceFilesByStableKey[lesson.StableKey] = append([]domainversion.ID(nil), lesson.FileIDs...)
	}
	for index := range draftDefinition.Lessons {
		draftDefinition.Lessons[index].FileIDs = append(
			[]domainversion.ID(nil), sourceFilesByStableKey[draftDefinition.Lessons[index].StableKey]...,
		)
	}
	targetRow, err := queries.CreateOwnedCourse(ctx, db.CreateOwnedCourseParams{
		ID: targetCourseID, CompanyID: actor.CompanyID, Title: draftDefinition.Title,
		Description: nullText(draftDefinition.Description), CoverUrl: nullText(draftDefinition.CoverURL), Status: "draft", AuthorID: actor.UserID,
		Sequential: draftDefinition.Sequential, DeadlineDays: nullInt4(int32Pointer(draftDefinition.DefaultInternalDeadlineDays)),
		CreatedAt: now, Visibility: "restricted", OwnerType: "company",
		OwnerUserID: uuid.NullUUID{}, CreatedByID: nullUUID(&actor.UserID),
	})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось создать независимую копию курса", err)
	}
	versionRow, err := queries.CreateCourseVersion(ctx, db.CreateCourseVersionParams{
		ID: targetVersionID, CompanyID: actor.CompanyID, CourseID: targetCourseID, Number: 1,
		Title: draftDefinition.Title, Description: nullText(draftDefinition.Description),
		CoverFileID: nullUUID(domainIDUUID(draftDefinition.CoverFileID)), CoverUrl: nullText(draftDefinition.CoverURL),
		Sequential: draftDefinition.Sequential, DefaultInternalDeadlineDays: nullInt4(int32Pointer(draftDefinition.DefaultInternalDeadlineDays)),
		CreatedByID: actor.UserID, CreatedAt: now,
	})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось создать черновик копии курса", err)
	}
	if affected, setErr := queries.SetCourseCurrentDraftVersion(ctx, db.SetCourseCurrentDraftVersionParams{
		UpdatedAt: now, CompanyID: actor.CompanyID, CourseID: targetCourseID, VersionID: targetVersionID,
	}); setErr != nil || affected != 1 {
		return PartnerCourseCopyResult{}, internal("Не удалось связать черновик копии с курсом", setErr)
	}
	if err = persistPartnerCourseCopyDefinition(ctx, queries, actor.CompanyID, targetVersionID, draftDefinition); err != nil {
		return PartnerCourseCopyResult{}, err
	}
	originRow, err := queries.CreateCourseOrigin(ctx, db.CreateCourseOriginParams{
		ID: originID, OriginType: "partner_course", SourceCourseID: nullUUID(&courseID),
		SourceCourseVersionID: nullUUID(&versionID), SourcePartnerID: source.OwnerUserID,
		SourceTemplateID: uuid.NullUUID{}, SourceTemplateVersionID: uuid.NullUUID{},
		InstantiatedByID: actor.UserID, InstantiatedAt: now, AcquisitionType: "free_copy",
		EntitlementID: uuid.NullUUID{}, CompanyID: actor.CompanyID, TargetCourseID: targetCourseID,
	})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось сохранить происхождение копии курса", err)
	}
	if _, err = queries.CreatePartnerCourseCopyIdempotency(ctx, db.CreatePartnerCourseCopyIdempotencyParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, SourceCourseID: courseID, SourceCourseVersionID: versionID,
		IdempotencyKey: idempotencyKey, TargetCourseID: targetCourseID, TargetCourseVersionID: targetVersionID,
		OriginID: originID, CreatedByID: actor.UserID, CreatedAt: now,
	}); err != nil {
		if isNoRows(err) {
			return PartnerCourseCopyResult{}, conflict("Копирование с этим ключом уже выполняется")
		}
		return PartnerCourseCopyResult{}, internal("Не удалось сохранить ключ копирования", err)
	}
	fileIDs, err := queries.ListCourseVersionFileIDsForClone(ctx, db.ListCourseVersionFileIDsForCloneParams{
		CompanyID: actor.CompanyID, CourseVersionID: versionID,
	})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось определить файлы партнёрского курса", err)
	}
	if len(fileIDs) > 0 {
		jobID := uuid.New()
		if _, err = queries.CreateFileCloneJob(ctx, db.CreateFileCloneJobParams{
			ID: jobID, CompanyID: actor.CompanyID, OperationType: "partner_course_copy", AggregateID: targetCourseID,
			IdempotencyKey:  "partner-copy:" + versionID.String() + ":" + idempotencyKey,
			SourceOwnerType: "course_version", SourceOwnerID: versionID,
			TargetOwnerType: "course_version", TargetOwnerID: targetVersionID, CreatedAt: now,
		}); err != nil {
			return PartnerCourseCopyResult{}, internal("Не удалось создать задачу копирования файлов", err)
		}
		if _, err = queries.AddFileCloneJobItems(ctx, db.AddFileCloneJobItemsParams{
			UpdatedAt: now, SourceFileIds: fileIDs, CompanyID: actor.CompanyID, JobID: jobID,
		}); err != nil {
			return PartnerCourseCopyResult{}, internal("Не удалось сохранить файлы для копирования", err)
		}
	}
	target := courseFromRow(targetRow)
	target.CurrentDraftVersionID = &targetVersionID
	draft := courseVersionFromRow(versionRow)
	origin := courseOriginFromRow(originRow)
	if err = s.auditCourse(ctx, queries, actor, "partner_course_copied", nil, target); err != nil {
		return PartnerCourseCopyResult{}, err
	}
	if err = s.emit(ctx, queries, actor.CompanyID, targetCourseID, actor.UserID,
		"teamos.academy.course.copied.v1", &eventsv1.AcademyPartnerCourseCopiedPayload{
			SourceCourseId: courseID.String(), SourceCourseVersionId: versionID.String(),
			SourcePartnerUserId: source.OwnerUserID.UUID.String(), TargetCourseId: targetCourseID.String(),
			TargetDraftVersionId: targetVersionID.String(), CopiedById: actor.UserID.String(),
			CopiedAt: timestamppb.New(now), TargetCourseTitle: optionalStringValue(target.Title),
		}); err != nil {
		return PartnerCourseCopyResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось сохранить копию партнёрского курса", err)
	}
	return PartnerCourseCopyResult{Course: target, Draft: draft, Origin: origin}, nil
}

func (s *Service) loadPartnerCourseCopyResult(
	ctx context.Context,
	queries *db.Queries,
	companyID, courseID, versionID uuid.UUID,
) (PartnerCourseCopyResult, error) {
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: companyID, ID: courseID})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось получить ранее созданную копию курса", err)
	}
	versionRow, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: companyID, ID: versionID})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось получить черновик ранее созданной копии", err)
	}
	originRow, err := queries.GetCourseOrigin(ctx, db.GetCourseOriginParams{CompanyID: companyID, TargetCourseID: courseID})
	if err != nil {
		return PartnerCourseCopyResult{}, internal("Не удалось получить происхождение ранее созданной копии", err)
	}
	return PartnerCourseCopyResult{
		Course: courseFromRow(courseRow), Draft: courseVersionFromRow(versionRow), Origin: courseOriginFromRow(originRow),
	}, nil
}

type copiedQuizOption struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Correct bool   `json:"correct"`
}

type copiedQuizQuestion struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Text    string             `json:"text"`
	Options []copiedQuizOption `json:"options"`
}

func persistPartnerCourseCopyDefinition(
	ctx context.Context,
	queries *db.Queries,
	companyID, versionID uuid.UUID,
	definition domainversion.Definition,
) error {
	for _, section := range definition.Sections {
		sectionID, err := uuid.Parse(string(section.ID))
		if err != nil {
			return internal("Некорректный идентификатор раздела копии", err)
		}
		stableKey, err := uuid.Parse(section.StableKey)
		if err != nil {
			return internal("Некорректный стабильный ключ раздела копии", err)
		}
		if _, err = queries.CreateCourseVersionSection(ctx, db.CreateCourseVersionSectionParams{
			ID: sectionID, StableKey: stableKey, Title: section.Title, OrderValue: int32(section.Order),
			CompanyID: companyID, CourseVersionID: versionID,
		}); err != nil {
			return internal("Не удалось скопировать раздел курса", err)
		}
	}
	for _, lesson := range definition.Lessons {
		lessonID, err := uuid.Parse(string(lesson.ID))
		if err != nil {
			return internal("Некорректный идентификатор урока копии", err)
		}
		sectionID, err := uuid.Parse(string(lesson.SectionID))
		if err != nil {
			return internal("Некорректный раздел урока копии", err)
		}
		stableKey, err := uuid.Parse(lesson.StableKey)
		if err != nil {
			return internal("Некорректный стабильный ключ урока копии", err)
		}
		if _, err = queries.CreateCourseVersionLesson(ctx, db.CreateCourseVersionLessonParams{
			ID: lessonID, SectionVersionID: sectionID, StableKey: stableKey, Title: lesson.Title,
			OrderValue: int32(lesson.Order), Content: append([]byte(nil), lesson.Content...), SourceType: lesson.SourceType,
			SourceArticleID: nullUUID(domainIDUUID(lesson.SourceArticleID)), SourceArticleVersion: nullInt4(int32Pointer(lesson.SourceArticleVersion)),
			SourceTemplateID:        nullUUID(domainIDUUID(lesson.SourceTemplateID)),
			SourceTemplateVersionID: nullUUID(domainIDUUID(lesson.SourceTemplateVersionID)),
			EstimatedMinutes:        nullInt4(int32Pointer(lesson.EstimatedMinutes)),
			FileIds:                 domainIDsToUUIDs(lesson.FileIDs),
			CompanyID:               companyID, CourseVersionID: versionID,
		}); err != nil {
			return internal("Не удалось скопировать урок курса", err)
		}
		if lesson.Quiz == nil {
			continue
		}
		quizID, err := uuid.Parse(string(lesson.Quiz.ID))
		if err != nil {
			return internal("Некорректный идентификатор теста копии", err)
		}
		questions := make([]copiedQuizQuestion, len(lesson.Quiz.Questions))
		for questionIndex, question := range lesson.Quiz.Questions {
			options := make([]copiedQuizOption, len(question.Options))
			for optionIndex, option := range question.Options {
				options[optionIndex] = copiedQuizOption{ID: string(option.ID), Text: option.Text, Correct: option.Correct}
			}
			questions[questionIndex] = copiedQuizQuestion{
				ID: string(question.ID), Type: string(question.Type), Text: question.Text, Options: options,
			}
		}
		encoded, err := json.Marshal(questions)
		if err != nil {
			return internal("Не удалось сформировать тест копии", err)
		}
		if _, err = queries.CreateCourseVersionQuiz(ctx, db.CreateCourseVersionQuizParams{
			ID: quizID, LessonVersionID: lessonID, Questions: encoded, PassingScore: int32(lesson.Quiz.PassingScore),
			MaxAttempts: nullInt4(int32Pointer(lesson.Quiz.MaxAttempts)), CompanyID: companyID, CourseVersionID: versionID,
		}); err != nil {
			return internal("Не удалось скопировать тест курса", err)
		}
	}
	return nil
}

func domainIDsToUUIDs(values []domainversion.ID) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(string(value))
		if err == nil {
			result = append(result, parsed)
		}
	}
	return result
}

func domainIDUUID(value *domainversion.ID) *uuid.UUID {
	if value == nil {
		return nil
	}
	parsed, err := uuid.Parse(string(*value))
	if err != nil {
		return nil
	}
	return &parsed
}

func int32Pointer(value *int) *int32 {
	if value == nil {
		return nil
	}
	converted := int32(*value)
	return &converted
}

func courseRestrictionFromRow(row db.CourseRestriction) CourseRestriction {
	return CourseRestriction{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, Type: row.RestrictionType,
		Reason: row.Reason, CreatedByID: row.CreatedByID, CreatedAt: row.CreatedAt,
		ResolvedByID: nullUUIDPointer(row.ResolvedByID), ResolvedAt: timestamptzPointer(row.ResolvedAt),
		ResolutionReason: textPointer(row.ResolutionReason),
	}
}

func courseOriginFromRow(row db.CourseOrigin) CourseOrigin {
	return CourseOrigin{
		Type: row.OriginType, SourceCourseID: nullUUIDPointer(row.SourceCourseID),
		SourceCourseVersionID: nullUUIDPointer(row.SourceCourseVersionID), SourcePartnerID: nullUUIDPointer(row.SourcePartnerID),
		SourceTemplateID: nullUUIDPointer(row.SourceTemplateID), SourceTemplateVersionID: nullUUIDPointer(row.SourceTemplateVersionID),
		InstantiatedByID: row.InstantiatedByID, InstantiatedAt: row.InstantiatedAt,
		AcquisitionType: row.AcquisitionType, EntitlementID: nullUUIDPointer(row.EntitlementID),
	}
}

func restrictionAuditAction(value string) string {
	if value == "block" {
		return "course_blocked"
	}
	return "distribution_paused"
}

func restrictionTypeToEvent(value string) eventsv1.AcademyCourseRestrictionType {
	if value == "block" {
		return eventsv1.AcademyCourseRestrictionType_ACADEMY_COURSE_RESTRICTION_TYPE_BLOCK
	}
	if value == "pause" {
		return eventsv1.AcademyCourseRestrictionType_ACADEMY_COURSE_RESTRICTION_TYPE_PAUSE
	}
	return eventsv1.AcademyCourseRestrictionType_ACADEMY_COURSE_RESTRICTION_TYPE_UNSPECIFIED
}

func (s *Service) emitRestrictionApplied(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	course Course,
	restriction CourseRestriction,
) error {
	if course.OwnerUserID == nil {
		return internal("У партнёрского курса отсутствует владелец", nil)
	}
	return s.emit(ctx, queries, actor.CompanyID, course.ID, actor.UserID,
		"teamos.academy.course.restriction.applied.v1", &eventsv1.AcademyCourseRestrictionAppliedPayload{
			RestrictionId: restriction.ID.String(), CourseId: course.ID.String(), PartnerUserId: course.OwnerUserID.String(),
			Type: restrictionTypeToEvent(restriction.Type), Reason: restriction.Reason, AppliedById: actor.UserID.String(),
			AppliedAt: timestamppb.New(restriction.CreatedAt), CourseTitle: optionalStringValue(course.Title),
			Link:             optionalStringValue(academyLink + "/courses/" + course.ID.String()),
			RecipientUserIds: []string{course.OwnerUserID.String()},
		})
}

func (s *Service) emitRestrictionResolved(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	course Course,
	restriction CourseRestriction,
) error {
	if course.OwnerUserID == nil || restriction.ResolvedAt == nil || restriction.ResolutionReason == nil {
		return internal("Некорректные данные снятого ограничения", nil)
	}
	return s.emit(ctx, queries, actor.CompanyID, course.ID, actor.UserID,
		"teamos.academy.course.restriction.resolved.v1", &eventsv1.AcademyCourseRestrictionResolvedPayload{
			RestrictionId: restriction.ID.String(), CourseId: course.ID.String(), PartnerUserId: course.OwnerUserID.String(),
			Type: restrictionTypeToEvent(restriction.Type), ResolutionReason: *restriction.ResolutionReason,
			ResolvedById: actor.UserID.String(), ResolvedAt: timestamppb.New(*restriction.ResolvedAt),
			CourseTitle: optionalStringValue(course.Title), Link: optionalStringValue(academyLink + "/courses/" + course.ID.String()),
			RecipientUserIds: []string{course.OwnerUserID.String()},
		})
}

func (s *Service) emitDistributionChanged(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	before, after Course,
	reason string,
) error {
	payload := &eventsv1.AcademyCourseDistributionChangedPayload{
		CourseId: after.ID.String(), PreviousStatus: courseDistributionToEvent(before.DistributionStatus),
		Status: courseDistributionToEvent(after.DistributionStatus), Reason: optionalStringValue(reason),
		CourseTitle: optionalStringValue(after.Title), Link: optionalStringValue(academyLink + "/courses/" + after.ID.String()),
	}
	if after.OwnerUserID != nil {
		payload.PartnerUserId = optionalUUIDStringValue(after.OwnerUserID)
		payload.RecipientUserIds = []string{after.OwnerUserID.String()}
	}
	return s.emit(ctx, queries, actor.CompanyID, after.ID, actor.UserID,
		"teamos.academy.course.distribution.changed.v1", payload)
}
