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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestExternalAccessMigrationIsolationDeadlinesAndHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	companyID, partnerID := uuid.New(), uuid.New()
	courseID, versionID, sectionID, lessonID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, sequential,
			visibility, owner_type, owner_user_id, created_by_id
		) VALUES ($1, $2, 'Курс партнёра', 'draft', $3, true,
			'restricted', 'partner', $3, $3);
		INSERT INTO course_versions (
			id, company_id, course_id, number, status, title,
			sequential, created_by_id
		) VALUES ($4, $2, $1, 1, 'draft', 'Курс партнёра', true, $3);
		UPDATE courses SET current_draft_version_id = $4 WHERE id = $1;
		INSERT INTO course_version_sections (
			id, company_id, course_version_id, title, "order"
		) VALUES ($5, $2, $4, 'Раздел', 0);
		INSERT INTO course_version_lessons (
			id, company_id, course_version_id, section_version_id,
			title, "order", content
		) VALUES ($6, $2, $4, $5, 'Урок', 0,
			'{"type":"doc","content":[]}'::jsonb);
		UPDATE course_versions
		SET status = 'published', published_by_id = $3, published_at = now(),
			content_hash = repeat('a', 64)
		WHERE id = $4;
		UPDATE courses
		SET status = 'published', current_draft_version_id = NULL,
			latest_published_version_id = $4
		WHERE id = $1`,
		courseID, companyID, partnerID, versionID, sectionID, lessonID)
	if err != nil {
		t.Fatalf("подготовка опубликованного курса: %v", err)
	}

	migration, err := os.ReadFile(filepath.Join(
		migrationsDir, "000009_external_learners_and_personal_accesses.up.sql",
	))
	if err != nil {
		t.Fatalf("чтение external migration: %v", err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("применение external migration: %v", err)
	}

	learnerID := uuid.New()
	verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `
		INSERT INTO external_learners (
			id, company_id, email, normalized_email, email_verified_at,
			created_at, updated_at
		) VALUES ($1, $2, ' Ivan+Candidate@Example.COM ',
			'ivan+candidate@example.com', $3, $3, $3)`,
		learnerID, companyID, verifiedAt)
	if err != nil {
		t.Fatalf("создание внешнего профиля без имени: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO external_learners (
			company_id, email, normalized_email
		) VALUES ($1, 'ivan+candidate@example.com', 'ivan+candidate@example.com')`,
		companyID); err == nil {
		t.Fatal("уникальность нормализованного email не сработала")
	}
	otherCompanyID := uuid.New()
	if _, err = pool.Exec(ctx, `
		INSERT INTO external_learners (
			company_id, email, normalized_email
		) VALUES ($1, 'ivan+candidate@example.com', 'ivan+candidate@example.com')`,
		otherCompanyID); err != nil {
		t.Fatalf("email ошибочно оказался глобально уникальным: %v", err)
	}

	accessID := uuid.New()
	issuedAt := verifiedAt.Add(time.Second)
	_, err = pool.Exec(ctx, `
		INSERT INTO external_personal_accesses (
			id, company_id, course_id, course_version_id, partner_owner_id,
			external_learner_id, expected_email, normalized_expected_email,
			deadline_days, token_hash, token_prefix, root_access_id,
			issuance_idempotency_key, issued_by_id, issued_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6,
			'Ivan+Candidate@Example.COM', 'ivan+candidate@example.com',
			3, decode(repeat('11', 32), 'hex'), 'public_1', $1,
			'issue-1', $5, $7, $7)`, accessID, companyID, courseID,
		versionID, partnerID, learnerID, issuedAt)
	if err != nil {
		t.Fatalf("создание персонального доступа: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO external_personal_accesses (
			id, company_id, course_id, course_version_id, partner_owner_id,
			expected_email, normalized_expected_email, deadline_days,
			token_hash, token_prefix, root_access_id,
			issuance_idempotency_key, issued_by_id
		) VALUES ($1, $2, $3, $4, $5, 'x@example.com', 'x@example.com', 2,
			decode(repeat('22', 32), 'hex'), 'public_2', $1, 'issue-2', $5)`,
		uuid.New(), companyID, courseID, versionID, uuid.New()); err == nil ||
		!strings.Contains(err.Error(), "ownership mismatch") {
		t.Fatalf("доступ чужого партнёра принят: %v", err)
	}

	challengeID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO external_verification_challenges (
			id, company_id, normalized_email, purpose, source_id,
			code_hash, expires_at, created_at
		) VALUES ($1, $2, 'ivan+candidate@example.com', 'personal_access', $3,
			decode(repeat('33', 32), 'hex'),
			$4::timestamptz + interval '10 minutes', $4)`,
		challengeID, companyID, accessID, issuedAt)
	if err != nil {
		t.Fatalf("создание verification challenge: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE external_verification_challenges SET attempts = 6 WHERE id = $1`,
		challengeID); err == nil {
		t.Fatal("лимит попыток challenge не сработал")
	}

	sessionID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO external_sessions (
			id, company_id, external_learner_id, token_hash,
			expires_at, last_used_at, created_at
		) VALUES ($1, $2, $3, decode(repeat('44', 32), 'hex'),
			$4::timestamptz + interval '24 hours', $4, $4)`,
		sessionID, companyID, learnerID, issuedAt)
	if err != nil {
		t.Fatalf("создание внешней сессии: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO external_sessions (
			company_id, external_learner_id, token_hash, expires_at
		) VALUES ($1, $2, decode(repeat('55', 32), 'hex'), now() + interval '1 day')`,
		otherCompanyID, learnerID); err == nil {
		t.Fatal("cross-tenant external session принята")
	}

	enrollmentID := uuid.New()
	activatedAt := issuedAt.Add(time.Minute)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("начало activation transaction: %v", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO course_enrollments (
			id, company_id, course_id, course_version_id, learner_type,
			external_learner_id, source_type, source_id, attempt_number,
			progress_status, access_status, current_lesson_version_id,
			activated_at, access_until, started_at, last_activity_at
		) VALUES ($1, $2, $3, $4, 'external', $5,
			'personal_access', $6, 1, 'in_progress', 'active', $7,
			$8, $8::timestamptz + interval '72 hours', $8, $8)`,
		enrollmentID, companyID, courseID, versionID, learnerID,
		accessID, lessonID, activatedAt)
	if err == nil {
		_, err = tx.Exec(ctx, `
			UPDATE external_personal_accesses
			SET status = 'activated', enrollment_id = $1,
				activated_at = $2, updated_at = $2
			WHERE company_id = $3 AND id = $4`,
			enrollmentID, activatedAt, companyID, accessID)
	}
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if err != nil {
		t.Fatalf("атомарная привязка enrollment: %v", err)
	}

	var deadlineHours float64
	err = pool.QueryRow(ctx, `
		SELECT extract(epoch FROM (access_until - activated_at)) / 3600
		FROM course_enrollments WHERE company_id = $1 AND id = $2`,
		companyID, enrollmentID).Scan(&deadlineHours)
	if err != nil || deadlineHours != 72 {
		t.Fatalf("deadline не равен 3*24 часа: hours=%v err=%v", deadlineHours, err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO course_enrollments (
			company_id, course_id, course_version_id, learner_type,
			external_learner_id, source_type, source_id, attempt_number,
			progress_status, access_status, activated_at, access_until
		) VALUES ($1, $2, $3, 'external', $4, 'personal_access', $5, 1,
			'in_progress', 'active', now(), now() + interval '1 day')`,
		companyID, courseID, versionID, learnerID, accessID); err == nil {
		t.Fatal("повторная активация создала второй enrollment")
	}

	historyID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO external_personal_access_history (
			id, company_id, personal_access_id, external_learner_id,
			enrollment_id, event_type, actor_type, actor_id,
			current_token_prefix, occurred_at
		) VALUES ($1, $2, $3, $4, $5, 'activated', 'external', $4,
			'public_1', $6)`, historyID, companyID, accessID, learnerID,
		enrollmentID, activatedAt)
	if err != nil {
		t.Fatalf("запись истории доступа: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE external_personal_access_history SET details = '{"changed":true}'
		WHERE id = $1`, historyID); err == nil ||
		!strings.Contains(err.Error(), "history is immutable") {
		t.Fatalf("история персонального доступа изменяема: %v", err)
	}
}
