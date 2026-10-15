package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type TaskControlDraft struct {
	Scope         TaskControlScope `json:"scope"`
	Revision      int64            `json:"revision"`
	SchemaVersion int              `json:"schemaVersion"`
	Checksum      string           `json:"checksum"`
	Rules         []tc.Rule        `json:"rules"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	UpdatedBy     uuid.UUID        `json:"updatedBy"`
}
type TaskControlDraftInput struct {
	ExpectedBindingRevision int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64     `json:"expectedModeEpoch"`
	ExpectedRevision        int64     `json:"expectedRevision"`
	Rules                   []tc.Rule `json:"rules"`
}
type TaskControlPublishInput struct {
	ExpectedBindingRevision int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64     `json:"expectedModeEpoch"`
	RequestID               uuid.UUID `json:"requestId"`
	ExpectedDraftRevision   int64     `json:"expectedDraftRevision"`
	ExpectedChecksum        string    `json:"expectedChecksum"`
	Reason                  string    `json:"reason"`
}
type TaskControlPublication struct {
	ID            uuid.UUID        `json:"id"`
	Scope         TaskControlScope `json:"scope"`
	Version       int64            `json:"version"`
	DraftRevision int64            `json:"draftRevision"`
	SchemaVersion int              `json:"schemaVersion"`
	Checksum      string           `json:"checksum"`
	Rules         []tc.Rule        `json:"rules"`
	CreatedAt     time.Time        `json:"createdAt"`
	CreatedBy     uuid.UUID        `json:"createdBy"`
	Reason        string           `json:"reason"`
}
type TaskControlModeInput struct {
	ExpectedBindingRevision int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64     `json:"expectedModeEpoch"`
	RequestID               uuid.UUID `json:"requestId"`
	TargetMode              string    `json:"targetMode"`
	PublicationID           uuid.UUID `json:"publicationId"`
	CoreAssertion           string    `json:"coreAssertion"`
}

func (s *Service) taskControlRulesTx(ctx context.Context, a Actor, id uuid.UUID, manage bool) (pgx.Tx, *db.Queries, db.TaskControlConnection, error) {
	var c db.TaskControlConnection
	q := db.New(s.pool)
	if _, e := s.taskControlActor(ctx, q, a, manage); e != nil {
		return nil, nil, c, e
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: a.CompanyID, ID: id})
	if e != nil {
		return nil, nil, c, notFound("Подключение")
	}
	st, e := s.taskControlStatus(ctx, a.CompanyID, c.AccountID, false)
	if e != nil {
		return nil, nil, c, e
	}
	if e = taskControlInstalled(st, c.InstallationID, c.IntegrationID, c.Generation, s.now()); e != nil {
		return nil, nil, c, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, nil, c, e
	}
	q = db.New(tx)
	fail := func(err error) (pgx.Tx, *db.Queries, db.TaskControlConnection, error) {
		_ = tx.Rollback(ctx)
		return nil, nil, c, err
	}
	if e = q.LockTaskControlAccount(ctx, c.AccountID); e != nil {
		return fail(e)
	}
	if _, e = s.taskControlActor(ctx, q, a, manage); e != nil {
		return fail(e)
	}
	if e = s.taskControlAccount(ctx, q, a.CompanyID, c.AccountID); e != nil {
		return fail(e)
	}
	c, e = q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: a.CompanyID, ID: id})
	if e != nil {
		return fail(e)
	}
	if c.State != "active" {
		return fail(forbidden("Подключение отозвано"))
	}
	if e = taskControlInstalled(st, c.InstallationID, c.IntegrationID, c.Generation, s.now()); e != nil {
		return fail(e)
	}
	_, sum := tc.Legacy([]tc.Rule{})
	e = q.EnsureTaskControlDraft(ctx, db.EnsureTaskControlDraftParams{CompanyID: a.CompanyID, BindingID: id, Checksum: sum, UpdatedBy: c.LinkedBy})
	if e != nil {
		return fail(e)
	}
	return tx, q, c, nil
}
func draftDTO(c db.TaskControlConnection, d db.TaskControlDraft) (TaskControlDraft, error) {
	var rules []tc.Rule
	e := json.Unmarshal(d.Rules, &rules)
	return TaskControlDraft{taskControlScope(c), d.Revision, 1, d.Checksum, rules, d.UpdatedAt, d.UpdatedBy}, e
}
func tcFence(c db.TaskControlConnection, revision, epoch int64) error {
	if revision != c.BindingRevision || epoch != c.ModeEpoch {
		return conflict("Ревизия подключения изменилась")
	}
	return nil
}
func (s *Service) GetTaskControlDraft(ctx context.Context, a Actor, id uuid.UUID) (TaskControlDraft, error) {
	var out TaskControlDraft
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, e := q.GetTaskControlDraft(ctx, db.GetTaskControlDraftParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return out, e
	}
	out, e = draftDTO(c, d)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) UpdateTaskControlDraft(ctx context.Context, a Actor, id uuid.UUID, in TaskControlDraftInput) (TaskControlDraft, error) {
	var out TaskControlDraft
	if issues := tc.ValidateRules(in.Rules); len(issues) > 0 {
		return out, validation(issues[0].Message)
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return out, e
	}
	_, sum := tc.Legacy(in.Rules)
	raw, _ := json.Marshal(in.Rules)
	d, e := q.UpdateTaskControlDraft(ctx, db.UpdateTaskControlDraftParams{CompanyID: a.CompanyID, BindingID: id, Rules: raw, Checksum: sum, UpdatedBy: a.UserID, Revision: in.ExpectedRevision})
	if isNoRows(e) {
		return out, conflict("Черновик уже изменён")
	}
	if e != nil {
		return out, e
	}
	out, e = draftDTO(c, d)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) ValidateTaskControlDraft(ctx context.Context, a Actor, id uuid.UUID) (any, error) {
	d, e := s.GetTaskControlDraft(ctx, a, id)
	if e != nil {
		return nil, e
	}
	issues := tc.ValidateRules(d.Rules)
	return map[string]any{"valid": len(issues) == 0, "draftRevision": d.Revision, "referencesRevision": "structural-v1", "issues": issues}, nil
}
func (s *Service) PublishTaskControlDraft(ctx context.Context, a Actor, id uuid.UUID, in TaskControlPublishInput) (TaskControlPublication, error) {
	var out TaskControlPublication
	if in.RequestID == uuid.Nil || strings.TrimSpace(in.Reason) == "" || utf8.RuneCountInString(in.Reason) > 1000 {
		return out, validation("Требуются ID операции и причина публикации")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	raw, _ := json.Marshal([]any{id, in})
	hash := sha256.Sum256(raw)
	hs := hex.EncodeToString(hash[:])
	prior, e := q.GetTaskControlPublishRequest(ctx, db.GetTaskControlPublishRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID})
	if e == nil {
		if prior.BindingID != id || prior.BodyHash != hs {
			return out, conflict("Ключ операции уже использован")
		}
		e = json.Unmarshal(prior.Payload, &out)
		return out, e
	}
	if !isNoRows(e) {
		return out, e
	}
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return out, e
	}
	d, e := q.GetTaskControlDraft(ctx, db.GetTaskControlDraftParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return out, e
	}
	if d.Revision != in.ExpectedDraftRevision || d.Checksum != in.ExpectedChecksum {
		return out, conflict("Черновик изменился")
	}
	var rules []tc.Rule
	if e = json.Unmarshal(d.Rules, &rules); e != nil {
		return out, e
	}
	if issues := tc.ValidateRules(rules); len(issues) > 0 {
		return out, validation(issues[0].Message)
	}
	rows, e := q.ListTaskControlPublications(ctx, db.ListTaskControlPublicationsParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return out, e
	}
	version := int64(1)
	if len(rows) > 0 {
		version = rows[0].Version + 1
	}
	out = TaskControlPublication{uuid.New(), taskControlScope(c), version, d.Revision, 1, d.Checksum, rules, s.now().UTC(), a.UserID, in.Reason}
	payload, _ := json.Marshal(out)
	_, e = q.CreateTaskControlPublication(ctx, db.CreateTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: out.ID, RequestID: in.RequestID, BodyHash: hs, Payload: payload})
	if e != nil {
		return out, e
	}
	_, e = q.SetTaskControlPublication(ctx, db.SetTaskControlPublicationParams{CompanyID: a.CompanyID, ID: id, PublicationID: uuid.NullUUID{UUID: out.ID, Valid: true}})
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) TaskControlPublications(ctx context.Context, a Actor, id, publication uuid.UUID) (any, error) {
	tx, q, _, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if publication != uuid.Nil {
		p, e := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: publication})
		if e != nil {
			return nil, notFound("Публикация")
		}
		return json.RawMessage(p.Payload), nil
	}
	rows, e := q.ListTaskControlPublications(ctx, db.ListTaskControlPublicationsParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return nil, e
	}
	items := []json.RawMessage{}
	for _, p := range rows {
		items = append(items, p.Payload)
	}
	return map[string]any{"items": items}, nil
}
func (s *Service) SetTaskControlMode(ctx context.Context, a Actor, id uuid.UUID, in TaskControlModeInput) (TaskControlIntent, error) {
	var out TaskControlIntent
	if in.RequestID == uuid.Nil || (in.TargetMode != "managed" && in.TargetMode != "standalone") {
		return out, validation("Некорректный режим")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	raw, _ := json.Marshal([]any{id, in.ExpectedBindingRevision, in.ExpectedModeEpoch, in.TargetMode, in.PublicationID})
	hash := sha256.Sum256(raw)
	hs := hex.EncodeToString(hash[:])
	prior, e := q.GetTaskControlConnectionRequest(ctx, db.GetTaskControlConnectionRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID})
	if e == nil {
		if prior.ActorID != a.UserID || prior.BindingID != id || prior.Action != "mode" || prior.BodyHash != hs {
			return out, conflict("Ключ операции уже использован")
		}
		e = json.Unmarshal(prior.Response, &out)
		return out, e
	}
	if !isNoRows(e) {
		return out, e
	}
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return out, e
	}
	if !c.PublicationID.Valid || c.PublicationID.UUID != in.PublicationID {
		return out, conflict("Сначала опубликуйте правила и выберите текущую публикацию")
	}
	proof, key, e := s.taskControlAssertion(in.CoreAssertion, "mode")
	if e != nil {
		return out, e
	}
	if proof.CompanyID != a.CompanyID || proof.InstallationID != c.InstallationID || proof.IntegrationID != c.IntegrationID || proof.Generation != c.Generation || proof.AccountID != c.AccountID || proof.RequestID != in.RequestID {
		return out, forbidden("Подтверждение относится к другому подключению")
	}
	if e = consumeTC(ctx, q, proof, key); e != nil {
		return out, e
	}
	c, e = q.SetTaskControlMode(ctx, db.SetTaskControlModeParams{CompanyID: a.CompanyID, ID: id, Mode: in.TargetMode, BindingRevision: in.ExpectedBindingRevision, ModeEpoch: in.ExpectedModeEpoch})
	if e != nil {
		return out, e
	}
	if e = q.DeleteTaskControlRuntimeSessions(ctx, db.DeleteTaskControlRuntimeSessionsParams{CompanyID: a.CompanyID, BindingID: id}); e != nil {
		return out, e
	}
	out = TaskControlIntent{in.RequestID, taskControlScope(c), "completed", in.TargetMode, s.now().UTC().Add(time.Minute), c.UpdatedAt}
	response, _ := json.Marshal(out)
	if e = q.SaveTaskControlConnectionRequest(ctx, db.SaveTaskControlConnectionRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID, BindingID: id, ActorID: a.UserID, Action: "mode", BodyHash: hs, Response: response}); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) TaskControlBootstrap(ctx context.Context, token string) (any, error) {
	c, _, e := s.AuthorizeTaskControlRuntime(ctx, token)
	if e != nil {
		return nil, e
	}
	out := map[string]any{"scope": c.Scope, "mode": c.Mode, "state": c.State, "installation": c.Installation, "minRuntimeVersion": "1.1.0"}
	if c.Mode != "managed" {
		return out, nil
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.LockTaskControlAccount(ctx, c.Scope.AccountID); e != nil {
		return nil, e
	}
	row, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: c.Scope.CompanyID, ID: c.Scope.BindingID})
	if e != nil {
		return nil, e
	}
	if row.State != "active" || row.Mode != "managed" || row.BindingRevision != c.Scope.BindingRevision || row.ModeEpoch != c.Scope.ModeEpoch {
		return nil, forbidden("Сессия подключения отозвана")
	}
	if !row.PublicationID.Valid {
		return nil, conflict("Нет опубликованных правил")
	}
	p, e := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: row.CompanyID, BindingID: row.ID, ID: row.PublicationID.UUID})
	if e != nil {
		return nil, e
	}
	var pub TaskControlPublication
	if e = json.Unmarshal(p.Payload, &pub); e != nil {
		return nil, e
	}
	pub.Scope = c.Scope
	legacy, _ := tc.Legacy(pub.Rules)
	out["publication"] = struct {
		TaskControlPublication
		RuntimeRules []tc.LegacyRule `json:"runtimeRules"`
	}{pub, legacy}
	return out, nil
}
