package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type TaskControlSourceRevisions struct {
	CRM          string `json:"crm"`
	Organization int64  `json:"organization"`
	Mapping      int64  `json:"mapping"`
}
type TaskControlDraftV2 struct {
	Scope         TaskControlScope `json:"scope"`
	Revision      int64            `json:"revision"`
	SchemaVersion int              `json:"schemaVersion"`
	IntentRules   []tc.IntentRule  `json:"intentRules"`
	Checksum      string           `json:"checksum"`
	Issues        []tc.Issue       `json:"issues"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	UpdatedBy     uuid.UUID        `json:"updatedBy"`
}
type TaskControlDraftInputV2 struct {
	ExpectedRevision        int64           `json:"expectedRevision"`
	ExpectedBindingRevision int64           `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64           `json:"expectedModeEpoch"`
	IntentRules             []tc.IntentRule `json:"intentRules"`
}
type TaskControlValidateInputV2 struct {
	ExpectedDraftRevision int64 `json:"expectedDraftRevision"`
}
type TaskControlPublishInputV2 struct {
	RequestID               uuid.UUID                   `json:"requestId"`
	ExpectedDraftRevision   int64                       `json:"expectedDraftRevision"`
	ExpectedChecksum        string                      `json:"expectedChecksum"`
	ExpectedBindingRevision int64                       `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64                       `json:"expectedModeEpoch"`
	ExpectedSourceRevisions *TaskControlSourceRevisions `json:"expectedSourceRevisions,omitempty"`
	Reason                  string                      `json:"reason"`
}
type TaskControlPublicationV2 struct {
	ID                 uuid.UUID        `json:"id"`
	Version            int64            `json:"version"`
	SchemaVersion      int              `json:"schemaVersion"`
	ScopeAtPublication TaskControlScope `json:"scopeAtPublication"`
	DraftRevision      int64            `json:"draftRevision"`
	IntentRules        []tc.IntentRule  `json:"intentRules"`
	IntentChecksum     string           `json:"intentChecksum"`
	CreatedAt          time.Time        `json:"createdAt"`
	CreatedBy          uuid.UUID        `json:"createdBy"`
	Reason             string           `json:"reason"`
	RestoredFromID     *uuid.UUID       `json:"restoredFromId"`
	InitialSnapshotID  uuid.UUID        `json:"initialSnapshotId"`
	RuntimeRevision    int64            `json:"runtimeRevision"`
}
type TaskControlResolvedSnapshot struct {
	SnapshotID         uuid.UUID                  `json:"snapshotId"`
	Scope              TaskControlScope           `json:"scope"`
	PublicationID      uuid.UUID                  `json:"publicationId"`
	PublicationVersion int64                      `json:"publicationVersion"`
	IntentChecksum     string                     `json:"intentChecksum"`
	RuntimeRevision    int64                      `json:"runtimeRevision"`
	SourceRevisions    TaskControlSourceRevisions `json:"sourceRevisions"`
	ResolverVersion    int                        `json:"resolverVersion"`
	ResolvedChecksum   string                     `json:"resolvedChecksum"`
	ResolvedAt         time.Time                  `json:"resolvedAt"`
	SourceFreshUntil   time.Time                  `json:"sourceFreshUntil"`
	Issues             []tc.IntentIssue           `json:"issues"`
	RuntimeRules       []tc.LegacyRule            `json:"runtimeRules"`
	Rules              []tc.Rule                  `json:"resolvedRules"`
}
type TaskControlActivateInputV2 struct {
	RequestID               uuid.UUID `json:"requestId"`
	ExpectedBindingRevision int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64     `json:"expectedModeEpoch"`
}

func taskControlRequiresV2() error {
	return coded(ErrorConflict, "requires_v2", "Подключение требует API версии 2")
}
func taskControlV2Required(c db.TaskControlConnection) error {
	if c.RuntimeSchemaVersion != 2 {
		return coded(ErrorConflict, "unsupported_schema", "Сначала активируйте runtime версии 2")
	}
	return nil
}
func intentDraftDTO(c db.TaskControlConnection, d db.TaskControlDraft) (TaskControlDraftV2, error) {
	rules := []tc.IntentRule{}
	if d.SchemaVersion == 2 {
		if e := json.Unmarshal(d.Rules, &rules); e != nil {
			return TaskControlDraftV2{}, e
		}
	} else {
		var legacy []tc.Rule
		if e := json.Unmarshal(d.Rules, &legacy); e != nil {
			return TaskControlDraftV2{}, e
		}
		for i := range legacy {
			r := legacy[i]
			rules = append(rules, tc.IntentRule{Kind: "legacyExplicit", Legacy: &r})
		}
	}
	sum, e := tc.IntentChecksum(rules)
	return TaskControlDraftV2{taskControlScope(c), d.Revision, 2, rules, sum, tc.ValidateIntentRules(rules), d.UpdatedAt, d.UpdatedBy}, e
}
func (s *Service) GetTaskControlDraftV2(ctx context.Context, a Actor, id uuid.UUID) (TaskControlDraftV2, error) {
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, false)
	if e != nil {
		return TaskControlDraftV2{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, e := q.GetTaskControlDraft(ctx, db.GetTaskControlDraftParams{CompanyID: a.CompanyID, BindingID: id})
	if isNoRows(e) {
		sum, _ := tc.IntentChecksum([]tc.IntentRule{})
		return TaskControlDraftV2{Scope: taskControlScope(c), Revision: 1, SchemaVersion: 2, IntentRules: []tc.IntentRule{}, Checksum: sum, Issues: []tc.Issue{}, UpdatedAt: c.UpdatedAt, UpdatedBy: c.LinkedBy}, nil
	}
	if e != nil {
		return TaskControlDraftV2{}, e
	}
	return intentDraftDTO(c, d)
}
func (s *Service) UpdateTaskControlDraftV2(ctx context.Context, a Actor, id uuid.UUID, in TaskControlDraftInputV2) (TaskControlDraftV2, error) {
	sum, e := tc.IntentChecksum(in.IntentRules)
	if e != nil {
		return TaskControlDraftV2{}, coded(ErrorValidation, "invalid_selection", e.Error())
	}
	raw, e := json.Marshal(in.IntentRules)
	if e != nil || len(raw) > tc.IntentMaxBytes {
		return TaskControlDraftV2{}, coded(ErrorPayloadTooLarge, "runtime_payload_too_large", "Намерение превышает 1 МиБ")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return TaskControlDraftV2{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return TaskControlDraftV2{}, e
	}
	if e = taskControlV2Required(c); e != nil {
		return TaskControlDraftV2{}, e
	}
	d, e := q.UpdateTaskControlDraftV2(ctx, db.UpdateTaskControlDraftV2Params{CompanyID: a.CompanyID, BindingID: id, Rules: raw, Checksum: sum, UpdatedBy: a.UserID, Revision: in.ExpectedRevision})
	if isNoRows(e) {
		return TaskControlDraftV2{}, conflict("Черновик уже изменён")
	}
	if e != nil {
		return TaskControlDraftV2{}, e
	}
	out, e := intentDraftDTO(c, d)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) taskControlResolveTx(ctx context.Context, q *db.Queries, c db.TaskControlConnection, rules []tc.IntentRule, publish bool) (tc.Resolution, TaskControlSourceRevisions, time.Time, error) {
	var zero tc.Resolution
	var src TaskControlSourceRevisions
	refs, e := s.taskControlFreshReferencesTx(ctx, q, c)
	if e != nil {
		return zero, src, time.Time{}, e
	}
	if e = q.EnsureTaskControlV2ReferenceRevision(ctx, c.CompanyID); e != nil {
		return zero, src, time.Time{}, e
	}
	rev, e := q.LockTaskControlV2ReferenceRevision(ctx, c.CompanyID)
	if e != nil {
		return zero, src, time.Time{}, e
	}
	counts, e := q.TaskControlReferenceOrganizationCounts(ctx, c.CompanyID)
	if e != nil {
		return zero, src, time.Time{}, e
	}
	if counts.Employees > 10000 || counts.Departments > 1000 {
		return zero, src, time.Time{}, coded(ErrorPayloadTooLarge, "references_too_large", "Организация превышает лимит справочника")
	}
	raw, e := q.TaskControlReferenceEmployees(ctx, db.TaskControlReferenceEmployeesParams{CompanyID: c.CompanyID, AccountID: c.AccountID})
	if e != nil {
		return zero, src, time.Time{}, e
	}
	var employees []referenceEmployeeRow
	if e = json.Unmarshal(raw, &employees); e != nil {
		return zero, src, time.Time{}, e
	}
	raw, e = q.TaskControlReferenceDepartments(ctx, c.CompanyID)
	if e != nil {
		return zero, src, time.Time{}, e
	}
	var deps []TaskControlReferenceDepartment
	if e = json.Unmarshal(raw, &deps); e != nil {
		return zero, src, time.Time{}, e
	}
	org := tc.OrganizationSnapshot{CompanyID: c.CompanyID, AccountID: c.AccountID, OrganizationRevision: rev.OrganizationRevision, MappingRevision: rev.MappingRevision, Employees: []tc.OrganizationEmployee{}, Departments: []tc.OrganizationDepartment{}, Mappings: []tc.EmployeeMapping{}}
	for _, v := range employees {
		org.Employees = append(org.Employees, tc.OrganizationEmployee{ID: v.EmployeeID, Active: v.Active, DepartmentIDs: v.DepartmentIDs})
		for _, crm := range v.CRMUserIDs {
			org.Mappings = append(org.Mappings, tc.EmployeeMapping{EmployeeID: v.EmployeeID, AccountID: c.AccountID, CRMUserID: crm})
		}
	}
	for _, v := range deps {
		org.Departments = append(org.Departments, tc.OrganizationDepartment{ID: v.ID, ParentID: v.ParentID})
	}
	src = TaskControlSourceRevisions{refs.Revision, rev.OrganizationRevision, rev.MappingRevision}
	result, e := tc.ResolveIntentRules(rules, refs, org, publish)
	if e == nil && !refs.FreshUntil.After(s.now()) {
		return zero, src, time.Time{}, coded(ErrorUpstream, "references_unavailable", "Справочники устарели во время пересчёта")
	}
	return result, src, refs.FreshUntil, e
}
func (s *Service) ValidateTaskControlDraftV2(ctx context.Context, a Actor, id uuid.UUID, in TaskControlValidateInputV2) (any, error) {
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = taskControlV2Required(c); e != nil {
		return nil, e
	}
	d, e := q.GetTaskControlDraft(ctx, db.GetTaskControlDraftParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return nil, e
	}
	out, e := intentDraftDTO(c, d)
	if e != nil {
		return nil, e
	}
	if in.ExpectedDraftRevision != d.Revision {
		return nil, conflict("Черновик изменился")
	}
	resolved, src, _, e := s.taskControlResolveTx(ctx, q, c, out.IntentRules, true)
	if e != nil {
		return nil, e
	}
	return map[string]any{"valid": !resolved.HasBlockingIssues(), "intentChecksum": out.Checksum, "sourceRevisions": src, "issues": resolved.Issues, "resolvedPreview": resolved.RuntimeRules}, nil
}
func requestBodyHash(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Service) saveTaskControlSnapshotV2(ctx context.Context, q *db.Queries, c db.TaskControlConnection, pub TaskControlPublicationV2, resolved tc.Resolution, src TaskControlSourceRevisions, fresh time.Time) (TaskControlResolvedSnapshot, error) {
	if !fresh.After(s.now()) {
		return TaskControlResolvedSnapshot{}, coded(ErrorUpstream, "references_unavailable", "Справочники устарели во время пересчёта")
	}
	state, e := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: c.CompanyID, BindingID: c.ID})
	if e != nil && !isNoRows(e) {
		return TaskControlResolvedSnapshot{}, e
	}
	revision := state.RuntimeRevision + 1
	if revision > 9007199254740991 {
		return TaskControlResolvedSnapshot{}, conflict("Исчерпан счётчик runtime")
	}
	snap := TaskControlResolvedSnapshot{uuid.New(), taskControlScope(c), pub.ID, pub.Version, pub.IntentChecksum, revision, src, 1, resolved.ResolvedChecksum, s.now().UTC(), fresh, resolved.Issues, resolved.RuntimeRules, resolved.Rules}
	raw, e := json.Marshal(snap)
	if e != nil {
		return snap, e
	}
	wire := TaskControlRuntimeEnvelopeV2{RuntimeSchemaVersion: 2, Scope: snap.Scope, ActorID: "9007199254740991", PublicationID: pub.ID, PublicationVersion: pub.Version, IntentChecksum: pub.IntentChecksum, SnapshotID: snap.SnapshotID, RuntimeRevision: revision, SourceRevisions: src, ResolverVersion: 1, ResolvedChecksum: resolved.ResolvedChecksum, PayloadChecksum: resolved.ResolvedChecksum, RuntimeRules: resolved.RuntimeRules, IssuedAt: s.now().UTC(), ValidUntil: fresh, MinRuntimeVersion: "1.2.0"}
	wireRaw, err := json.Marshal(wire)
	if err != nil {
		return snap, err
	}
	if len(wireRaw) > 1<<20 {
		return snap, coded(ErrorPayloadTooLarge, "runtime_payload_too_large", "Runtime превышает 1 МиБ")
	}
	if e = q.SaveTaskControlV2Snapshot(ctx, db.SaveTaskControlV2SnapshotParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: snap.SnapshotID, ModeEpoch: c.ModeEpoch, RuntimeRevision: revision, PublicationID: pub.ID, Payload: raw}); e != nil {
		return snap, e
	}
	e = q.SetTaskControlV2State(ctx, db.SetTaskControlV2StateParams{CompanyID: c.CompanyID, BindingID: c.ID, CurrentSnapshotID: uuid.NullUUID{UUID: snap.SnapshotID, Valid: true}, RuntimeRevision: revision, VerifiedSourceFreshUntil: pgtype.Timestamptz{Time: fresh, Valid: true}})
	if e == nil {
		e = q.ClearTaskControlV2Failure(ctx, db.ClearTaskControlV2FailureParams{CompanyID: c.CompanyID, BindingID: c.ID})
	}
	return snap, e
}
func (s *Service) publishTaskControlIntentV2(ctx context.Context, q *db.Queries, c db.TaskControlConnection, a Actor, in TaskControlPublishInputV2, rules []tc.IntentRule, revision int64, bodyHash string, restored *uuid.UUID) (TaskControlPublicationV2, error) {
	var out TaskControlPublicationV2
	result, src, fresh, e := s.taskControlResolveTx(ctx, q, c, rules, true)
	if e != nil {
		return out, e
	}
	if result.HasBlockingIssues() {
		for _, issue := range result.Issues {
			if issue.Code == "runtime_payload_too_large" {
				return out, coded(ErrorPayloadTooLarge, issue.Code, issue.Message)
			}
		}
		return out, coded(ErrorValidation, "invalid_selection", "Публикация содержит недоступные ссылки; проверьте результат проверки")
	}
	if in.ExpectedSourceRevisions != nil && *in.ExpectedSourceRevisions != src {
		return out, coded(ErrorConflict, "source_changed", "Справочники изменились")
	}
	rows, e := q.ListTaskControlPublications(ctx, db.ListTaskControlPublicationsParams{CompanyID: c.CompanyID, BindingID: c.ID})
	if e != nil {
		return out, e
	}
	version := int64(1)
	if len(rows) > 0 {
		version = rows[0].Version + 1
	}
	sum, e := tc.IntentChecksum(rules)
	if e != nil {
		return out, e
	}
	out = TaskControlPublicationV2{ID: uuid.New(), Version: version, SchemaVersion: 2, ScopeAtPublication: taskControlScope(c), DraftRevision: revision, IntentRules: rules, IntentChecksum: sum, CreatedAt: s.now().UTC(), CreatedBy: a.UserID, Reason: strings.TrimSpace(in.Reason), RestoredFromID: restored}
	snap, e := s.saveTaskControlSnapshotV2(ctx, q, c, out, result, src, fresh)
	if e != nil {
		return out, e
	}
	out.InitialSnapshotID = snap.SnapshotID
	out.RuntimeRevision = snap.RuntimeRevision
	raw, _ := json.Marshal(out)
	if _, e = q.CreateTaskControlPublication(ctx, db.CreateTaskControlPublicationParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: out.ID, RequestID: in.RequestID, BodyHash: bodyHash, Payload: raw}); e != nil {
		return out, e
	}
	_, e = q.SetTaskControlPublication(ctx, db.SetTaskControlPublicationParams{CompanyID: c.CompanyID, ID: c.ID, PublicationID: uuid.NullUUID{UUID: out.ID, Valid: true}})
	return out, e
}
func (s *Service) PublishTaskControlDraftV2(ctx context.Context, a Actor, id uuid.UUID, in TaskControlPublishInputV2) (TaskControlPublicationV2, error) {
	var out TaskControlPublicationV2
	if in.RequestID == uuid.Nil || utf8.RuneCountInString(in.Reason) > 1000 {
		return out, validation("Некорректная операция публикации")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = taskControlV2Required(c); e != nil {
		return out, e
	}
	hash := requestBodyHash([]any{id, in})
	prior, e := q.GetTaskControlPublishRequest(ctx, db.GetTaskControlPublishRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID})
	if e == nil {
		if prior.BindingID != id || prior.BodyHash != hash {
			return out, conflict("Ключ операции уже использован")
		}
		e = json.Unmarshal(prior.Payload, &out)
		if out.CreatedBy != a.UserID {
			return TaskControlPublicationV2{}, forbidden("Операция принадлежит другому пользователю")
		}
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
	draft, e := intentDraftDTO(c, d)
	if e != nil {
		return out, e
	}
	if draft.Revision != in.ExpectedDraftRevision || draft.Checksum != in.ExpectedChecksum {
		return out, conflict("Черновик изменился")
	}
	out, e = s.publishTaskControlIntentV2(ctx, q, c, a, in, draft.IntentRules, d.Revision, hash, nil)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

type TaskControlPublicationOptionsV2 struct {
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

func (s *Service) TaskControlPublicationsV2(ctx context.Context, a Actor, id, pubID uuid.UUID, options ...TaskControlPublicationOptionsV2) (any, error) {
	tx, q, _, e := s.taskControlRulesTx(ctx, a, id, false)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if pubID != uuid.Nil {
		p, e := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: pubID})
		if e != nil {
			return nil, notFound("Публикация")
		}
		var meta struct {
			SchemaVersion int `json:"schemaVersion"`
		}
		_ = json.Unmarshal(p.Payload, &meta)
		if meta.SchemaVersion != 2 {
			return nil, coded(ErrorConflict, "unsupported_schema", "Публикация имеет схему версии 1")
		}
		return json.RawMessage(p.Payload), nil
	}
	opts := TaskControlPublicationOptionsV2{Limit: 100}
	if len(options) > 0 {
		opts = options[0]
		if opts.Limit == 0 {
			opts.Limit = 100
		}
	}
	if opts.Limit < 1 || opts.Limit > 100 {
		return nil, validation("Некорректный размер страницы")
	}
	var cursor uuid.NullUUID
	if opts.Cursor != "" {
		cursorID, e := uuid.Parse(opts.Cursor)
		if e != nil {
			return nil, validation("Некорректный курсор")
		}
		cursor = uuid.NullUUID{UUID: cursorID, Valid: true}
		if _, e = q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: cursor.UUID}); e != nil {
			return nil, notFound("Курсор публикации")
		}
	}
	rows, e := q.ListTaskControlPublicationsV2(ctx, db.ListTaskControlPublicationsV2Params{CompanyID: a.CompanyID, BindingID: id, CursorID: cursor, PageLimit: int32(opts.Limit + 1)})
	if e != nil {
		return nil, e
	}
	more := len(rows) > opts.Limit
	if more {
		rows = rows[:opts.Limit]
	}
	items := []json.RawMessage{}
	for _, row := range rows {
		items = append(items, row.Payload)
	}
	out := map[string]any{"items": items}
	if more && len(rows) > 0 {
		out["nextCursor"] = rows[len(rows)-1].ID
	}
	return out, nil
}
func taskControlV2Descriptor(st tc.Installation) bool {
	if !taskControlRuntimeV2Version(st.RuntimeVersion) {
		return false
	}
	for _, v := range st.RuntimeCapabilities {
		if v == "dynamicRulesV2" {
			return true
		}
	}
	return false
}
func (s *Service) ActivateTaskControlV2(ctx context.Context, a Actor, id uuid.UUID, in TaskControlActivateInputV2) (any, error) {
	if in.RequestID == uuid.Nil {
		return nil, validation("Требуется ID операции")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	hash := requestBodyHash([]any{id, in})
	prior, e := q.GetTaskControlConnectionRequest(ctx, db.GetTaskControlConnectionRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID})
	if e == nil {
		if prior.ActorID != a.UserID || prior.BindingID != id || prior.Action != "activateV2" || prior.BodyHash != hash {
			return nil, conflict("Ключ операции уже использован")
		}
		return json.RawMessage(prior.Response), nil
	}
	if !isNoRows(e) {
		return nil, e
	}
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return nil, e
	}
	if c.RuntimeSchemaVersion == 2 {
		return nil, conflict("Версия 2 уже активирована")
	}
	snapshot, e := q.GetTaskControlSnapshot(ctx, db.GetTaskControlSnapshotParams{CompanyID: c.CompanyID, AccountID: c.AccountID})
	if e != nil {
		return nil, e
	}
	var st tc.Installation
	if json.Unmarshal(snapshot.Payload, &st) != nil || !taskControlV2Descriptor(st) || !st.FreshUntil.After(s.now()) {
		return nil, coded(ErrorConflict, "runtime_upgrade_required", "Нужен подтверждённый runtime с поддержкой dynamicRulesV2")
	}
	if c.Mode != "standalone" {
		return nil, coded(ErrorConflict, "mode_transition_required", "Сначала переключите подключение в автономный режим")
	}
	old := c
	c, e = q.ActivateTaskControlV2(ctx, db.ActivateTaskControlV2Params{CompanyID: a.CompanyID, ID: id})
	if e != nil {
		return nil, e
	}
	if e = q.DeleteTaskControlRuntimeSessions(ctx, db.DeleteTaskControlRuntimeSessionsParams{CompanyID: a.CompanyID, BindingID: id}); e != nil {
		return nil, e
	}
	// Carry the verified CRM snapshot across an epoch migration; Core reference scope is unchanged.
	if cached, err := q.GetTaskControlReferenceCache(ctx, db.GetTaskControlReferenceCacheParams{CompanyID: a.CompanyID, BindingID: id, ScopeKey: referenceScopeKey(old)}); err == nil {
		if e = q.SaveTaskControlReferenceCache(ctx, db.SaveTaskControlReferenceCacheParams{CompanyID: a.CompanyID, BindingID: id, ScopeKey: referenceScopeKey(c), Payload: cached.Payload, LastError: cached.LastError}); e != nil {
			return nil, e
		}
	}
	out := map[string]any{"scope": taskControlScope(c), "runtimeSchemaVersion": 2, "state": "completed"}
	raw, _ := json.Marshal(out)
	if e = q.SaveTaskControlConnectionRequest(ctx, db.SaveTaskControlConnectionRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID, BindingID: id, ActorID: a.UserID, Action: "activateV2", BodyHash: hash, Response: raw}); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}

// RunTaskControlResolver reconciles only active publications. Durable jobs survive restarts.
func (s *Service) RunTaskControlResolver(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	nextSweep := time.Time{}
	for {
		q := db.New(s.pool)
		if !s.now().Before(nextSweep) {
			if e := q.QueueTaskControlV2Jobs(ctx); e != nil && ctx.Err() == nil {
				s.logger.WarnContext(ctx, "Не удалось поставить пересчёт правил", "error", e)
			}
			nextSweep = s.now().Add(time.Minute)
			_ = q.PruneTaskControlV2History(ctx)
			_ = q.PruneTaskControlV2Issued(ctx)
			_ = q.PruneTaskControlV2Snapshots(ctx)
		}
		if e := q.DrainTaskControlV2SourceChanges(ctx); e != nil && ctx.Err() == nil {
			s.logger.WarnContext(ctx, "Не удалось обработать изменения справочников", "error", e)
		}
		for {
			worked, e := s.resolveTaskControlV2Job(ctx)
			if e != nil && ctx.Err() == nil {
				s.logger.WarnContext(ctx, "Не удалось пересчитать правила", "error", e)
				break
			}
			if !worked || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) resolveTaskControlV2Job(ctx context.Context) (bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	job, e := q.ClaimTaskControlV2Job(ctx)
	if isNoRows(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: job.CompanyID, ID: job.BindingID})
	if e != nil {
		return true, e
	}
	if e = q.LockTaskControlAccount(ctx, c.AccountID); e != nil {
		return true, e
	}
	c, e = q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: job.CompanyID, ID: job.BindingID})
	if e != nil {
		return true, e
	}
	if c.State == "active" && c.Mode == "managed" && c.RuntimeSchemaVersion == 2 && c.ModeEpoch == job.ModeEpoch && c.PublicationID.Valid && c.PublicationID.UUID == job.PublicationID && s.taskControlAccount(ctx, q, c.CompanyID, c.AccountID) == nil {
		p, err := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: job.PublicationID})
		if err != nil {
			return true, err
		}
		var pub TaskControlPublicationV2
		if json.Unmarshal(p.Payload, &pub) == nil && pub.SchemaVersion == 2 {
			result, src, fresh, err := s.taskControlResolveTx(ctx, q, c, pub.IntentRules, false)
			if err == nil && !result.HasBlockingIssues() {
				state, se := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: c.CompanyID, BindingID: c.ID})
				same := false
				if se == nil && state.CurrentSnapshotID.Valid {
					row, re := q.GetTaskControlV2Snapshot(ctx, db.GetTaskControlV2SnapshotParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: state.CurrentSnapshotID.UUID})
					var snap TaskControlResolvedSnapshot
					if re == nil && json.Unmarshal(row.Payload, &snap) == nil {
						same = snap.Scope == taskControlScope(c) && snap.PublicationID == pub.ID && snap.SourceRevisions == src
					}
				}
				if same && fresh.After(s.now()) {
					e = q.SetTaskControlV2State(ctx, db.SetTaskControlV2StateParams{CompanyID: c.CompanyID, BindingID: c.ID, CurrentSnapshotID: state.CurrentSnapshotID, RuntimeRevision: state.RuntimeRevision, VerifiedSourceFreshUntil: pgtype.Timestamptz{Time: fresh, Valid: true}})
				} else {
					_, e = s.saveTaskControlSnapshotV2(ctx, q, c, pub, result, src, fresh)
				}
				if e != nil {
					var appErr *Error
					if errors.As(e, &appErr) {
						if e = q.RecordTaskControlV2Failure(ctx, db.RecordTaskControlV2FailureParams{CompanyID: c.CompanyID, BindingID: c.ID, Code: appErr.Code, Message: appErr.Message}); e != nil {
							return true, e
						}
					} else {
						return true, e
					}
				} else if e = q.ClearTaskControlV2Failure(ctx, db.ClearTaskControlV2FailureParams{CompanyID: c.CompanyID, BindingID: c.ID}); e != nil {
					return true, e
				}
			} else {
				code := "references_unavailable"
				message := "Источники пересчёта недоступны"
				var appErr *Error
				if errors.As(err, &appErr) {
					code = appErr.Code
					message = appErr.Message
				}
				for _, issue := range result.Issues {
					if issue.Blocking {
						code = issue.Code
						message = issue.Message
						break
					}
				}
				if e = q.RecordTaskControlV2Failure(ctx, db.RecordTaskControlV2FailureParams{CompanyID: c.CompanyID, BindingID: c.ID, Code: code, Message: message}); e != nil {
					return true, e
				}
			}
		}
	}
	if e = q.DeleteTaskControlV2Job(ctx, db.DeleteTaskControlV2JobParams{CompanyID: job.CompanyID, BindingID: job.BindingID}); e != nil {
		return true, e
	}
	return true, tx.Commit(ctx)
}

func (s *Service) taskControlV2DescriptorTx(ctx context.Context, q *db.Queries, c db.TaskControlConnection) error {
	row, e := q.GetTaskControlSnapshot(ctx, db.GetTaskControlSnapshotParams{CompanyID: c.CompanyID, AccountID: c.AccountID})
	var st tc.Installation
	if e != nil || json.Unmarshal(row.Payload, &st) != nil || !st.Valid(c.CompanyID, c.AccountID, s.now()) || !taskControlV2Descriptor(st) {
		return coded(ErrorConflict, "runtime_upgrade_required", "Требуется подтверждённый runtime версии 1.2.0")
	}
	return taskControlInstalled(st, c.InstallationID, c.IntegrationID, c.Generation, s.now())
}
