package application

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

type courseAuditSnapshot struct {
	OwnerType          string `json:"ownerType"`
	OwnerUserID        string `json:"ownerUserId,omitempty"`
	LifecycleStatus    string `json:"lifecycleStatus"`
	DistributionStatus string `json:"distributionStatus"`
}

func auditCourseSnapshot(value Course) []byte {
	snapshot := courseAuditSnapshot{
		OwnerType: value.OwnerType, LifecycleStatus: value.LifecycleStatus,
		DistributionStatus: value.DistributionStatus,
	}
	if value.OwnerUserID != nil {
		snapshot.OwnerUserID = value.OwnerUserID.String()
	}
	encoded, _ := json.Marshal(snapshot)
	return encoded
}

func (s *Service) auditCourse(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	action string,
	before *Course,
	after Course,
) error {
	var beforeState []byte
	if before != nil {
		beforeState = auditCourseSnapshot(*before)
	}
	_, err := queries.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, ActorID: actor.UserID,
		ActorRole: actor.Role, Action: action, AggregateType: "course",
		AggregateID: after.ID, BeforeState: beforeState, AfterState: auditCourseSnapshot(after),
		CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return internal("Не удалось сохранить запись аудита", err)
	}
	return nil
}

type courseVersionAuditSnapshot struct {
	CourseID    string `json:"courseId"`
	Number      int32  `json:"number"`
	Status      string `json:"status"`
	ContentHash string `json:"contentHash,omitempty"`
}

func auditCourseVersionSnapshot(value CourseVersion) []byte {
	snapshot := courseVersionAuditSnapshot{
		CourseID: value.CourseID.String(), Number: value.Number, Status: value.Status,
	}
	if value.ContentHash != nil {
		snapshot.ContentHash = *value.ContentHash
	}
	encoded, _ := json.Marshal(snapshot)
	return encoded
}

func (s *Service) auditCourseVersion(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	action string,
	before *CourseVersion,
	after CourseVersion,
) error {
	var beforeState []byte
	if before != nil {
		beforeState = auditCourseVersionSnapshot(*before)
	}
	_, err := queries.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, ActorID: actor.UserID,
		ActorRole: actor.Role, Action: action, AggregateType: "course_version",
		AggregateID: after.ID, BeforeState: beforeState, AfterState: auditCourseVersionSnapshot(after),
		CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return internal("Не удалось сохранить запись аудита", err)
	}
	return nil
}

func (s *Service) auditMutation(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	action, aggregateType string,
	aggregateID uuid.UUID,
	before, after any,
) error {
	beforeState, err := json.Marshal(before)
	if err != nil {
		return internal("Не удалось сформировать состояние аудита", err)
	}
	afterState, err := json.Marshal(after)
	if err != nil {
		return internal("Не удалось сформировать состояние аудита", err)
	}
	_, err = queries.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, ActorID: actor.UserID,
		ActorRole: actor.Role, Action: action, AggregateType: aggregateType,
		AggregateID: aggregateID, BeforeState: beforeState, AfterState: afterState,
		CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return internal("Не удалось сохранить запись аудита", err)
	}
	return nil
}
