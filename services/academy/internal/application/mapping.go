package application

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func courseFromRow(row db.Course) Course {
	return Course{
		ID: row.ID, CompanyID: row.CompanyID, Title: row.Title,
		Description:  textPointer(row.Description),
		CoverURL:     textPointer(row.CoverUrl),
		Status:       row.Status,
		AuthorID:     row.AuthorID,
		Sequential:   row.Sequential,
		DeadlineDays: int4Pointer(row.DeadlineDays),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func coursesFromRows(rows []db.Course) []Course {
	result := make([]Course, len(rows))
	for index := range rows {
		result[index] = courseFromRow(rows[index])
	}
	return result
}

func sectionFromRow(row db.CourseSection) CourseSection {
	return CourseSection{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		Title: row.Title, Order: row.Order,
	}
}

func lessonFromRow(row db.Lesson) Lesson {
	return Lesson{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		SectionID: row.SectionID, Title: row.Title, Order: row.Order,
		Content:         append(json.RawMessage(nil), row.Content...),
		SourceArticleID: nullUUIDPointer(row.SourceArticleID),
		SourceMode:      textPointer(row.SourceMode),
		QuizID:          nullUUIDPointer(row.QuizID),
	}
}

func lessonsFromRows(rows []db.Lesson) []Lesson {
	result := make([]Lesson, len(rows))
	for index := range rows {
		result[index] = lessonFromRow(rows[index])
	}
	return result
}

func quizFromRow(row db.Quiz) Quiz {
	return Quiz{
		ID: row.ID, CompanyID: row.CompanyID, LessonID: row.LessonID,
		Questions:    append(json.RawMessage(nil), row.Questions...),
		PassingScore: row.PassingScore,
		MaxAttempts:  int4Pointer(row.MaxAttempts),
	}
}

func quizzesFromRows(rows []db.Quiz) []Quiz {
	result := make([]Quiz, len(rows))
	for index := range rows {
		result[index] = quizFromRow(rows[index])
	}
	return result
}

func assignmentFromRow(row db.Assignment) Assignment {
	return Assignment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		AssigneeType: row.AssigneeType,
		AssigneeID:   nullUUIDPointer(row.AssigneeID),
		InviteToken:  textPointer(row.InviteToken),
		DueDate:      timestamptzPointer(row.DueDate),
		AssignedByID: row.AssignedByID,
		CreatedAt:    row.CreatedAt,
	}
}

func assignmentsFromRows(rows []db.Assignment) []Assignment {
	result := make([]Assignment, len(rows))
	for index := range rows {
		result[index] = assignmentFromRow(rows[index])
	}
	return result
}

func progressFromRow(row db.Progress, attempts []QuizAttempt) Progress {
	if attempts == nil {
		attempts = []QuizAttempt{}
	}
	return Progress{
		UserID: row.UserID, CourseID: row.CourseID, Status: row.Status,
		CompletedLessonIDs: append([]uuid.UUID{}, row.CompletedLessonIds...),
		QuizAttempts:       attempts,
		StartedAt:          timestamptzPointer(row.StartedAt),
		CompletedAt:        timestamptzPointer(row.CompletedAt),
	}
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func int4Pointer(value pgtype.Int4) *int32 {
	if !value.Valid {
		return nil
	}
	result := value.Int32
	return &result
}

func boolNull(value *bool) pgtype.Bool {
	if value == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *value, Valid: true}
}

func nullInt4(value *int32) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *value, Valid: true}
}

func nullUUIDPointer(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := value.UUID
	return &result
}

func nullUUID(value *uuid.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *value, Valid: true}
}

func timestamptzPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func nullTimestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
