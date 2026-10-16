package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type TaskControlCore interface {
	Installation(context.Context, uuid.UUID, string) (tc.Installation, error)
}

func WithTaskControl(core TaskControlCore, keys map[string]ed25519.PublicKey) ServiceOption {
	return func(s *Service) { s.taskControlCore = core; s.taskControlKeys = keys }
}

type TaskControlScope struct {
	CompanyID       uuid.UUID `json:"companyId"`
	BindingID       uuid.UUID `json:"bindingId"`
	BindingRevision int64     `json:"bindingRevision"`
	InstallationID  uuid.UUID `json:"installationId"`
	IntegrationID   uuid.UUID `json:"integrationId"`
	AccountID       string    `json:"accountId"`
	Product         string    `json:"product"`
	ModeEpoch       int64     `json:"modeEpoch"`
}
type TaskControlConnection struct {
	Scope        TaskControlScope `json:"scope"`
	State        string           `json:"state"`
	Mode         string           `json:"mode"`
	UpdatedAt    time.Time        `json:"updatedAt"`
	Installation tc.Installation  `json:"installation"`
}
type TaskControlIntent struct {
	ID         uuid.UUID        `json:"id"`
	Scope      TaskControlScope `json:"scope"`
	State      string           `json:"state"`
	TargetMode string           `json:"targetMode"`
	ExpiresAt  time.Time        `json:"expiresAt"`
	UpdatedAt  time.Time        `json:"updatedAt"`
}
type TaskControlLinkInput struct {
	IntentID       uuid.UUID `json:"intentId"`
	InstallationID uuid.UUID `json:"installationId"`
	IntegrationID  uuid.UUID `json:"integrationId"`
	AccountID      string    `json:"accountId"`
	CoreAssertion  string    `json:"coreAssertion"`
}
type TaskControlRevokeInput struct {
	RequestID               uuid.UUID `json:"requestId"`
	ExpectedBindingRevision int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64     `json:"expectedModeEpoch"`
	CoreAssertion           string    `json:"coreAssertion"`
}

func taskControlScope(c db.TaskControlConnection) TaskControlScope {
	return TaskControlScope{c.CompanyID, c.ID, c.BindingRevision, c.InstallationID, c.IntegrationID, c.AccountID, tc.Product, c.ModeEpoch}
}
func taskControlIntent(c db.TaskControlConnection) TaskControlIntent {
	return TaskControlIntent{c.IntentID, taskControlScope(c), "completed", "standalone", c.UpdatedAt.Add(time.Minute), c.UpdatedAt}
}
func (s *Service) taskControlActor(ctx context.Context, q *db.Queries, a Actor, manage bool) (Actor, error) {
	a, p, e := s.taskControlPermissions(ctx, q, a)
	if e != nil {
		return a, e
	}
	if !p.Read || (manage && !p.Manage) {
		return a, coded(ErrorForbidden, "task_control_access_denied", "Недостаточно прав Контроля задач")
	}
	return a, nil
}
func (s *Service) taskControlPermissions(ctx context.Context, q *db.Queries, a Actor) (Actor, tc.Permissions, error) {
	none := tc.Permissions{}
	company, e := q.GetCompany(ctx, a.CompanyID)
	if e != nil {
		return a, none, notFound("Компания")
	}
	if company.Status != "active" {
		return a, none, forbidden("Компания недоступна")
	}
	user, e := q.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{CompanyID: a.CompanyID, ID: a.UserID})
	if e != nil || user.Status != "active" || user.ExternalDeletedAt.Valid {
		return a, none, forbidden("Пользователь недоступен")
	}
	a.Role = user.Role
	a.SectionAccess = user.SectionAccess
	var override *tc.Permissions
	p, e := q.GetTaskControlPermissions(ctx, db.GetTaskControlPermissionsParams{CompanyID: a.CompanyID, UserID: a.UserID})
	if e == nil {
		override = &tc.Permissions{Read: p.CanRead, Manage: p.CanManage}
	} else if !isNoRows(e) {
		return a, none, internal("Не удалось проверить права Контроля задач", e)
	}
	return a, tc.Access(a.Role, override), nil
}
func (s *Service) taskControlAccount(ctx context.Context, q *db.Queries, company uuid.UUID, account string) error {
	if !tc.ValidCRMID(account) {
		return validation("Некорректный аккаунт")
	}
	row, e := q.GetCompanyIntegrationByExternalAccount(ctx, db.GetCompanyIntegrationByExternalAccountParams{Provider: "rakurs", ExternalAccountID: account})
	if e != nil || row.CompanyID != company {
		return notFound("Аккаунт")
	}
	if row.Status != "active" {
		return forbidden("Аккаунт отключён")
	}
	c, e := q.GetCompany(ctx, company)
	if e != nil || c.Status != "active" {
		return forbidden("Компания недоступна")
	}
	return nil
}
func (s *Service) taskControlStatus(ctx context.Context, company uuid.UUID, account string, force bool) (tc.Installation, error) {
	now := s.now().UTC()
	q := db.New(s.pool)
	unknown := tc.Installation{CompanyID: company, AccountID: account, Product: tc.Product, State: "unknown", CheckedAt: now, FreshUntil: now, ReasonCode: "core_unavailable"}
	if e := s.taskControlAccount(ctx, q, company, account); e != nil {
		return unknown, e
	}
	cached, e := q.GetTaskControlSnapshot(ctx, db.GetTaskControlSnapshotParams{CompanyID: company, AccountID: account})
	var prior tc.Installation
	if e == nil && json.Unmarshal(cached.Payload, &prior) == nil && !force && prior.Valid(company, account, now) {
		if e = s.retireTaskControlInstallation(ctx, company, account); e != nil {
			return unknown, e
		}
		return prior, nil
	}
	if e != nil && !isNoRows(e) {
		return unknown, internal("Не удалось прочитать статус установки", e)
	}
	if s.taskControlCore == nil {
		return unknown, nil
	}
	value, e := s.taskControlCore.Installation(ctx, company, account)
	if e != nil || !value.Valid(company, account, s.now()) {
		return unknown, nil
	}
	// Refuse a regressing status from the same installation. Unknown never becomes uninstall.
	if prior.InstallationID != nil && value.InstallationID != nil && *prior.InstallationID == *value.InstallationID && value.Revision < prior.Revision {
		return unknown, nil
	}
	body, _ := json.Marshal(value)
	e = q.SaveTaskControlSnapshot(ctx, db.SaveTaskControlSnapshotParams{CompanyID: company, AccountID: account, Payload: body, CheckedAt: value.CheckedAt, FreshUntil: value.FreshUntil})
	if e != nil {
		return unknown, internal("Не удалось сохранить статус установки", e)
	}
	// A later concurrent read may have stored newer evidence.
	saved, e := q.GetTaskControlSnapshot(ctx, db.GetTaskControlSnapshotParams{CompanyID: company, AccountID: account})
	if e != nil || json.Unmarshal(saved.Payload, &value) != nil {
		return unknown, internal("Не удалось перечитать статус установки", e)
	}
	if e = s.retireTaskControlInstallation(ctx, company, account); e != nil {
		return unknown, e
	}
	return value, nil
}

// Only fresh positive evidence retires a binding. Retain its draft/publications
// for history and require an explicit link for the next installation generation.
func (s *Service) retireTaskControlInstallation(ctx context.Context, company uuid.UUID, account string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.LockTaskControlAccount(ctx, account); e != nil {
		return e
	}
	row, e := q.GetTaskControlSnapshot(ctx, db.GetTaskControlSnapshotParams{CompanyID: company, AccountID: account})
	if e != nil {
		return e
	}
	var st tc.Installation
	if json.Unmarshal(row.Payload, &st) != nil || !st.Valid(company, account, s.now()) || st.State == "unknown" {
		return nil
	}
	connections, e := q.ListTaskControlConnections(ctx, company)
	if e != nil {
		return e
	}
	for _, c := range connections {
		if c.AccountID != account || c.State != "active" {
			continue
		}
		changed := st.State == "notInstalled" || (st.State == "installed" && (*st.InstallationID != c.InstallationID || *st.IntegrationID != c.IntegrationID || st.Generation != c.Generation))
		if !changed {
			continue
		}
		if _, e = q.RevokeTaskControlConnection(ctx, db.RevokeTaskControlConnectionParams{CompanyID: company, ID: c.ID, BindingRevision: c.BindingRevision, ModeEpoch: c.ModeEpoch}); e != nil {
			return e
		}
		if e = q.DeleteTaskControlRuntimeSessions(ctx, db.DeleteTaskControlRuntimeSessionsParams{CompanyID: company, BindingID: c.ID}); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func taskControlInstalled(status tc.Installation, installation, integration uuid.UUID, generation int64, now time.Time) error {
	if status.State == "notInstalled" {
		return forbidden("Контроль задач не установлен")
	}
	if !status.Valid(status.CompanyID, status.AccountID, now) || status.State != "installed" {
		return upstream("Статус установки Контроля задач не подтверждён", nil)
	}
	if *status.InstallationID != installation || *status.IntegrationID != integration || status.Generation != generation {
		return coded(ErrorForbidden, "task_control_installation_changed", "Установка изменилась; требуется новое подключение")
	}
	return nil
}

func (s *Service) currentTaskControlInstallation(ctx context.Context, q *db.Queries, company uuid.UUID, account string, installation, integration uuid.UUID, generation int64) error {
	row, e := q.GetTaskControlSnapshot(ctx, db.GetTaskControlSnapshotParams{CompanyID: company, AccountID: account})
	var st tc.Installation
	if e != nil || json.Unmarshal(row.Payload, &st) != nil || !st.Valid(company, account, s.now()) {
		return upstream("Статус установки Контроля задач не подтверждён", e)
	}
	return taskControlInstalled(st, installation, integration, generation, s.now())
}
func (s *Service) GetTaskControlInstallation(ctx context.Context, a Actor, account string) (tc.Installation, error) {
	if _, e := s.taskControlActor(ctx, db.New(s.pool), a, false); e != nil {
		return tc.Installation{}, e
	}
	return s.taskControlStatus(ctx, a.CompanyID, account, false)
}
func (s *Service) ListTaskControlConnections(ctx context.Context, a Actor) ([]TaskControlConnection, error) {
	q := db.New(s.pool)
	if _, e := s.taskControlActor(ctx, q, a, false); e != nil {
		return nil, e
	}
	rows, e := q.ListTaskControlConnections(ctx, a.CompanyID)
	if e != nil {
		return nil, internal("Не удалось прочитать подключения", e)
	}
	out := make([]TaskControlConnection, 0, len(rows))
	for _, c := range rows {
		st, e := s.taskControlStatus(ctx, a.CompanyID, c.AccountID, false)
		if e != nil {
			return nil, e
		}
		c, e = q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: a.CompanyID, ID: c.ID})
		if e != nil {
			return nil, e
		}
		out = append(out, TaskControlConnection{taskControlScope(c), c.State, c.Mode, c.UpdatedAt, st})
	}
	return out, nil
}
func (s *Service) GetTaskControlIntent(ctx context.Context, a Actor, id uuid.UUID) (TaskControlIntent, error) {
	q := db.New(s.pool)
	if _, e := s.taskControlActor(ctx, q, a, true); e != nil {
		return TaskControlIntent{}, e
	}
	c, e := q.GetTaskControlIntent(ctx, db.GetTaskControlIntentParams{CompanyID: a.CompanyID, IntentID: id})
	if e != nil {
		if !isNoRows(e) {
			return TaskControlIntent{}, internal("Не удалось прочитать намерение", e)
		}
		receipt, err := q.GetTaskControlConnectionRequest(ctx, db.GetTaskControlConnectionRequestParams{CompanyID: a.CompanyID, RequestID: id})
		if err != nil || receipt.Action != "mode" || receipt.ActorID != a.UserID {
			return TaskControlIntent{}, notFound("Подключение")
		}
		var out TaskControlIntent
		if err = json.Unmarshal(receipt.Response, &out); err != nil {
			return out, internal("Не удалось прочитать намерение", err)
		}
		if err = s.taskControlAccount(ctx, q, a.CompanyID, out.Scope.AccountID); err != nil {
			return TaskControlIntent{}, err
		}
		return out, nil
	}
	if c.LinkedBy != a.UserID {
		return TaskControlIntent{}, notFound("Намерение")
	}
	if e = s.taskControlAccount(ctx, q, a.CompanyID, c.AccountID); e != nil {
		return TaskControlIntent{}, e
	}
	return taskControlIntent(c), nil
}
func (s *Service) taskControlAssertion(raw, purpose string) (tc.Assertion, string, error) {
	a, k, e := tc.Verify(raw, s.taskControlKeys, s.now())
	if e != nil {
		return a, k, coded(ErrorUnauthenticated, "task_control_assertion_invalid", "Подтверждение Core недействительно или истекло")
	}
	if a.Purpose != purpose || (purpose != "runtime" && !a.Admin) {
		return a, k, forbidden("Недостаточно прав подтверждения Core")
	}
	return a, k, nil
}
func (s *Service) consumeTC(ctx context.Context, q *db.Queries, a tc.Assertion, key string) error {
	if s.now().Unix() >= a.ExpiresAt {
		return coded(ErrorUnauthenticated, "task_control_assertion_invalid", "Подтверждение Core истекло")
	}
	n, e := q.ConsumeTaskControlAssertion(ctx, db.ConsumeTaskControlAssertionParams{KeyID: key, Jti: uuid.MustParse(a.ID), CompanyID: a.CompanyID, ExpiresAt: time.Unix(a.ExpiresAt, 0)})
	if e != nil {
		return internal("Не удалось принять подтверждение Core", e)
	}
	if n != 1 {
		return coded(ErrorUnauthenticated, "task_control_assertion_replayed", "Подтверждение Core уже использовано")
	}
	return nil
}
func (s *Service) LinkTaskControl(ctx context.Context, actor Actor, in TaskControlLinkInput) (TaskControlIntent, error) {
	var out TaskControlIntent
	q := db.New(s.pool)
	if _, e := s.taskControlActor(ctx, q, actor, true); e != nil {
		return out, e
	}
	if in.IntentID == uuid.Nil || in.InstallationID == uuid.Nil || in.IntegrationID == uuid.Nil || !tc.ValidCRMID(in.AccountID) {
		return out, validation("Некорректное подключение")
	}
	if e := s.taskControlAccount(ctx, q, actor.CompanyID, in.AccountID); e != nil {
		return out, e
	}
	// Lost-response recovery is restricted to the same owner, input and still-current installation.
	existing, ee := q.GetTaskControlIntent(ctx, db.GetTaskControlIntentParams{CompanyID: actor.CompanyID, IntentID: in.IntentID})
	st, e := s.taskControlStatus(ctx, actor.CompanyID, in.AccountID, true)
	if e != nil {
		return out, e
	}
	if ee == nil {
		if existing.LinkedBy != actor.UserID || existing.InstallationID != in.InstallationID || existing.IntegrationID != in.IntegrationID || existing.AccountID != in.AccountID || existing.State != "active" {
			return out, coded(ErrorConflict, "task_control_idempotency_conflict", "Намерение уже использовано")
		}
		if e = taskControlInstalled(st, existing.InstallationID, existing.IntegrationID, existing.Generation, s.now()); e != nil {
			return out, e
		}
		return taskControlIntent(existing), nil
	}
	if !isNoRows(ee) {
		return out, internal("Не удалось прочитать намерение", ee)
	}
	assertion, key, e := s.taskControlAssertion(in.CoreAssertion, "link")
	if e != nil {
		return out, e
	}
	if assertion.CompanyID != actor.CompanyID || assertion.AccountID != in.AccountID || assertion.InstallationID != in.InstallationID || assertion.IntegrationID != in.IntegrationID || assertion.RequestID != in.IntentID {
		return out, forbidden("Подтверждение относится к другому подключению")
	}
	if e = taskControlInstalled(st, in.InstallationID, in.IntegrationID, assertion.Generation, s.now()); e != nil {
		return out, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, internal("Не удалось начать подключение", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	if e = q.LockTaskControlAccount(ctx, in.AccountID); e != nil {
		return out, e
	}
	if _, e = s.taskControlActor(ctx, q, actor, true); e != nil {
		return out, e
	}
	if e = s.taskControlAccount(ctx, q, actor.CompanyID, in.AccountID); e != nil {
		return out, e
	}
	if e = s.currentTaskControlInstallation(ctx, q, actor.CompanyID, in.AccountID, in.InstallationID, in.IntegrationID, assertion.Generation); e != nil {
		return out, e
	}
	if saved, err := q.GetTaskControlIntent(ctx, db.GetTaskControlIntentParams{CompanyID: actor.CompanyID, IntentID: in.IntentID}); err == nil {
		if saved.LinkedBy == actor.UserID && saved.InstallationID == in.InstallationID && saved.IntegrationID == in.IntegrationID && saved.AccountID == in.AccountID && saved.Generation == assertion.Generation && saved.State == "active" {
			return taskControlIntent(saved), nil
		}
		return out, coded(ErrorConflict, "task_control_idempotency_conflict", "Намерение уже использовано")
	} else if !isNoRows(err) {
		return out, err
	}
	prior, e := q.ActiveTaskControlConnection(ctx, db.ActiveTaskControlConnectionParams{CompanyID: actor.CompanyID, AccountID: in.AccountID})
	if e == nil {
		if prior.InstallationID == in.InstallationID && prior.Generation == assertion.Generation {
			return out, conflict("Аккаунт уже подключён")
		}
		// Explicit new link after reinstall retires the old generation; it never reuses its session or rules.
		if _, e = q.RevokeTaskControlConnection(ctx, db.RevokeTaskControlConnectionParams{CompanyID: actor.CompanyID, ID: prior.ID, BindingRevision: prior.BindingRevision, ModeEpoch: prior.ModeEpoch}); e != nil {
			return out, e
		}
	} else if !isNoRows(e) {
		return out, e
	}
	if s.now().Unix() >= assertion.ExpiresAt {
		return out, forbidden("Подтверждение Core истекло")
	}
	if e = s.consumeTC(ctx, q, assertion, key); e != nil {
		return out, e
	}
	c, e := q.CreateTaskControlConnection(ctx, db.CreateTaskControlConnectionParams{CompanyID: actor.CompanyID, ID: uuid.New(), InstallationID: in.InstallationID, IntegrationID: in.IntegrationID, AccountID: in.AccountID, Generation: assertion.Generation, IntentID: in.IntentID, LinkedBy: actor.UserID, CrmAdminID: assertion.ActorID})
	if isUniqueViolation(e) {
		return out, conflict("Установка уже подключена")
	}
	if e != nil {
		return out, internal("Не удалось подключить установку", e)
	}
	return taskControlIntent(c), tx.Commit(ctx)
}
func (s *Service) RevokeTaskControl(ctx context.Context, actor Actor, id uuid.UUID, in TaskControlRevokeInput) (TaskControlConnection, error) {
	var out TaskControlConnection
	q := db.New(s.pool)
	if _, e := s.taskControlActor(ctx, q, actor, true); e != nil {
		return out, e
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: actor.CompanyID, ID: id})
	if e != nil {
		return out, notFound("Подключение")
	}
	if e = s.taskControlAccount(ctx, q, actor.CompanyID, c.AccountID); e != nil {
		return out, e
	}
	if in.RequestID == uuid.Nil || in.ExpectedBindingRevision <= 0 || in.ExpectedModeEpoch <= 0 {
		return out, validation("Некорректная ревизия")
	}
	material, _ := json.Marshal([]any{id, in.ExpectedBindingRevision, in.ExpectedModeEpoch})
	hash := sha256.Sum256(material)
	bodyHash := hex.EncodeToString(hash[:])
	prior, e := q.GetTaskControlConnectionRequest(ctx, db.GetTaskControlConnectionRequestParams{CompanyID: actor.CompanyID, RequestID: in.RequestID})
	if e == nil {
		if prior.ActorID != actor.UserID || prior.Action != "revoke" || prior.BodyHash != bodyHash || prior.BindingID != id {
			return out, coded(ErrorConflict, "task_control_idempotency_conflict", "Ключ операции уже использован")
		}
		e = json.Unmarshal(prior.Response, &out)
		return out, e
	}
	if !isNoRows(e) {
		return out, e
	}
	a, key, e := s.taskControlAssertion(in.CoreAssertion, "revoke")
	if e != nil {
		return out, e
	}
	if a.CompanyID != actor.CompanyID || a.AccountID != c.AccountID || a.InstallationID != c.InstallationID || a.IntegrationID != c.IntegrationID || a.RequestID != in.RequestID || a.Generation != c.Generation {
		return out, forbidden("Чужое подтверждение отключения")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	if e = q.LockTaskControlAccount(ctx, c.AccountID); e != nil {
		return out, e
	}
	if _, e = s.taskControlActor(ctx, q, actor, true); e != nil {
		return out, e
	}
	if e = s.taskControlAccount(ctx, q, actor.CompanyID, c.AccountID); e != nil {
		return out, e
	}
	if saved, err := q.GetTaskControlConnectionRequest(ctx, db.GetTaskControlConnectionRequestParams{CompanyID: actor.CompanyID, RequestID: in.RequestID}); err == nil {
		if saved.ActorID != actor.UserID || saved.BodyHash != bodyHash || saved.BindingID != id || saved.Action != "revoke" {
			return out, coded(ErrorConflict, "task_control_idempotency_conflict", "Ключ операции уже использован")
		}
		err = json.Unmarshal(saved.Response, &out)
		return out, err
	} else if !isNoRows(err) {
		return out, err
	}
	if s.now().Unix() >= a.ExpiresAt {
		return out, forbidden("Подтверждение Core истекло")
	}
	if e = s.consumeTC(ctx, q, a, key); e != nil {
		return out, e
	}
	c, e = q.RevokeTaskControlConnection(ctx, db.RevokeTaskControlConnectionParams{CompanyID: actor.CompanyID, ID: id, BindingRevision: in.ExpectedBindingRevision, ModeEpoch: in.ExpectedModeEpoch})
	if isNoRows(e) {
		return out, conflict("Ревизия подключения изменилась")
	}
	if e != nil {
		return out, e
	}
	if e = q.DeleteTaskControlRuntimeSessions(ctx, db.DeleteTaskControlRuntimeSessionsParams{CompanyID: actor.CompanyID, BindingID: id}); e != nil {
		return out, e
	}
	out = TaskControlConnection{taskControlScope(c), c.State, c.Mode, c.UpdatedAt, tc.Installation{CompanyID: actor.CompanyID, AccountID: c.AccountID, Product: tc.Product, State: "unknown", CheckedAt: s.now(), FreshUntil: s.now()}}
	response, _ := json.Marshal(out)
	if e = q.SaveTaskControlConnectionRequest(ctx, db.SaveTaskControlConnectionRequestParams{CompanyID: actor.CompanyID, RequestID: in.RequestID, BindingID: id, ActorID: actor.UserID, Action: "revoke", BodyHash: bodyHash, Response: response}); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

type TaskControlSession struct {
	AccessToken string           `json:"accessToken"`
	ExpiresAt   time.Time        `json:"expiresAt"`
	Scope       TaskControlScope `json:"scope"`
	ActorID     string           `json:"actorId"`
}

func (s *Service) ExchangeTaskControlSession(ctx context.Context, raw string) (TaskControlSession, error) {
	var out TaskControlSession
	a, key, e := s.taskControlAssertion(raw, "runtime")
	if e != nil {
		return out, e
	}
	st, e := s.taskControlStatus(ctx, a.CompanyID, a.AccountID, false)
	if e != nil {
		return out, e
	}
	if e = taskControlInstalled(st, a.InstallationID, a.IntegrationID, a.Generation, s.now()); e != nil {
		return out, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.LockTaskControlAccount(ctx, a.AccountID); e != nil {
		return out, e
	}
	if e = s.taskControlAccount(ctx, q, a.CompanyID, a.AccountID); e != nil {
		return out, e
	}
	c, e := q.ActiveTaskControlConnection(ctx, db.ActiveTaskControlConnectionParams{CompanyID: a.CompanyID, AccountID: a.AccountID})
	if e != nil {
		return out, notFound("Подключение")
	}
	if c.InstallationID != a.InstallationID || c.IntegrationID != a.IntegrationID || c.Generation != a.Generation {
		return out, forbidden("Подключение изменилось")
	}
	if e = s.currentTaskControlInstallation(ctx, q, a.CompanyID, a.AccountID, a.InstallationID, a.IntegrationID, a.Generation); e != nil {
		return out, e
	}
	if s.now().Unix() >= a.ExpiresAt {
		return out, forbidden("Подтверждение Core истекло")
	}
	if e = s.consumeTC(ctx, q, a, key); e != nil {
		return out, e
	}
	token := make([]byte, 32)
	if _, e = rand.Read(token); e != nil {
		return out, e
	}
	out = TaskControlSession{base64.RawURLEncoding.EncodeToString(token), s.now().UTC().Add(5 * time.Minute), taskControlScope(c), a.ActorID}
	digest := sha256.Sum256([]byte(out.AccessToken))
	e = q.CreateTaskControlRuntimeSession(ctx, db.CreateTaskControlRuntimeSessionParams{TokenHash: digest[:], CompanyID: c.CompanyID, BindingID: c.ID, ActorID: a.ActorID, BindingRevision: c.BindingRevision, ModeEpoch: c.ModeEpoch, Generation: c.Generation, ExpiresAt: out.ExpiresAt})
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// AuthorizeTaskControlRuntime is the mandatory gate for later config/ACK handlers.
func (s *Service) AuthorizeTaskControlRuntime(ctx context.Context, token string) (TaskControlConnection, string, error) {
	var out TaskControlConnection
	if len(token) != 43 {
		return out, "", coded(ErrorUnauthenticated, "task_control_session_invalid", "Недействительная сессия виджета")
	}
	hash := sha256.Sum256([]byte(token))
	q := db.New(s.pool)
	session, e := q.GetTaskControlRuntimeSession(ctx, db.GetTaskControlRuntimeSessionParams{TokenHash: hash[:], ExpiresAt: s.now()})
	if e != nil {
		return out, "", coded(ErrorUnauthenticated, "task_control_session_invalid", "Сессия виджета истекла или отозвана")
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: session.CompanyID, ID: session.BindingID})
	if e != nil {
		return out, "", forbidden("Подключение отсутствует")
	}
	st, e := s.taskControlStatus(ctx, c.CompanyID, c.AccountID, false)
	if e != nil {
		return out, "", e
	}
	// Re-read fences after the network call, including explicit revoke.
	c, e = q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: session.CompanyID, ID: session.BindingID})
	if e != nil {
		return out, "", forbidden("Подключение отсутствует")
	}
	if c.State != "active" || c.BindingRevision != session.BindingRevision || c.ModeEpoch != session.ModeEpoch || c.Generation != session.Generation || !s.now().Before(session.ExpiresAt) {
		return out, "", forbidden("Сессия подключения отозвана")
	}
	if e = taskControlInstalled(st, c.InstallationID, c.IntegrationID, c.Generation, s.now()); e != nil {
		return out, "", e
	}
	return TaskControlConnection{taskControlScope(c), c.State, c.Mode, c.UpdatedAt, st}, session.ActorID, nil
}
