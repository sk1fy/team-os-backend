package application

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) ProcessFileCloneJobs(ctx context.Context) error {
	if s.files == nil {
		return nil
	}
	queries := db.New(s.pool)
	now := s.now().UTC()
	if _, err := queries.RequeueStaleFileCloneJobs(ctx, db.RequeueStaleFileCloneJobsParams{
		LastError:     pgtype.Text{String: "Задача копирования файлов не завершилась вовремя", Valid: true},
		NextAttemptAt: now, UpdatedAt: now, StaleBefore: now.Add(-10 * time.Minute),
	}); err != nil {
		return internal("Не удалось вернуть зависшие задачи копирования файлов", err)
	}
	jobs, err := queries.ClaimFileCloneJobs(ctx, db.ClaimFileCloneJobsParams{ClaimedAt: now, BatchSize: 20})
	if err != nil {
		return internal("Не удалось получить задачи копирования файлов", err)
	}
	for _, job := range jobs {
		if err = s.processFileCloneJob(ctx, job); err != nil {
			s.logger.Warn("не удалось скопировать файлы Academy", "jobId", job.ID, "error", err)
			if retryErr := s.retryFileCloneJob(ctx, job, err); retryErr != nil {
				return retryErr
			}
		}
	}
	return nil
}

func (s *Service) processFileCloneJob(ctx context.Context, job db.AcademyFileCloneJob) error {
	queries := db.New(s.pool)
	items, err := queries.ListFileCloneJobItems(ctx, db.ListFileCloneJobItemsParams{
		CompanyID: job.CompanyID, JobID: job.ID,
	})
	if err != nil {
		return fmt.Errorf("получить файлы задачи: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("задача не содержит файлов")
	}
	sourceIDs := make([]uuid.UUID, len(items))
	for index := range items {
		sourceIDs[index] = items[index].SourceFileID
	}
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: job.CompanyID, ID: job.AggregateID})
	if err != nil {
		return fmt.Errorf("получить курс-владелец файлов: %w", err)
	}
	role := "admin"
	if courseRow.OwnerType == "partner" {
		role = "partner"
	}
	requestedBy := courseRow.AuthorID
	if courseRow.CreatedByID.Valid {
		requestedBy = courseRow.CreatedByID.UUID
	}
	result, err := s.files.CloneFilesForOwner(
		ctx, job.CompanyID, requestedBy, role, "academy-file-clone:"+job.ID.String(),
		job.TargetOwnerType, job.TargetOwnerID, sourceIDs,
	)
	if err != nil {
		return err
	}
	if result.State != "succeeded" {
		return fmt.Errorf("Files ещё не завершил копирование: %s", result.State)
	}
	if len(result.Files) != len(sourceIDs) {
		return fmt.Errorf("Files вернул неполное соответствие копий")
	}
	return s.applyClonedFiles(ctx, job, items, result.Files)
}

func (s *Service) applyClonedFiles(
	ctx context.Context,
	job db.AcademyFileCloneJob,
	items []db.AcademyFileCloneJobItem,
	mapping map[uuid.UUID]uuid.UUID,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("начать применение копий: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	version, err := queries.GetCourseVersion(ctx, db.GetCourseVersionParams{CompanyID: job.CompanyID, ID: job.TargetOwnerID})
	if err != nil {
		return fmt.Errorf("получить целевую версию: %w", err)
	}
	if version.CoverFileID.Valid {
		if clonedID, exists := mapping[version.CoverFileID.UUID]; exists {
			affected, rewriteErr := queries.SetCourseVersionClonedCoverFile(ctx, db.SetCourseVersionClonedCoverFileParams{
				CoverFileID: nullUUID(&clonedID), CompanyID: job.CompanyID, CourseVersionID: version.ID,
			})
			if err = requireSingleFileCloneRewrite(affected, rewriteErr, "заменить обложку версии"); err != nil {
				return err
			}
		}
	}
	lessons, err := queries.GetCourseVersionLessons(ctx, db.GetCourseVersionLessonsParams{
		CompanyID: job.CompanyID, CourseVersionID: version.ID,
	})
	if err != nil {
		return fmt.Errorf("получить уроки целевой версии: %w", err)
	}
	replacements := make(map[string]string, len(mapping))
	for sourceID, targetID := range mapping {
		replacements[sourceID.String()] = targetID.String()
	}
	for _, lesson := range lessons {
		changed := false
		fileIDs := append([]uuid.UUID(nil), lesson.FileIds...)
		for index, sourceID := range fileIDs {
			if targetID, exists := mapping[sourceID]; exists {
				fileIDs[index] = targetID
				changed = true
			}
		}
		if !changed {
			continue
		}
		content, replaceErr := richtext.ReplaceFileIDs(lesson.Content, replacements)
		if replaceErr != nil {
			return fmt.Errorf("заменить ссылки на файлы в уроке: %w", replaceErr)
		}
		affected, rewriteErr := queries.SetCourseVersionLessonClonedFiles(ctx, db.SetCourseVersionLessonClonedFilesParams{
			Content: content, FileIds: fileIDs, CompanyID: job.CompanyID, LessonID: lesson.ID,
		})
		if err = requireSingleFileCloneRewrite(affected, rewriteErr, "сохранить ссылки на копии файлов"); err != nil {
			return err
		}
	}
	now := s.now().UTC()
	for _, item := range items {
		targetID, exists := mapping[item.SourceFileID]
		if !exists {
			return fmt.Errorf("для файла %s отсутствует копия", item.SourceFileID)
		}
		if item.Status == "completed" && item.TargetFileID.Valid && item.TargetFileID.UUID == targetID {
			continue
		}
		if _, err = queries.CompleteFileCloneJobItem(ctx, db.CompleteFileCloneJobItemParams{
			TargetFileID: nullUUID(&targetID), UpdatedAt: now, CompanyID: job.CompanyID,
			JobID: job.ID, SourceFileID: item.SourceFileID,
		}); err != nil {
			return fmt.Errorf("зафиксировать копию файла: %w", err)
		}
	}
	if _, err = queries.CompleteFileCloneJob(ctx, db.CompleteFileCloneJobParams{
		CompletedAt: now, CompanyID: job.CompanyID, ID: job.ID,
	}); err != nil {
		return fmt.Errorf("завершить задачу копирования файлов: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("сохранить копии файлов: %w", err)
	}
	return nil
}

func requireSingleFileCloneRewrite(affected int64, rewriteErr error, action string) error {
	if rewriteErr != nil {
		return fmt.Errorf("%s: %w", action, rewriteErr)
	}
	if affected != 1 {
		return fmt.Errorf("%s: целевая версия больше не является черновиком", action)
	}
	return nil
}

func (s *Service) retryFileCloneJob(ctx context.Context, job db.AcademyFileCloneJob, cause error) error {
	delayMinutes := math.Pow(2, float64(min(job.Attempts, int32(8))))
	now := s.now().UTC()
	_, err := db.New(s.pool).RetryFileCloneJob(ctx, db.RetryFileCloneJobParams{
		LastError:     pgtype.Text{String: cause.Error(), Valid: true},
		NextAttemptAt: now.Add(time.Duration(delayMinutes) * time.Minute), UpdatedAt: now,
		CompanyID: job.CompanyID, ID: job.ID,
	})
	if err != nil {
		return internal("Не удалось перенести задачу копирования файлов", err)
	}
	return nil
}
