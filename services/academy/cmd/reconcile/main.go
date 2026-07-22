// Command reconcile verifies that the immutable Academy model contains a
// lossless projection of the legacy courses, progress and quiz attempts. It is
// intended as a release gate while all Academy writes are frozen, immediately
// after migration and before either legacy or new command traffic resumes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

type reconciliationReport struct {
	Ready                           bool  `json:"ready"`
	LegacyCoursesTotal              int64 `json:"legacyCoursesTotal"`
	VersionedCoursesTotal           int64 `json:"versionedCoursesTotal"`
	CoursesWithoutVersion           int64 `json:"coursesWithoutVersion"`
	CoursesWithMultipleDrafts       int64 `json:"coursesWithMultipleDrafts"`
	AssignmentsTotal                int64 `json:"assignmentsTotal"`
	AssignmentsWithoutVersion       int64 `json:"assignmentsWithoutVersion"`
	LegacyProgressTotal             int64 `json:"legacyProgressTotal"`
	LegacyProgressWithoutEnrollment int64 `json:"legacyProgressWithoutEnrollment"`
	ProgressStatusMismatches        int64 `json:"progressStatusMismatches"`
	CompletedLessonMismatches       int64 `json:"completedLessonMismatches"`
	LegacyQuizAttemptsTotal         int64 `json:"legacyQuizAttemptsTotal"`
	QuizAttemptBindingMismatches    int64 `json:"quizAttemptBindingMismatches"`
	TenantScopeMismatches           int64 `json:"tenantScopeMismatches"`
	DeprecatedPublicCoursesTotal    int64 `json:"deprecatedPublicCoursesTotal"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		log.Printf("academy reconcile: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, output io.Writer) error {
	databaseURL := strings.TrimSpace(getenv("ACADEMY_DB_URL"))
	if databaseURL == "" {
		return errors.New("ACADEMY_DB_URL не задан")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("настроить PostgreSQL: %w", err)
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return fmt.Errorf("подключиться к PostgreSQL: %w", err)
	}

	row, err := db.New(pool).GetAcademyCutoverReconciliation(ctx)
	if err != nil {
		return fmt.Errorf("выполнить сверку Академии: %w", err)
	}
	report := reportFromRow(row)
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(report); err != nil {
		return fmt.Errorf("вывести результат сверки: %w", err)
	}
	if !report.Ready {
		return errors.New("сверка обнаружила расхождения; переключение остановлено")
	}
	return nil
}

func reportFromRow(row db.GetAcademyCutoverReconciliationRow) reconciliationReport {
	report := reconciliationReport{
		LegacyCoursesTotal:              row.LegacyCoursesTotal,
		VersionedCoursesTotal:           row.VersionedCoursesTotal,
		CoursesWithoutVersion:           row.CoursesWithoutVersion,
		CoursesWithMultipleDrafts:       row.CoursesWithMultipleDrafts,
		AssignmentsTotal:                row.AssignmentsTotal,
		AssignmentsWithoutVersion:       row.AssignmentsWithoutVersion,
		LegacyProgressTotal:             row.LegacyProgressTotal,
		LegacyProgressWithoutEnrollment: row.LegacyProgressWithoutEnrollment,
		ProgressStatusMismatches:        row.ProgressStatusMismatches,
		CompletedLessonMismatches:       row.CompletedLessonMismatches,
		LegacyQuizAttemptsTotal:         row.LegacyQuizAttemptsTotal,
		QuizAttemptBindingMismatches:    row.QuizAttemptBindingMismatches,
		TenantScopeMismatches:           row.TenantScopeMismatches,
		DeprecatedPublicCoursesTotal:    row.DeprecatedPublicCoursesTotal,
	}
	report.Ready = report.isReady()
	return report
}

func (report reconciliationReport) isReady() bool {
	return report.LegacyCoursesTotal == report.VersionedCoursesTotal &&
		report.CoursesWithoutVersion == 0 &&
		report.CoursesWithMultipleDrafts == 0 &&
		report.AssignmentsWithoutVersion == 0 &&
		report.LegacyProgressWithoutEnrollment == 0 &&
		report.ProgressStatusMismatches == 0 &&
		report.CompletedLessonMismatches == 0 &&
		report.QuizAttemptBindingMismatches == 0 &&
		report.TenantScopeMismatches == 0
}
