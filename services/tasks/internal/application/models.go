package application

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	domainrecurrence "github.com/sk1fy/team-os-backend/services/tasks/internal/domain/recurrence"
)

type Actor struct {
	UserID        uuid.UUID
	CompanyID     uuid.UUID
	Role          string
	PositionIDs   []uuid.UUID
	DepartmentIDs []uuid.UUID
}

type Board struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	Name         string
	Type         string
	DepartmentID *uuid.UUID
	OwnerID      *uuid.UUID
	CreatedAt    time.Time
}

type TaskColumn struct {
	ID      uuid.UUID
	BoardID uuid.UUID
	Name    string
	Order   int32
	Color   *string
}

type Label struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	Name      string
	Color     string
}

type ChecklistItem struct {
	ID   uuid.UUID
	Text string
	Done bool
}

type Attachment struct {
	ID       uuid.UUID
	Name     string
	URL      string
	Size     int64
	MimeType string
}

type TaskSource struct {
	Type       string
	Title      string
	URL        string
	FunnelName *string
	StageName  *string
}

type RecurrenceRule struct {
	Frequency domainrecurrence.Frequency
	Interval  int
	Weekdays  []int
}

type Task struct {
	ID                 uuid.UUID
	CompanyID          uuid.UUID
	BoardID            uuid.UUID
	ColumnID           uuid.UUID
	Order              int32
	Title              string
	Description        json.RawMessage
	AuthorID           uuid.UUID
	AssigneeIDs        []uuid.UUID
	AssigneePositionID *uuid.UUID
	WatcherIDs         []uuid.UUID
	DueDate            *time.Time
	Priority           string
	LabelIDs           []uuid.UUID
	Checklist          []ChecklistItem
	Attachments        []Attachment
	Source             *TaskSource
	LinkedArticleIDs   []uuid.UUID
	Recurrence         *RecurrenceRule
	CompletedAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Comment struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	AuthorID  uuid.UUID
	Content   json.RawMessage
	CreatedAt time.Time
}