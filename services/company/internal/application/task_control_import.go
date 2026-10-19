package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type TaskControlSourceInput struct {
	ExpectedBindingRevision int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64     `json:"expectedModeEpoch"`
	RequestID               uuid.UUID `json:"requestId"`
	Kind                    string    `json:"kind"`
	CatalogID               string    `json:"catalogId,omitempty"`
}
type TaskControlSourceRequest struct {
	ID                 uuid.UUID        `json:"id"`
	Scope              TaskControlScope `json:"scope"`
	Kind               string           `json:"kind"`
	State              string           `json:"state"`
	CatalogID          string           `json:"catalogId,omitempty"`
	SourceSnapshotID   *uuid.UUID       `json:"sourceSnapshotId,omitempty"`
	ExpiresAt          time.Time        `json:"expiresAt"`
	Issues             []tc.Issue       `json:"issues,omitempty"`
	ErrorCode          string           `json:"errorCode,omitempty"`
	SnapshotFreshUntil *time.Time       `json:"freshUntil,omitempty"`
}
type taskControlSourceRecord struct {
	TaskControlSourceRequest
	ActorID    uuid.UUID                `json:"actorId"`
	CRMAdminID string                   `json:"crmAdminId"`
	BodyHash   string                   `json:"bodyHash"`
	ResultHash string                   `json:"resultHash,omitempty"`
	ReceivedAt time.Time                `json:"receivedAt,omitempty"`
	FreshUntil time.Time                `json:"snapshotExpiresAt,omitempty"`
	Result     *TaskControlSourceResult `json:"result,omitempty"`
}
type TaskControlSourcePayload struct {
	Rules         []tc.Rule                  `json:"rules"`
	Raw           json.RawMessage            `json:"raw"`
	CatalogID     string                     `json:"catalogId,omitempty"`
	FieldIDs      map[string]json.RawMessage `json:"fieldIds,omitempty"`
	Code          string                     `json:"code,omitempty"`
	Issues        []tc.Issue                 `json:"issues,omitempty"`
	Discrepancies []tc.Issue                 `json:"discrepancies,omitempty"`
}
type TaskControlSourceResult struct {
	CoreAssertion  string                   `json:"coreAssertion"`
	Complete       bool                     `json:"complete"`
	SourceChecksum string                   `json:"sourceChecksum"`
	FetchedAt      time.Time                `json:"fetchedAt"`
	Payload        TaskControlSourcePayload `json:"payload"`
}

func getTCImport(ctx context.Context, q *db.Queries, c db.TaskControlConnection, kind string, id uuid.UUID, out any) error {
	raw, e := q.GetTaskControlImportObject(ctx, db.GetTaskControlImportObjectParams{CompanyID: c.CompanyID, BindingID: c.ID, Kind: kind, ID: id})
	if isNoRows(e) {
		return notFound("Источник или импорт")
	}
	if e != nil {
		return e
	}
	return json.Unmarshal(raw, out)
}
func saveTCImport(ctx context.Context, q *db.Queries, c db.TaskControlConnection, kind string, id uuid.UUID, in any) error {
	raw, e := json.Marshal(in)
	if e != nil {
		return e
	}
	return q.SaveTaskControlImportObject(ctx, db.SaveTaskControlImportObjectParams{CompanyID: c.CompanyID, BindingID: c.ID, Kind: kind, ID: id, Payload: raw})
}
func sourceConflict() error {
	return coded(ErrorConflict, "source_changed", "Источник изменился или устарел; получите новый снимок")
}
func (s *Service) CreateTaskControlSource(ctx context.Context, a Actor, id uuid.UUID, in TaskControlSourceInput) (TaskControlSourceRequest, error) {
	var out TaskControlSourceRequest
	if in.Kind == "references" {
		return out, coded(ErrorConflict, "references_server_owned", "Справочники доступны через серверный API выбора")
	}
	if in.RequestID == uuid.Nil || (in.Kind != "catalog" && in.Kind != "runtime_config") || !tc.ValidCRMID(in.CatalogID) {
		return out, validation("Некорректный запрос источника")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	hash := tcImportSemanticHash(in)
	var saved taskControlSourceRecord
	if e = getTCImport(ctx, q, c, "source", in.RequestID, &saved); e == nil {
		if saved.ActorID != a.UserID || saved.BodyHash != hash || saved.Scope != taskControlScope(c) {
			return out, sourceConflict()
		}
		return sourceDTO(saved, s.now()), nil
	} else if !tcImportErrorKind(e, ErrorNotFound) {
		return out, e
	}
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return out, e
	}
	pending, e := q.ListTaskControlImportSources(ctx, db.ListTaskControlImportSourcesParams{CompanyID: c.CompanyID, BindingID: c.ID, Now: s.now()})
	if e != nil {
		return out, e
	}
	if len(pending) >= 100 {
		return out, coded(ErrorConflict, "source_requests_limit", "Дождитесь завершения запросов источника")
	}
	saved = taskControlSourceRecord{TaskControlSourceRequest: TaskControlSourceRequest{ID: in.RequestID, Scope: taskControlScope(c), Kind: in.Kind, State: "pending", CatalogID: in.CatalogID, ExpiresAt: s.now().UTC().Add(10 * time.Minute)}, ActorID: a.UserID, CRMAdminID: c.CrmAdminID, BodyHash: hash}
	if e = saveTCImport(ctx, q, c, "source", saved.ID, saved); e != nil {
		return out, e
	}
	return saved.TaskControlSourceRequest, tx.Commit(ctx)
}
func sourceDTO(r taskControlSourceRecord, now time.Time) TaskControlSourceRequest {
	out := r.TaskControlSourceRequest
	if out.State == "pending" && !out.ExpiresAt.After(now) {
		out.State = "expired"
	}
	return out
}
func (s *Service) GetTaskControlSource(ctx context.Context, a Actor, id, rid uuid.UUID) (TaskControlSourceRequest, error) {
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return TaskControlSourceRequest{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var r taskControlSourceRecord
	if e = getTCImport(ctx, q, c, "source", rid, &r); e != nil {
		return TaskControlSourceRequest{}, e
	}
	if r.Scope != taskControlScope(c) {
		return TaskControlSourceRequest{}, sourceConflict()
	}
	return sourceDTO(r, s.now()), nil
}

// Bridge authorization deliberately uses a freshly verified CRM admin, never a runtime session.
func (s *Service) taskControlBridgeTx(ctx context.Context, id uuid.UUID, raw string) (pgx.Tx, *db.Queries, db.TaskControlConnection, tc.Assertion, string, error) {
	var c db.TaskControlConnection
	proof, key, e := s.taskControlAssertion(raw, "adminBridge")
	if e != nil {
		return nil, nil, c, proof, key, e
	}
	q := db.New(s.pool)
	if id == uuid.Nil {
		c, e = q.ActiveTaskControlConnection(ctx, db.ActiveTaskControlConnectionParams{CompanyID: proof.CompanyID, AccountID: proof.AccountID})
		id = c.ID
	} else {
		c, e = q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: proof.CompanyID, ID: id})
	}
	if e != nil {
		return nil, nil, c, proof, key, notFound("Подключение")
	}
	st, e := s.taskControlStatus(ctx, c.CompanyID, c.AccountID, false)
	if e != nil {
		return nil, nil, c, proof, key, e
	}
	if e = taskControlInstalled(st, c.InstallationID, c.IntegrationID, c.Generation, s.now()); e != nil {
		return nil, nil, c, proof, key, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, nil, c, proof, key, e
	}
	q = db.New(tx)
	fail := func(err error) (pgx.Tx, *db.Queries, db.TaskControlConnection, tc.Assertion, string, error) {
		_ = tx.Rollback(ctx)
		return nil, nil, c, proof, key, err
	}
	if e = q.LockTaskControlAccount(ctx, c.AccountID); e != nil {
		return fail(e)
	}
	if e = s.taskControlAccount(ctx, q, c.CompanyID, c.AccountID); e != nil {
		return fail(e)
	}
	c, e = q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: proof.CompanyID, ID: id})
	if e != nil {
		return fail(e)
	}
	if c.State != "active" {
		return fail(forbidden("Подключение отозвано"))
	}
	if e = s.currentTaskControlInstallation(ctx, q, c.CompanyID, c.AccountID, c.InstallationID, c.IntegrationID, c.Generation); e != nil {
		return fail(e)
	}
	if proof.CompanyID != c.CompanyID || proof.AccountID != c.AccountID || proof.InstallationID != c.InstallationID || proof.IntegrationID != c.IntegrationID || proof.Generation != c.Generation || proof.ActorID != c.CrmAdminID {
		_ = tx.Rollback(ctx)
		return nil, nil, c, proof, key, forbidden("Подтверждение относится к другому подключению или администратору")
	}
	return tx, q, c, proof, key, nil
}
func (s *Service) PendingTaskControlSources(ctx context.Context, id uuid.UUID, raw string) ([]TaskControlSourceRequest, error) {
	out := []TaskControlSourceRequest{}
	tx, q, c, proof, key, e := s.taskControlBridgeTx(ctx, id, raw)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = s.consumeTC(ctx, q, proof, key); e != nil {
		return out, e
	}
	records, e := q.ListTaskControlImportSources(ctx, db.ListTaskControlImportSourcesParams{CompanyID: c.CompanyID, BindingID: c.ID, Now: s.now()})
	if e != nil {
		return out, e
	}
	for _, raw := range records {
		var r taskControlSourceRecord
		if e = json.Unmarshal(raw, &r); e != nil {
			return out, e
		}
		if r.Scope == taskControlScope(c) && r.CRMAdminID == proof.ActorID && r.ExpiresAt.After(s.now()) {
			if _, e = s.taskControlActor(ctx, q, Actor{CompanyID: c.CompanyID, UserID: r.ActorID}, true); e == nil {
				out = append(out, r.TaskControlSourceRequest)
			}
		}
	}
	return out, tx.Commit(ctx)
}
func (s *Service) SubmitTaskControlSource(ctx context.Context, id, rid uuid.UUID, in TaskControlSourceResult) (TaskControlSourceRequest, error) {
	var out TaskControlSourceRequest
	raw, _ := json.Marshal(in)
	if len(raw) > 1048576 {
		return out, coded(ErrorPayloadTooLarge, "task_control_payload_too_large", "Источник превышает 1 МиБ")
	}
	tx, q, c, proof, key, e := s.taskControlBridgeTx(ctx, id, in.CoreAssertion)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if proof.RequestID != rid {
		return out, forbidden("Подтверждение относится к другому запросу")
	}
	var r taskControlSourceRecord
	if e = getTCImport(ctx, q, c, "source", rid, &r); e != nil {
		return out, e
	}
	if r.Scope != taskControlScope(c) || r.CRMAdminID != proof.ActorID {
		return out, sourceConflict()
	}
	if _, e = s.taskControlActor(ctx, q, Actor{CompanyID: c.CompanyID, UserID: r.ActorID}, true); e != nil {
		return out, e
	}
	semantic := in
	semantic.CoreAssertion = ""
	hash := tcImportSemanticHash(semantic)
	if r.ResultHash != "" {
		if r.ResultHash != hash {
			return out, coded(ErrorConflict, "idempotency_conflict", "Другой результат уже принят")
		}
		if e = s.consumeTC(ctx, q, proof, key); e != nil {
			return out, e
		}
		return r.TaskControlSourceRequest, tx.Commit(ctx)
	}
	if !r.ExpiresAt.After(s.now()) {
		return out, sourceConflict()
	}
	if in.Complete && in.Payload.CatalogID != r.CatalogID {
		return out, sourceConflict()
	}
	if e = s.consumeTC(ctx, q, proof, key); e != nil {
		return out, e
	}
	r.ResultHash = hash
	r.ReceivedAt = s.now().UTC()
	r.State = "failed"
	r.ErrorCode = "source_incomplete"
	r.Issues = append(append([]tc.Issue{}, in.Payload.Issues...), in.Payload.Discrepancies...)
	r.Result = &semantic
	if in.Complete {
		if in.Payload.Raw == nil || in.Payload.Rules == nil {
			return out, validation("Нужны исходные данные и полный массив правил")
		}
		_, sum := tc.Legacy(in.Payload.Rules)
		if sum != in.SourceChecksum {
			return out, validation("Контрольная сумма источника не совпадает")
		}
		// Validate the original bytes before JSONB can discard duplicate keys or
		// normalize numeric syntax. A failed capture never receives a snapshot ID.
		captureIssues := tc.ValidateImportCapture(in.Payload.Rules, in.Payload.Raw, in.Payload.FieldIDs, in.Payload.Code, r.Kind, r.CatalogID, c.AccountID)
		if len(captureIssues) > 0 {
			r.ErrorCode = "source_invalid"
			r.Issues = append(r.Issues, captureIssues...)
		} else {
			r.State = "completed"
			r.ErrorCode = ""
			sid := uuid.New()
			r.SourceSnapshotID = &sid
			r.FreshUntil = r.ReceivedAt.Add(5 * time.Minute)
			r.SnapshotFreshUntil = &r.FreshUntil
		}
	}
	if e = saveTCImport(ctx, q, c, "source", rid, r); e != nil {
		return out, e
	}
	return r.TaskControlSourceRequest, tx.Commit(ctx)
}

type TaskControlImportInput struct {
	ExpectedBindingRevision int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64     `json:"expectedModeEpoch"`
	RequestID               uuid.UUID `json:"requestId"`
	ExpectedDraftRevision   int64     `json:"expectedDraftRevision"`
	SourceSnapshotID        uuid.UUID `json:"sourceSnapshotId"`
}
type TaskControlImportPreview struct {
	ID               uuid.UUID        `json:"id"`
	Scope            TaskControlScope `json:"scope"`
	CatalogID        string           `json:"catalogId"`
	SourceKind       string           `json:"sourceKind"`
	SourceSnapshotID uuid.UUID        `json:"sourceSnapshotId"`
	SourceChecksum   string           `json:"sourceChecksum"`
	DraftRevision    int64            `json:"draftRevision"`
	Complete         bool             `json:"complete"`
	Rules            []tc.Rule        `json:"rules"`
	Issues           []tc.Issue       `json:"issues"`
	Original         json.RawMessage  `json:"original"`
	Backup           json.RawMessage  `json:"backup,omitempty"`
	AppliedRequestID *uuid.UUID       `json:"appliedRequestId,omitempty"`
	ExpiresAt        time.Time        `json:"expiresAt"`
}
type taskControlImportRecord struct {
	TaskControlImportPreview
	ActorID            uuid.UUID `json:"actorId"`
	BodyHash           string    `json:"bodyHash"`
	SourceIdentityHash string    `json:"sourceIdentityHash"`
	SchemaVersion      int       `json:"schemaVersion"`
}
type TaskControlImportApplyInput struct {
	ExpectedBindingRevision  int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch        int64     `json:"expectedModeEpoch"`
	RequestID                uuid.UUID `json:"requestId"`
	ExpectedDraftRevision    int64     `json:"expectedDraftRevision"`
	ExpectedSourceChecksum   string    `json:"expectedSourceChecksum"`
	VerificationSnapshotID   uuid.UUID `json:"verificationSnapshotId"`
	AcknowledgeDiscrepancies bool      `json:"acknowledgeDiscrepancies,omitempty"`
}
type taskControlImportApplyRecord struct {
	ActorID  uuid.UUID           `json:"actorId"`
	BodyHash string              `json:"bodyHash"`
	Response json.RawMessage     `json:"response"`
	Backup   db.TaskControlDraft `json:"backup"`
	Scope    TaskControlScope    `json:"scope"`
}

func (s *Service) taskControlImportSnapshot(ctx context.Context, q *db.Queries, c db.TaskControlConnection, id uuid.UUID) (taskControlSourceRecord, error) {
	var r taskControlSourceRecord
	raw, e := q.GetTaskControlImportSourceSnapshot(ctx, db.GetTaskControlImportSourceSnapshotParams{CompanyID: c.CompanyID, BindingID: c.ID, SnapshotID: id.String()})
	if isNoRows(e) {
		return r, notFound("Снимок источника")
	}
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(raw, &r); e != nil {
		return r, e
	}
	if r.Scope != taskControlScope(c) || r.State != "completed" || r.Result == nil || !r.Result.Complete || !r.FreshUntil.After(s.now()) {
		return r, sourceConflict()
	}
	return r, nil
}
func sourceIdentity(r taskControlSourceRecord) string {
	return tcImportSemanticHash([]any{r.Kind, r.CatalogID, r.Result.SourceChecksum, r.Result.Payload.Code, r.Result.Payload.FieldIDs, r.Result.Payload.Raw})
}
func (s *Service) PreviewTaskControlImport(ctx context.Context, a Actor, id uuid.UUID, in TaskControlImportInput, v2 bool) (TaskControlImportPreview, error) {
	var out TaskControlImportPreview
	if in.RequestID == uuid.Nil || in.SourceSnapshotID == uuid.Nil {
		return out, validation("Требуются идентификаторы импорта и источника")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	hash := tcImportSemanticHash([]any{in, v2})
	var r taskControlImportRecord
	if e = getTCImport(ctx, q, c, "preview", in.RequestID, &r); e == nil {
		if r.ActorID != a.UserID || r.BodyHash != hash || r.Scope != taskControlScope(c) {
			return out, sourceConflict()
		}
		return r.TaskControlImportPreview, nil
	} else if !tcImportErrorKind(e, ErrorNotFound) {
		return out, e
	}
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return out, e
	}
	if !v2 && c.RuntimeSchemaVersion == 2 {
		return out, taskControlRequiresV2()
	}
	if v2 {
		if e = taskControlV2Required(c); e != nil {
			return out, e
		}
	}
	d, e := q.GetTaskControlDraft(ctx, db.GetTaskControlDraftParams{CompanyID: c.CompanyID, BindingID: c.ID})
	if e != nil {
		return out, e
	}
	if d.Revision != in.ExpectedDraftRevision {
		return out, conflict("Черновик уже изменён")
	}
	src, e := s.taskControlImportSnapshot(ctx, q, c, in.SourceSnapshotID)
	if e != nil {
		return out, e
	}
	issues := tc.ValidateRules(src.Result.Payload.Rules)
	issues = append(issues, src.Result.Payload.Discrepancies...)
	issues = append(issues, tc.ImportCaptureDiscrepancies(src.Result.Payload.Rules, src.Result.Payload.Raw, src.Result.Payload.FieldIDs, src.Result.Payload.Code, src.Kind, src.CatalogID, c.AccountID)...)
	issues = append(issues, tc.ValidateImportCapture(src.Result.Payload.Rules, src.Result.Payload.Raw, src.Result.Payload.FieldIDs, src.Result.Payload.Code, src.Kind, src.CatalogID, c.AccountID)...)
	out = TaskControlImportPreview{ID: in.RequestID, Scope: taskControlScope(c), CatalogID: src.CatalogID, SourceKind: src.Kind, SourceSnapshotID: in.SourceSnapshotID, SourceChecksum: src.Result.SourceChecksum, DraftRevision: d.Revision, Complete: !tcImportBlocking(issues, src.Kind), Rules: src.Result.Payload.Rules, Issues: issues, Original: src.Result.Payload.Raw, ExpiresAt: s.now().UTC().Add(30 * time.Minute)}
	schema := 1
	if v2 {
		schema = 2
	}
	r = taskControlImportRecord{TaskControlImportPreview: out, ActorID: a.UserID, BodyHash: hash, SourceIdentityHash: sourceIdentity(src), SchemaVersion: schema}
	if e = saveTCImport(ctx, q, c, "preview", out.ID, r); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) GetTaskControlImport(ctx context.Context, a Actor, id, pid uuid.UUID) (TaskControlImportPreview, error) {
	var r taskControlImportRecord
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return r.TaskControlImportPreview, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	e = getTCImport(ctx, q, c, "preview", pid, &r)
	if e == nil && r.Scope != taskControlScope(c) {
		e = sourceConflict()
	}
	return r.TaskControlImportPreview, e
}
func (s *Service) ApplyTaskControlImport(ctx context.Context, a Actor, id, pid uuid.UUID, in TaskControlImportApplyInput, v2 bool) (json.RawMessage, error) {
	if in.RequestID == uuid.Nil || in.VerificationSnapshotID == uuid.Nil {
		return nil, validation("Требуются идентификаторы применения и проверки")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	hash := tcImportSemanticHash([]any{pid, in, v2})
	var saved taskControlImportApplyRecord
	if e = getTCImport(ctx, q, c, "apply", in.RequestID, &saved); e == nil {
		if saved.ActorID != a.UserID || saved.BodyHash != hash || saved.Scope != taskControlScope(c) {
			return nil, coded(ErrorConflict, "idempotency_conflict", "Идентификатор применения уже использован")
		}
		return saved.Response, nil
	} else if !tcImportErrorKind(e, ErrorNotFound) {
		return nil, e
	}
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return nil, e
	}
	if !v2 && c.RuntimeSchemaVersion == 2 {
		return nil, taskControlRequiresV2()
	}
	var preview taskControlImportRecord
	if e = getTCImport(ctx, q, c, "preview", pid, &preview); e != nil {
		return nil, e
	}
	schema := 1
	if v2 {
		schema = 2
	}
	if preview.Scope != taskControlScope(c) || preview.SchemaVersion != schema || !preview.Complete || !preview.ExpiresAt.After(s.now()) || preview.SourceChecksum != in.ExpectedSourceChecksum || preview.SourceSnapshotID == in.VerificationSnapshotID {
		return nil, sourceConflict()
	}
	verification, e := s.taskControlImportSnapshot(ctx, q, c, in.VerificationSnapshotID)
	if e != nil {
		return nil, e
	}
	if sourceIdentity(verification) != preview.SourceIdentityHash || verification.Result.SourceChecksum != preview.SourceChecksum || verification.ReceivedAt.Before(preview.ExpiresAt.Add(-30*time.Minute)) || len(tc.ValidateRules(verification.Result.Payload.Rules)) > 0 || tcImportBlocking(verification.Result.Payload.Discrepancies, verification.Kind) || len(tc.ValidateImportCapture(verification.Result.Payload.Rules, verification.Result.Payload.Raw, verification.Result.Payload.FieldIDs, verification.Result.Payload.Code, verification.Kind, verification.CatalogID, c.AccountID)) > 0 {
		return nil, sourceConflict()
	}
	if len(preview.Issues) > 0 && !in.AcknowledgeDiscrepancies {
		return nil, coded(ErrorConflict, "import_conflicts_unconfirmed", "Подтвердите расхождения источников перед импортом")
	}
	backup, e := q.GetTaskControlDraft(ctx, db.GetTaskControlDraftParams{CompanyID: c.CompanyID, BindingID: c.ID})
	if e != nil {
		return nil, e
	}
	if preview.DraftRevision != in.ExpectedDraftRevision || backup.Revision != in.ExpectedDraftRevision {
		return nil, conflict("Черновик уже изменён")
	}
	var d db.TaskControlDraft
	if v2 {
		if e = taskControlV2Required(c); e != nil {
			return nil, e
		}
		intent := make([]tc.IntentRule, 0, len(preview.Rules))
		for i := range preview.Rules {
			r := preview.Rules[i]
			intent = append(intent, tc.IntentRule{Kind: "legacyExplicit", Legacy: &r})
		}
		raw, _ := json.Marshal(intent)
		sum, err := tc.IntentChecksum(intent)
		if err != nil {
			return nil, err
		}
		d, e = q.UpdateTaskControlDraftV2(ctx, db.UpdateTaskControlDraftV2Params{CompanyID: c.CompanyID, BindingID: c.ID, Rules: raw, Checksum: sum, UpdatedBy: a.UserID, Revision: in.ExpectedDraftRevision})
	} else {
		raw, _ := json.Marshal(preview.Rules)
		_, sum := tc.Legacy(preview.Rules)
		d, e = q.UpdateTaskControlDraft(ctx, db.UpdateTaskControlDraftParams{CompanyID: c.CompanyID, BindingID: c.ID, Rules: raw, Checksum: sum, UpdatedBy: a.UserID, Revision: in.ExpectedDraftRevision})
	}
	if e != nil {
		return nil, e
	}
	var response any
	if v2 {
		response, e = intentDraftDTO(c, d)
	} else {
		response, e = draftDTO(c, d)
	}
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(response)
	if e != nil {
		return nil, e
	}
	backupJSON, _ := json.Marshal(map[string]any{"revision": backup.Revision, "schemaVersion": backup.SchemaVersion, "rules": json.RawMessage(backup.Rules), "checksum": backup.Checksum, "updatedAt": backup.UpdatedAt, "updatedBy": backup.UpdatedBy})
	preview.Backup = backupJSON
	preview.AppliedRequestID = &in.RequestID
	if e = saveTCImport(ctx, q, c, "preview", pid, preview); e != nil {
		return nil, e
	}
	saved = taskControlImportApplyRecord{ActorID: a.UserID, BodyHash: hash, Response: raw, Backup: backup, Scope: taskControlScope(c)}
	if e = saveTCImport(ctx, q, c, "apply", in.RequestID, saved); e != nil {
		return nil, e
	}
	return raw, tx.Commit(ctx)
}

func taskControlRuntimeV2Version(version string) bool {
	return version == "1.2.0" || version == "1.2.1" || version == "1.2.2" || version == "1.2.3" || version == "1.2.4"
}

func tcImportErrorKind(err error, kind ErrorKind) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == kind
}

func tcImportSemanticHash(value any) string {
	raw, e := json.Marshal(value)
	if e != nil {
		return ""
	}
	var semantic any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e = d.Decode(&semantic); e != nil {
		return ""
	}
	return requestBodyHash(semantic)
}

func tcImportBlocking(issues []tc.Issue, kind string) bool {
	for _, issue := range issues {
		if issue.Code == "catalog_runtime_divergence" || (kind == "catalog" && issue.Code == "runtime_config_invalid") || (kind == "runtime_config" && issue.Code == "catalog_invalid") {
			continue
		}
		return true
	}
	return false
}
