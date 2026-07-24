//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPartnerAudienceControlsCatalogDetailAndSelfEnrollment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := externalQuizTestPool(t, ctx)
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	fixture := seedExternalQuizFixture(t, ctx, pool, now)
	if _, err := pool.Exec(ctx, `
		UPDATE courses
		SET visibility='company', owner_type='company',
		    lifecycle_status='active', distribution_status='active'
		WHERE company_id=$1 AND id=$2`,
		fixture.companyID, fixture.courseID,
	); err != nil {
		t.Fatalf("prepare company course: %v", err)
	}
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("create Academy service: %v", err)
	}
	service.now = func() time.Time { return now }
	owner := Actor{CompanyID: fixture.companyID, UserID: uuid.New(), Role: "owner"}
	employee := Actor{CompanyID: fixture.companyID, UserID: uuid.New(), Role: "employee"}
	partner := Actor{CompanyID: fixture.companyID, UserID: uuid.New(), Role: "partner"}

	employeeCatalog, err := service.GetAcademyCatalog(ctx, employee, CatalogQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("employee catalog: %v", err)
	}
	if employeeCatalog.Total != 1 || len(employeeCatalog.Items) != 1 {
		t.Fatalf("employee catalog = %+v", employeeCatalog)
	}
	defaultAudience, err := service.GetCoursePartnerAudience(ctx, owner, fixture.courseID)
	if err != nil {
		t.Fatalf("default audience: %v", err)
	}
	if defaultAudience.Audience != partnerAudienceNone || len(defaultAudience.PartnerUserIDs) != 0 {
		t.Fatalf("default audience = %+v", defaultAudience)
	}
	assertPartnerCatalogTotal(t, ctx, service, partner, 0)

	if _, err = service.SetCoursePartnerAudience(ctx, owner, SetCoursePartnerAudienceInput{
		CourseID: fixture.courseID, Audience: partnerAudienceAll,
	}); err != nil {
		t.Fatalf("set all partners: %v", err)
	}
	assertPartnerCatalogTotal(t, ctx, service, partner, 1)
	if _, err = service.GetCatalogCourseVersion(ctx, partner, fixture.courseID); err != nil {
		t.Fatalf("partner course detail: %v", err)
	}

	if _, err = service.SetCoursePartnerAudience(ctx, owner, SetCoursePartnerAudienceInput{
		CourseID: fixture.courseID, Audience: partnerAudienceSelected, PartnerUserIDs: []uuid.UUID{uuid.New()},
	}); err != nil {
		t.Fatalf("set another selected partner: %v", err)
	}
	assertPartnerCatalogTotal(t, ctx, service, partner, 0)
	if _, err = service.SelfEnrollCourse(ctx, partner, fixture.courseID); !isApplicationError(err, ErrorNotFound) {
		t.Fatalf("partner outside audience self-enroll error = %v", err)
	}

	if _, err = service.SetCoursePartnerAudience(ctx, owner, SetCoursePartnerAudienceInput{
		CourseID: fixture.courseID, Audience: partnerAudienceSelected,
		PartnerUserIDs: []uuid.UUID{partner.UserID, partner.UserID},
	}); err != nil {
		t.Fatalf("select current partner: %v", err)
	}
	assertPartnerCatalogTotal(t, ctx, service, partner, 1)
	enrollment, err := service.SelfEnrollCourse(ctx, partner, fixture.courseID)
	if err != nil {
		t.Fatalf("partner self-enroll: %v", err)
	}
	if enrollment.UserID == nil || *enrollment.UserID != partner.UserID ||
		enrollment.SourceType != "self_enrollment" {
		t.Fatalf("partner enrollment = %+v", enrollment)
	}
	if enrollment.CourseTitle == nil || *enrollment.CourseTitle != "Внешний курс" ||
		enrollment.CompletedLessonCount == nil || *enrollment.CompletedLessonCount != 0 ||
		enrollment.TotalLessonCount == nil || *enrollment.TotalLessonCount != 2 {
		t.Fatalf("partner enrollment read model = %+v", enrollment)
	}

	if _, err = pool.Exec(ctx, `
		INSERT INTO course_partner_audiences (company_id, course_id, audience)
		VALUES ($1,$2,'all_partners')`,
		uuid.New(), fixture.courseID,
	); err == nil {
		t.Fatal("cross-tenant audience row accepted")
	}
}

func TestReviewOpenAnswerAttemptThroughApplication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := externalQuizTestPool(t, ctx)
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	fixture := seedExternalQuizFixtureWithQuestions(t, ctx, pool, now, []byte(
		`[{"id":"q1","type":"open","text":"Объясните ответ","options":[]}]`,
	))
	userID, ownerID, enrollmentID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_enrollments (
			id, company_id, course_id, course_version_id, learner_type,
			user_id, source_type, attempt_number, progress_status,
			access_status, current_lesson_version_id, started_at,
			last_activity_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,'user',$5,'legacy',1,'in_progress',
			'active',$6,$7,$7,$7,$7)`,
		enrollmentID, fixture.companyID, fixture.courseID, fixture.versionID,
		userID, fixture.firstLessonID, now,
	); err != nil {
		t.Fatalf("prepare employee enrollment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO enrollment_lesson_progress (
			company_id, enrollment_id, lesson_version_id, status, first_opened_at
		) VALUES ($1,$2,$3,'current',$4)`,
		fixture.companyID, enrollmentID, fixture.firstLessonID, now,
	); err != nil {
		t.Fatalf("prepare lesson progress: %v", err)
	}
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("create Academy service: %v", err)
	}
	service.now = func() time.Time { return now }
	employee := Actor{CompanyID: fixture.companyID, UserID: userID, Role: "employee"}
	owner := Actor{CompanyID: fixture.companyID, UserID: ownerID, Role: "owner"}
	if _, err = service.CompleteEnrollmentLesson(ctx, employee, CompleteEnrollmentLessonInput{
		EnrollmentID: enrollmentID, LessonID: fixture.firstLessonID,
		IdempotencyKey: "review-first-lesson",
	}); err != nil {
		t.Fatalf("complete first lesson: %v", err)
	}
	answer := "Развёрнутый ответ"
	attempt, progress, err := service.SubmitEnrollmentQuizAttempt(ctx, employee, SubmitEnrollmentQuizInput{
		EnrollmentID: enrollmentID, QuizID: fixture.quizID,
		IdempotencyKey: "review-open-attempt",
		Answers:        []EnrollmentQuizAnswer{{QuestionID: fixture.questionID, Text: &answer}},
	})
	if err != nil {
		t.Fatalf("submit open answer: %v", err)
	}
	if !attempt.PendingReview || attempt.Passed || progress.Enrollment.ProgressStatus == "completed" {
		t.Fatalf("pending attempt=%+v progress=%+v", attempt, progress.Enrollment)
	}
	comment := "Ответ принят"
	reviewed, reviewedProgress, err := service.ReviewEnrollmentQuizAttempt(ctx, owner, ReviewEnrollmentQuizInput{
		EnrollmentID: enrollmentID, AttemptID: attempt.ID, Passed: true, Comment: &comment,
	})
	if err != nil {
		t.Fatalf("review open answer: %v", err)
	}
	if reviewed.PendingReview || !reviewed.Passed || reviewed.ReviewedByID == nil ||
		*reviewed.ReviewedByID != ownerID {
		t.Fatalf("reviewed attempt = %+v", reviewed)
	}
	if reviewedProgress.Enrollment.ProgressStatus != "completed" ||
		reviewedProgress.Enrollment.ProgressPercent != 100 {
		t.Fatalf("reviewed progress = %+v", reviewedProgress.Enrollment)
	}
	assertCount(t, ctx, pool, `
		SELECT count(*) FROM audit_log
		WHERE company_id=$1 AND action='quiz_attempt_reviewed'
		  AND aggregate_id=$2`, 1,
		fixture.companyID, attempt.ID)
}

func TestPartnerExternalReportUsesPaginatedReadModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := externalQuizTestPool(t, ctx)
	now := time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC)
	fixture := seedExternalQuizFixture(t, ctx, pool, now)
	partnerID := uuid.New()
	if _, err := pool.Exec(ctx, `
		UPDATE courses
		SET owner_type='partner', owner_user_id=$3
		WHERE company_id=$1 AND id=$2`,
		fixture.companyID, fixture.courseID, partnerID,
	); err != nil {
		t.Fatalf("prepare partner course: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE external_learners
		SET first_name='Иван', last_name='Партнёрский'
		WHERE company_id=$1 AND id=$2`,
		fixture.companyID, fixture.learnerID,
	); err != nil {
		t.Fatalf("prepare external learner: %v", err)
	}
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("create Academy service: %v", err)
	}
	partner := Actor{CompanyID: fixture.companyID, UserID: partnerID, Role: "partner"}
	search := "партнёрский"
	page, err := service.GetPartnerExternalReportPage(ctx, partner, PartnerExternalReportQuery{
		Search: &search, CourseID: &fixture.courseID, Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("partner external report: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("partner external report page = %+v", page)
	}
	item := page.Items[0]
	if item.EnrollmentID != fixture.enrollmentID || item.CourseID != fixture.courseID ||
		item.LearnerEmail != "learner@example.test" || item.LearnerName == nil ||
		*item.LearnerName != "Иван Партнёрский" {
		t.Fatalf("partner external report row = %+v", item)
	}
	otherPartner := Actor{CompanyID: fixture.companyID, UserID: uuid.New(), Role: "partner"}
	empty, err := service.GetPartnerExternalReportPage(ctx, otherPartner, PartnerExternalReportQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("other partner external report: %v", err)
	}
	if empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatalf("other partner can see report: %+v", empty)
	}
}

func TestInternalEnrollmentReportFiltersAndPaginatesInAcademy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := externalQuizTestPool(t, ctx)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	fixture := seedExternalQuizFixture(t, ctx, pool, now)
	firstUserID, secondUserID := uuid.New(), uuid.New()
	firstEnrollmentID, secondEnrollmentID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_enrollments (
			id, company_id, course_id, course_version_id, learner_type,
			user_id, source_type, attempt_number, progress_status,
			access_status, current_lesson_version_id, frozen_at,
			created_at, updated_at
		) VALUES
			($1,$2,$3,$4,'user',$5,'legacy',1,'not_started','ready',$6,NULL,$7,$7),
			($8,$2,$3,$4,'user',$9,'legacy',1,'in_progress','frozen',$6,$7,$7,$7)`,
		firstEnrollmentID, fixture.companyID, fixture.courseID, fixture.versionID,
		firstUserID, fixture.firstLessonID, now,
		secondEnrollmentID, secondUserID,
	); err != nil {
		t.Fatalf("prepare internal enrollments: %v", err)
	}
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("create Academy service: %v", err)
	}
	service.now = func() time.Time { return now }
	owner := Actor{CompanyID: fixture.companyID, UserID: uuid.New(), Role: "owner"}
	page, err := service.GetInternalEnrollmentReportPage(ctx, owner, InternalEnrollmentReportQuery{
		UserIDs: []uuid.UUID{firstUserID, secondUserID}, Page: 1, PageSize: 1,
	})
	if err != nil {
		t.Fatalf("internal report page: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Page != 1 || page.PageSize != 1 {
		t.Fatalf("internal report page = %+v", page)
	}
	if page.Items[0].CourseTitle == nil || *page.Items[0].CourseTitle != "Внешний курс" ||
		page.Items[0].TotalLessonCount == nil || *page.Items[0].TotalLessonCount != 2 {
		t.Fatalf("internal report read model = %+v", page.Items[0])
	}

	search := "сотрудник"
	frozen := "frozen"
	filtered, err := service.GetInternalEnrollmentReportPage(ctx, owner, InternalEnrollmentReportQuery{
		UserIDs: []uuid.UUID{firstUserID, secondUserID}, SearchUserIDs: []uuid.UUID{secondUserID},
		Search: &search, Status: &frozen, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("filtered internal report: %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 ||
		filtered.Items[0].ID != secondEnrollmentID {
		t.Fatalf("filtered internal report = %+v", filtered)
	}

	courseSearch := "внешний"
	byCourse, err := service.GetInternalEnrollmentReportPage(ctx, owner, InternalEnrollmentReportQuery{
		UserIDs: []uuid.UUID{firstUserID, secondUserID}, Search: &courseSearch,
		Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("course search internal report: %v", err)
	}
	if byCourse.Total != 2 || len(byCourse.Items) != 2 {
		t.Fatalf("course search internal report = %+v", byCourse)
	}

	employee := Actor{CompanyID: fixture.companyID, UserID: firstUserID, Role: "employee"}
	if _, err = service.GetInternalEnrollmentReportPage(ctx, employee, InternalEnrollmentReportQuery{
		UserIDs: []uuid.UUID{firstUserID}, Page: 1, PageSize: 20,
	}); !isApplicationError(err, ErrorForbidden) {
		t.Fatalf("employee internal report error = %v", err)
	}
}

func assertPartnerCatalogTotal(
	t *testing.T,
	ctx context.Context,
	service *Service,
	partner Actor,
	want int64,
) {
	t.Helper()
	page, err := service.GetAcademyCatalog(ctx, partner, CatalogQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("partner catalog: %v", err)
	}
	if page.Total != want || int64(len(page.Items)) != want {
		t.Fatalf("partner catalog total=%d items=%d, want=%d", page.Total, len(page.Items), want)
	}
}
