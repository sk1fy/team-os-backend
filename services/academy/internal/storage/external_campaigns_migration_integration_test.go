//go:build integration

package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestExternalCampaignMigrationIsolationAnalyticsAndRetention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к миграциям")
	}
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("academy"), postgres.WithUsername("academy"),
		postgres.WithPassword("academy"), postgres.WithInitScripts(
			filepath.Join(migrationsDir, "000001_init.up.sql"),
			filepath.Join(migrationsDir, "000002_assignment_events_and_outbox.up.sql"),
			filepath.Join(migrationsDir, "000003_course_visibility_assignment_idempotency.up.sql"),
			filepath.Join(migrationsDir, "000004_course_ownership_lifecycle_audit.up.sql"),
			filepath.Join(migrationsDir, "000005_immutable_course_versions.up.sql"),
			filepath.Join(migrationsDir, "000006_version_pinned_enrollments.up.sql"),
			filepath.Join(migrationsDir, "000007_partner_courses_and_restrictions.up.sql"),
			filepath.Join(migrationsDir, "000008_course_templates_and_kb_snapshots.up.sql"),
			filepath.Join(migrationsDir, "000009_external_learners_and_personal_accesses.up.sql"),
		),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("запуск Postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("DSN Postgres: %v", err)
	}
	pool, err := newMigrationTestPool(ctx, dsn)
	if err != nil {
		t.Fatalf("подключение к Postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	migration, err := os.ReadFile(filepath.Join(
		migrationsDir, "000010_external_campaigns_and_analytics.up.sql",
	))
	if err != nil {
		t.Fatalf("чтение campaign migration: %v", err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("применение campaign migration: %v", err)
	}

	companyID, partnerID, adminID := uuid.New(), uuid.New(), uuid.New()
	partnerCourseID, partnerVersionID, partnerLessonID := createPublishedCampaignCourse(
		t, ctx, pool, companyID, partnerID, "partner",
	)
	companyCourseID, companyVersionID, _ := createPublishedCampaignCourse(
		t, ctx, pool, companyID, adminID, "company",
	)

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	partnerCampaignID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO external_campaigns (
			id, company_id, course_id, course_version_id, owner_type,
			owner_user_id, purpose, name, deadline_days, token_hash,
			token_prefix, created_by_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'partner', $5, 'partner_promo',
			'Летняя промокампания', 3, decode(repeat('11', 32), 'hex'),
			'promo_11', $5, $6, $6)`,
		partnerCampaignID, companyID, partnerCourseID, partnerVersionID,
		partnerID, createdAt)
	if err != nil {
		t.Fatalf("создание партнёрской кампании: %v", err)
	}

	if _, err = pool.Exec(ctx, `
		INSERT INTO external_campaigns (
			company_id, course_id, course_version_id, owner_type,
			owner_user_id, purpose, name, deadline_days, token_hash,
			token_prefix, created_by_id
		) VALUES ($1, $2, $3, 'partner', $4, 'partner_promo',
			'Чужая', 3, decode(repeat('22', 32), 'hex'), 'promo_22', $4)`,
		companyID, partnerCourseID, partnerVersionID, uuid.New()); err == nil ||
		!strings.Contains(err.Error(), "ownership mismatch") {
		t.Fatalf("кампания чужого партнёра принята: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO external_campaigns (
			company_id, course_id, course_version_id, owner_type,
			purpose, name, deadline_days, token_hash, token_prefix, created_by_id
		) VALUES ($1, $2, $3, 'company', 'company_candidate',
			'Кандидаты', 8, decode(repeat('33', 32), 'hex'), 'candidate', $4)`,
		companyID, companyCourseID, companyVersionID, adminID); err == nil {
		t.Fatal("deadlineDays=8 ошибочно принят")
	}

	var resolvedCompanyID uuid.UUID
	if err = pool.QueryRow(ctx, `
		SELECT company_id FROM external_campaigns
		WHERE token_hash = decode(repeat('11', 32), 'hex')`).
		Scan(&resolvedCompanyID); err != nil || resolvedCompanyID != companyID {
		t.Fatalf("глобальное token resolution: company=%s err=%v",
			resolvedCompanyID, err)
	}

	learnerID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO external_learners (
			id, company_id, email, normalized_email, email_verified_at,
			created_at, updated_at
		) VALUES ($1, $2, 'candidate@example.com', 'candidate@example.com',
			$3, $3, $3)`,
		learnerID, companyID, createdAt)
	if err != nil {
		t.Fatalf("создание внешнего ученика: %v", err)
	}

	activate := func(proposedID uuid.UUID) uuid.UUID {
		t.Helper()
		var enrollmentID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO course_enrollments (
				id, company_id, course_id, course_version_id, learner_type,
				external_learner_id, source_type, source_id, attempt_number,
				progress_status, access_status, current_lesson_version_id,
				activated_at, access_until, started_at, last_activity_at
			) VALUES ($1, $2, $3, $4, 'external', $5,
				'partner_promo_campaign', $6, 1, 'in_progress', 'active', $7,
				$8, $8::timestamptz + interval '72 hours', $8, $8)
			ON CONFLICT (company_id, source_id, external_learner_id)
				WHERE learner_type = 'external'
				  AND source_type IN (
				      'partner_promo_campaign', 'company_candidate_campaign'
				  )
			DO UPDATE SET updated_at = course_enrollments.updated_at
			RETURNING id`, proposedID, companyID, partnerCourseID,
			partnerVersionID, learnerID, partnerCampaignID, partnerLessonID,
			createdAt.Add(time.Minute)).Scan(&enrollmentID)
		if err != nil {
			t.Fatalf("активация кампании: %v", err)
		}
		return enrollmentID
	}
	firstEnrollmentID := activate(uuid.New())
	secondEnrollmentID := activate(uuid.New())
	if firstEnrollmentID != secondEnrollmentID {
		t.Fatalf("повторная активация создала дубль: %s != %s",
			firstEnrollmentID, secondEnrollmentID)
	}

	otherCampaignID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO external_campaigns (
			id, company_id, course_id, course_version_id, owner_type,
			owner_user_id, purpose, name, deadline_days, token_hash,
			token_prefix, created_by_id
		) VALUES ($1, $2, $3, $4, 'partner', $5, 'partner_promo',
			'Вторая кампания', 3, decode(repeat('44', 32), 'hex'),
			'promo_44', $5)`, otherCampaignID, companyID, partnerCourseID,
		partnerVersionID, partnerID)
	if err != nil {
		t.Fatalf("создание второй кампании: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO course_enrollments (
			company_id, course_id, course_version_id, learner_type,
			external_learner_id, source_type, source_id, attempt_number,
			progress_status, access_status, activated_at, access_until
		) VALUES ($1, $2, $3, 'external', $4, 'partner_promo_campaign',
			$5, 1, 'in_progress', 'active', $6,
			$6::timestamptz + interval '3 days')`,
		companyID, partnerCourseID, partnerVersionID, learnerID,
		otherCampaignID, createdAt); err != nil {
		t.Fatalf("другая кампания не создала отдельное прохождение: %v", err)
	}

	eventID := uuid.New()
	insertEvent := func(proposedID uuid.UUID) uuid.UUID {
		t.Helper()
		var storedID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO analytics_events (
				id, company_id, campaign_id, enrollment_id,
				external_learner_id, event_type, event_idempotency_key,
				request_hash, visitor_hash, visitor_hash_key_id,
				request_ip_hash, request_ip_hash_key_id,
				utm_source, utm_medium, referrer, occurred_at
			) VALUES ($1, $2, $3, $4, $5, 'course_activated', 'event-1',
				repeat('a', 64), decode(repeat('55', 32), 'hex'), 'visitor-v1',
				decode(repeat('66', 32), 'hex'), 'ip-2026-07-22',
				'newsletter', 'email', 'https://example.test/jobs', $6)
			ON CONFLICT (company_id, campaign_id, event_idempotency_key)
			DO UPDATE SET id = analytics_events.id
			RETURNING id`, proposedID, companyID, partnerCampaignID,
			firstEnrollmentID, learnerID, createdAt.Add(2*time.Minute)).Scan(&storedID)
		if err != nil {
			t.Fatalf("запись analytics event: %v", err)
		}
		return storedID
	}
	if got := insertEvent(eventID); got != eventID {
		t.Fatalf("потерян event id: %s", got)
	}
	if got := insertEvent(uuid.New()); got != eventID {
		t.Fatalf("idempotent ingest вернул другой event: %s", got)
	}

	var analyticsCount int
	var utmSource, referrer string
	err = pool.QueryRow(ctx, `
		SELECT count(*), min(utm_source), min(referrer)
		FROM analytics_events WHERE campaign_id = $1`, partnerCampaignID).
		Scan(&analyticsCount, &utmSource, &referrer)
	if err != nil || analyticsCount != 1 || utmSource != "newsletter" ||
		referrer != "https://example.test/jobs" {
		t.Fatalf("неверные analytics/UTM: count=%d source=%q referrer=%q err=%v",
			analyticsCount, utmSource, referrer, err)
	}
	var rawIPColumns int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'analytics_events'
		  AND column_name IN ('ip', 'ip_address', 'raw_ip', 'request_ip')`).
		Scan(&rawIPColumns)
	if err != nil || rawIPColumns != 0 {
		t.Fatalf("analytics содержит raw IP column: count=%d err=%v",
			rawIPColumns, err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO analytics_events (
			company_id, campaign_id, event_type, event_idempotency_key,
			request_hash, visitor_hash, occurred_at
		) VALUES ($1, $2, 'landing_viewed', 'bad-hash-shape',
			repeat('b', 64), decode(repeat('77', 32), 'hex'), $3)`,
		companyID, partnerCampaignID, createdAt); err == nil {
		t.Fatal("visitor hash без key id ошибочно принят")
	}

	historyID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO external_campaign_history (
			id, company_id, campaign_id, event_type, actor_type, actor_id,
			previous_status, current_status, occurred_at
		) VALUES ($1, $2, $3, 'created', 'internal', $4,
			NULL, 'active', $5)`, historyID, companyID, partnerCampaignID,
		partnerID, createdAt)
	if err != nil {
		t.Fatalf("запись истории кампании: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE external_campaign_history SET details = '{"changed":true}'
		WHERE id = $1`, historyID); err == nil ||
		!strings.Contains(err.Error(), "history is immutable") {
		t.Fatalf("история кампании изменяема: %v", err)
	}

	deletedAt := createdAt.Add(time.Hour)
	_, err = pool.Exec(ctx, `
		UPDATE courses
		SET lifecycle_status = 'deleted', deleted_at = $1, deleted_by_id = $2
		WHERE company_id = $3 AND id = $4`,
		deletedAt, adminID, companyID, partnerCourseID)
	if err != nil {
		t.Fatalf("soft delete курса: %v", err)
	}
	var lifecycle, tombstoneTitle string
	var retainedEvents, retainedEnrollments int
	err = pool.QueryRow(ctx, `
		SELECT course.lifecycle_status,
		       CASE WHEN course.lifecycle_status = 'deleted'
		            THEN 'Удалённый курс' ELSE version.title END,
		       (SELECT count(*) FROM analytics_events WHERE campaign_id = campaign.id),
		       (SELECT count(*) FROM course_enrollments WHERE source_id = campaign.id)
		FROM external_campaigns AS campaign
		JOIN courses AS course ON course.company_id = campaign.company_id
		 AND course.id = campaign.course_id
		JOIN course_versions AS version ON version.id = campaign.course_version_id
		WHERE campaign.company_id = $1 AND campaign.id = $2`,
		companyID, partnerCampaignID).Scan(
		&lifecycle, &tombstoneTitle, &retainedEvents, &retainedEnrollments,
	)
	if err != nil || lifecycle != "deleted" || tombstoneTitle != "Удалённый курс" ||
		retainedEvents != 1 || retainedEnrollments != 1 {
		t.Fatalf("статистика не сохранена после delete: lifecycle=%q title=%q events=%d enrollments=%d err=%v",
			lifecycle, tombstoneTitle, retainedEvents, retainedEnrollments, err)
	}
}

func createPublishedCampaignCourse(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID uuid.UUID,
	actorID uuid.UUID,
	ownerType string,
) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	courseID, versionID := uuid.New(), uuid.New()
	sectionID, lessonID := uuid.New(), uuid.New()
	var ownerUserID any
	if ownerType == "partner" {
		ownerUserID = actorID
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, sequential,
			visibility, owner_type, owner_user_id, created_by_id
		) VALUES ($1, $2, 'Курс кампании', 'draft', $3, true,
			'restricted', $4, $5, $3);
		INSERT INTO course_versions (
			id, company_id, course_id, number, status, title,
			sequential, created_by_id
		) VALUES ($6, $2, $1, 1, 'draft', 'Курс кампании', true, $3);
		UPDATE courses SET current_draft_version_id = $6 WHERE id = $1;
		INSERT INTO course_version_sections (
			id, company_id, course_version_id, title, "order"
		) VALUES ($7, $2, $6, 'Раздел', 0);
		INSERT INTO course_version_lessons (
			id, company_id, course_version_id, section_version_id,
			title, "order", content
		) VALUES ($8, $2, $6, $7, 'Урок', 0,
			'{"type":"doc","content":[]}'::jsonb);
		UPDATE course_versions
		SET status = 'published', published_by_id = $3, published_at = now(),
			content_hash = repeat('c', 64)
		WHERE id = $6;
		UPDATE courses
		SET status = 'published', current_draft_version_id = NULL,
			latest_published_version_id = $6
		WHERE id = $1`,
		courseID, companyID, actorID, ownerType, ownerUserID,
		versionID, sectionID, lessonID)
	if err != nil {
		t.Fatalf("подготовка %s курса: %v", ownerType, err)
	}
	return courseID, versionID, lessonID
}
