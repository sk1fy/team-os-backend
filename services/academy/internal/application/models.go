package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Actor struct {
	UserID        uuid.UUID
	CompanyID     uuid.UUID
	Role          string
	PositionIDs   []uuid.UUID
	DepartmentIDs []uuid.UUID
	// Raw bearer token, forwarded to kb/company on synchronous RPC (§9).
	Token string
}

func (a Actor) canManage() bool {
	return a.Role == "owner" || a.Role == "admin"
}

type Course struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	Title        string
	Description  *string
	CoverURL     *string
	Status       string
	AuthorID     uuid.UUID
	Sequential   bool
	DeadlineDays *int32
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CourseSection struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	CourseID  uuid.UUID
	Title     string
	Order     int32
}

type Lesson struct {
	ID              uuid.UUID
	CompanyID       uuid.UUID
	CourseID        uuid.UUID
	SectionID       uuid.UUID
	Title           string
	Order           int32
	Content         json.RawMessage
	SourceArticleID *uuid.UUID
	SourceMode      *string
	QuizID          *uuid.UUID
}

type Quiz struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	LessonID     uuid.UUID
	Questions    json.RawMessage
	PassingScore int32
	MaxAttempts  *int32
}

type Assignment struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	CourseID     uuid.UUID
	AssigneeType string
	AssigneeID   *uuid.UUID
	InviteToken  *string
	DueDate      *time.Time
	AssignedByID uuid.UUID
	CreatedAt    time.Time
}

type QuizAttempt struct {
	ID            uuid.UUID
	QuizID        uuid.UUID
	UserID        uuid.UUID
	Score         int32
	Passed        bool
	PendingReview bool
	CreatedAt     time.Time
}

type Progress struct {
	UserID             uuid.UUID
	CourseID           uuid.UUID
	Status             string
	CompletedLessonIDs []uuid.UUID
	QuizAttempts       []QuizAttempt
	StartedAt          *time.Time
	CompletedAt        *time.Time
}

// KbArticle is the subset of a knowledge-base article academy copies into
// lessons; fetched over gRPC with the actor's forwarded token.
type KbArticle struct {
	ID        uuid.UUID
	SectionID uuid.UUID
	Title     string
	Content   json.RawMessage
}

type KbSection struct {
	ID   uuid.UUID
	Name string
}

type KbClient interface {
	GetArticle(ctx context.Context, token string, id uuid.UUID) (KbArticle, error)
	GetArticlesByIds(ctx context.Context, token string, ids []uuid.UUID) ([]KbArticle, error)
	GetSections(ctx context.Context, token string) ([]KbSection, error)
}

type CompanyClient interface {
	ResolvePositionUsers(ctx context.Context, token string, positionID uuid.UUID) ([]uuid.UUID, error)
	ResolveDepartmentUsers(ctx context.Context, token string, departmentID uuid.UUID) ([]uuid.UUID, error)
}
