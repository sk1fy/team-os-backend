//go:build integration

package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func TestFileCloneJobsGatePublicationAndRemainRetryableAfterRewriteRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := academyTestPool(t, ctx)
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("создание сервиса: %v", err)
	}

	actor := Actor{CompanyID: uuid.New(), UserID: uuid.New(), Role: "admin"}
	course, err := service.CreateCourse(ctx, actor, CreateCourseInput{Title: "Курс с копированием файлов"})
	if err != nil {
		t.Fatalf("создание курса: %v", err)
	}
	if course.CurrentDraftVersionID == nil {
		t.Fatal("у нового курса нет черновика")
	}
	draftID := *course.CurrentDraftVersionID
	queries := db.New(pool)
	sections, err := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{
		CompanyID: actor.CompanyID, CourseVersionID: draftID,
	})
	if err != nil || len(sections) != 1 {
		t.Fatalf("разделы черновика: sections=%v err=%v", sections, err)
	}
	lesson, err := service.CreateCourseVersionLesson(ctx, actor, CreateCourseVersionLessonInput{
		VersionID: draftID, SectionVersionID: sections[0].ID,
		Title: "Урок", Content: json.RawMessage(`{"type":"doc"}`),
	})
	if err != nil {
		t.Fatalf("создание урока: %v", err)
	}

	sourceFileID, clonedFileID := uuid.New(), uuid.New()
	if _, err = pool.Exec(ctx, `
		UPDATE course_version_lessons SET file_ids = ARRAY[$1]::uuid[] WHERE id = $2`,
		sourceFileID, lesson.ID); err != nil {
		t.Fatalf("подготовка файла урока: %v", err)
	}
	jobID := insertFileCloneJobForTest(t, ctx, pool, actor.CompanyID, course.ID, draftID, sourceFileID, "pending", "publish-gate")

	if _, err = service.PublishCourseVersion(ctx, actor, course.ID, "publish-pending"); !isApplicationError(err, ErrorConflict) {
		t.Fatalf("pending job не заблокировал публикацию: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE academy_file_clone_jobs
		SET status = 'failed', last_error = 'Временная ошибка', updated_at = now()
		WHERE id = $1`, jobID); err != nil {
		t.Fatalf("перевод задания в failed: %v", err)
	}
	if _, err = service.PublishCourseVersion(ctx, actor, course.ID, "publish-failed"); !isApplicationError(err, ErrorConflict) {
		t.Fatalf("failed job не заблокировал публикацию: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE academy_file_clone_jobs
		SET status = 'running', last_error = NULL, attempts = attempts + 1, updated_at = now()
		WHERE id = $1`, jobID); err != nil {
		t.Fatalf("повторный запуск задания: %v", err)
	}
	job, err := queries.GetFileCloneJobByIdempotencyKey(ctx, db.GetFileCloneJobByIdempotencyKeyParams{
		CompanyID: actor.CompanyID, OperationType: "template_instantiate", IdempotencyKey: "publish-gate",
	})
	if err != nil {
		t.Fatalf("чтение задания: %v", err)
	}
	items, err := queries.ListFileCloneJobItems(ctx, db.ListFileCloneJobItemsParams{
		CompanyID: actor.CompanyID, JobID: job.ID,
	})
	if err != nil {
		t.Fatalf("чтение файлов задания: %v", err)
	}
	if err = service.applyClonedFiles(ctx, job, items, map[uuid.UUID]uuid.UUID{
		sourceFileID: clonedFileID,
	}); err != nil {
		t.Fatalf("применение копий к черновику: %v", err)
	}
	if _, err = service.PublishCourseVersion(ctx, actor, course.ID, "publish-completed"); err != nil {
		t.Fatalf("completed job не разрешил публикацию: %v", err)
	}

	secondCloneID := uuid.New()
	secondJobID := insertFileCloneJobForTest(t, ctx, pool, actor.CompanyID, course.ID, draftID, clonedFileID, "running", "rewrite-race")
	secondJob, err := queries.GetFileCloneJobByIdempotencyKey(ctx, db.GetFileCloneJobByIdempotencyKeyParams{
		CompanyID: actor.CompanyID, OperationType: "template_instantiate", IdempotencyKey: "rewrite-race",
	})
	if err != nil {
		t.Fatalf("чтение второго задания: %v", err)
	}
	secondItems, err := queries.ListFileCloneJobItems(ctx, db.ListFileCloneJobItemsParams{
		CompanyID: actor.CompanyID, JobID: secondJobID,
	})
	if err != nil {
		t.Fatalf("чтение файлов второго задания: %v", err)
	}
	rewriteErr := service.applyClonedFiles(ctx, secondJob, secondItems, map[uuid.UUID]uuid.UUID{
		clonedFileID: secondCloneID,
	})
	if rewriteErr == nil {
		t.Fatal("нулевой rewrite опубликованной версии был принят за успех")
	}
	if err = service.retryFileCloneJob(ctx, secondJob, rewriteErr); err != nil {
		t.Fatalf("перенос задания для повтора: %v", err)
	}

	var jobStatus, itemStatus string
	var completedAt *time.Time
	var lastError *string
	err = pool.QueryRow(ctx, `
		SELECT job.status, job.completed_at, job.last_error, item.status
		FROM academy_file_clone_jobs AS job
		JOIN academy_file_clone_job_items AS item ON item.job_id = job.id
		WHERE job.id = $1`, secondJobID).Scan(&jobStatus, &completedAt, &lastError, &itemStatus)
	if err != nil {
		t.Fatalf("чтение состояния задания после гонки: %v", err)
	}
	if jobStatus != "failed" || completedAt != nil || lastError == nil || itemStatus != "pending" {
		t.Fatalf("задание тихо завершилось: job=%s completedAt=%v lastError=%v item=%s",
			jobStatus, completedAt, lastError, itemStatus)
	}
}

func insertFileCloneJobForTest(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID, aggregateID, targetVersionID, sourceFileID uuid.UUID,
	status, idempotencyKey string,
) uuid.UUID {
	t.Helper()
	jobID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO academy_file_clone_jobs (
			id, company_id, operation_type, aggregate_id, idempotency_key,
			source_owner_type, source_owner_id, target_owner_type,
			target_owner_id, status
		) VALUES ($1, $2, 'template_instantiate', $3, $4,
			'template_version', $5, 'course_version', $6, $7);
			INSERT INTO academy_file_clone_job_items (
				company_id, job_id, source_file_id, status
			) VALUES ($2, $1, $8, 'pending')`,
		pgx.QueryExecModeSimpleProtocol,
		jobID, companyID, aggregateID, idempotencyKey, uuid.New(), targetVersionID, status, sourceFileID); err != nil {
		t.Fatalf("создание задания копирования: %v", err)
	}
	return jobID
}
