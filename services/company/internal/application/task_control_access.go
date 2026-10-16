package application

import (
	"context"
	"errors"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// TaskControlError preserves explicit codes and adds stable defaults at the RPC boundary.
func TaskControlError(err error) error {
	var app *Error
	if !errors.As(err, &app) || app.Code != "" {
		return err
	}
	copy := *app
	copy.Code = map[ErrorKind]string{ErrorValidation: "task_control_invalid_request", ErrorUnauthenticated: "task_control_access_denied", ErrorForbidden: "task_control_access_denied", ErrorNotFound: "task_control_not_found", ErrorConflict: "task_control_revision_conflict", ErrorUpstream: "task_control_core_unavailable", ErrorInternal: "task_control_internal_error"}[app.Kind]
	return &copy
}

type TaskControlCapabilities struct {
	Read                     bool            `json:"read"`
	Manage                   bool            `json:"manage"`
	Operations               map[string]bool `json:"operations"`
	AvailableOperations      []string        `json:"availableOperations"`
	AdminProofTTLSeconds     int             `json:"adminProofTtlSeconds"`
	RuntimeSessionTTLSeconds int             `json:"runtimeSessionTtlSeconds"`
}

func (s *Service) GetTaskControlCapabilities(ctx context.Context, a Actor) (TaskControlCapabilities, error) {
	_, p, err := s.taskControlPermissions(ctx, db.New(s.pool), a)
	if err != nil {
		return TaskControlCapabilities{}, err
	}
	out := TaskControlCapabilities{Read: p.Read, Manage: p.Manage, Operations: map[string]bool{},
		AvailableOperations:  []string{"connections", "installation", "draft", "publications", "link", "revoke", "mode", "updateDraft", "validate", "publish", "intent"},
		AdminProofTTLSeconds: 60, RuntimeSessionTTLSeconds: 300}
	for _, op := range []string{"connections", "installation", "draft", "publications", "history", "references", "application", "link", "revoke", "mode", "updateDraft", "validate", "publish", "rollback", "imports", "sourceRequests", "refreshReferences", "intent"} {
		manage, _ := tc.RequiredAccess(op)
		out.Operations[op] = p.Read && (!manage || p.Manage)
	}
	return out, nil
}

// AuthorizeTaskControlOperation gates not-yet-implemented product APIs as well.
// A successful permission check is not a claim that the operation is available.
func (s *Service) AuthorizeTaskControlOperation(ctx context.Context, a Actor, binding uuid.UUID, operation string) error {
	manage, known := tc.RequiredAccess(operation)
	if !known {
		return coded(ErrorForbidden, "task_control_access_denied", "Неизвестная операция Контроля задач")
	}
	q := db.New(s.pool)
	if _, err := s.taskControlActor(ctx, q, a, manage); err != nil {
		return err
	}
	c, err := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: a.CompanyID, ID: binding})
	if err != nil {
		return notFound("Подключение")
	}
	if err = s.taskControlAccount(ctx, q, a.CompanyID, c.AccountID); err != nil {
		return err
	}
	if manage && c.State != "active" {
		return coded(ErrorForbidden, "task_control_binding_revoked", "Подключение отозвано")
	}
	return nil
}
