package application

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const (
	taskControlApplyAction = "applyRules"
	taskControlSaveReason  = "Сохранение правил"
)

type taskControlRuleSave struct {
	actor   Actor
	binding uuid.UUID
	id      uuid.UUID
	hash    string
}

func taskControlApplyRequest(c db.TaskControlConnection, actor Actor, input any, apply []*uuid.UUID) (*taskControlRuleSave, error) {
	if len(apply) == 0 || apply[0] == nil {
		return nil, nil
	}
	if *apply[0] == uuid.Nil {
		return nil, validation("Требуется ID операции сохранения")
	}
	return &taskControlRuleSave{
		actor: actor, binding: c.ID, id: *apply[0],
		hash: requestBodyHash([]any{taskControlApplyAction, c.ID, input}),
	}, nil
}

func (r *taskControlRuleSave) replay(ctx context.Context, q *db.Queries, out any) (bool, error) {
	if r == nil {
		return false, nil
	}
	prior, err := q.GetTaskControlConnectionRequest(ctx, db.GetTaskControlConnectionRequestParams{CompanyID: r.actor.CompanyID, RequestID: r.id})
	if err == nil {
		if prior.ActorID != r.actor.UserID || prior.BindingID != r.binding || prior.Action != taskControlApplyAction || prior.BodyHash != r.hash {
			return false, taskControlRuleSaveConflict()
		}
		return true, json.Unmarshal(prior.Response, out)
	}
	if !isNoRows(err) {
		return false, err
	}
	_, err = q.GetTaskControlPublishRequest(ctx, db.GetTaskControlPublishRequestParams{CompanyID: r.actor.CompanyID, RequestID: r.id})
	if err == nil {
		return false, taskControlRuleSaveConflict()
	}
	if !isNoRows(err) {
		return false, err
	}
	return false, nil
}

func (r *taskControlRuleSave) record(ctx context.Context, q *db.Queries, out any) error {
	if r == nil {
		return nil
	}
	response, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return taskControlRuleSaveError(q.SaveTaskControlConnectionRequest(ctx, db.SaveTaskControlConnectionRequestParams{
		CompanyID: r.actor.CompanyID, RequestID: r.id, BindingID: r.binding, ActorID: r.actor.UserID,
		Action: taskControlApplyAction, BodyHash: r.hash, Response: response,
	}))
}

func taskControlRuleSaveError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "task_control_connection_requests_pkey", "task_control_publications_company_id_request_id_key":
			return taskControlRuleSaveConflict()
		}
	}
	return err
}

func taskControlRuleSaveConflict() error {
	return coded(ErrorConflict, "task_control_idempotency_conflict", "Ключ операции уже использован")
}

func activateTaskControlRules(ctx context.Context, q *db.Queries, c db.TaskControlConnection) (db.TaskControlConnection, error) {
	if c.Mode == "managed" {
		return c, nil
	}
	managed, err := q.SetTaskControlMode(ctx, db.SetTaskControlModeParams{
		CompanyID: c.CompanyID, ID: c.ID, Mode: "managed", BindingRevision: c.BindingRevision, ModeEpoch: c.ModeEpoch,
	})
	if err != nil {
		return c, err
	}
	if err = q.DeleteTaskControlRuntimeSessions(ctx, db.DeleteTaskControlRuntimeSessionsParams{CompanyID: c.CompanyID, BindingID: c.ID}); err != nil {
		return c, err
	}
	cached, err := q.GetTaskControlReferenceCache(ctx, db.GetTaskControlReferenceCacheParams{CompanyID: c.CompanyID, BindingID: c.ID, ScopeKey: referenceScopeKey(c)})
	if isNoRows(err) {
		return managed, nil
	}
	if err != nil {
		return c, err
	}
	err = q.SaveTaskControlReferenceCache(ctx, db.SaveTaskControlReferenceCacheParams{
		CompanyID: c.CompanyID, BindingID: c.ID, ScopeKey: referenceScopeKey(managed), Payload: cached.Payload, LastError: cached.LastError,
	})
	return managed, err
}

func checkTaskControlModeTransition(ctx context.Context, q *db.Queries, c db.TaskControlConnection, target string) error {
	if target != "standalone" {
		return nil
	}
	applied, err := q.HasAppliedTaskControlRules(ctx, db.HasAppliedTaskControlRulesParams{
		CompanyID: c.CompanyID, BindingID: c.ID,
	})
	if err != nil {
		return err
	}
	if applied {
		return coded(ErrorConflict, "task_control_managed_only", "Подключение использует правила TeamOS")
	}
	return nil
}
