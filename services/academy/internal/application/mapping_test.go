package application

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func TestLessonFromRowCopiesContent(t *testing.T) {
	articleID := uuid.New()
	content := []byte(`{"type":"doc"}`)
	row := db.Lesson{
		ID: uuid.New(), CompanyID: uuid.New(), CourseID: uuid.New(), SectionID: uuid.New(),
		Title: "Урок", Content: content,
		SourceArticleID: uuid.NullUUID{UUID: articleID, Valid: true},
		SourceMode:      pgtype.Text{String: "link", Valid: true},
	}

	lesson := lessonFromRow(row)
	content[0] = '['

	if string(lesson.Content) != `{"type":"doc"}` {
		t.Fatalf("content was aliased: %s", lesson.Content)
	}
	if lesson.SourceArticleID == nil || *lesson.SourceArticleID != articleID {
		t.Fatalf("source article ID = %v, want %s", lesson.SourceArticleID, articleID)
	}
	if lesson.SourceMode == nil || *lesson.SourceMode != "link" {
		t.Fatalf("source mode = %v, want link", lesson.SourceMode)
	}
}

func TestProgressFromRowCopiesCompletedLessons(t *testing.T) {
	completedLessonID := uuid.New()
	row := db.Progress{
		UserID: uuid.New(), CourseID: uuid.New(), Status: "in_progress",
		CompletedLessonIds: []uuid.UUID{completedLessonID},
	}

	progress := progressFromRow(row, nil)
	row.CompletedLessonIds[0] = uuid.Nil

	if len(progress.CompletedLessonIDs) != 1 || progress.CompletedLessonIDs[0] != completedLessonID {
		t.Fatalf("completed lessons were aliased: %#v", progress.CompletedLessonIDs)
	}
	if progress.QuizAttempts == nil || len(progress.QuizAttempts) != 0 {
		t.Fatalf("quiz attempts = %#v, want empty slice", progress.QuizAttempts)
	}
}
