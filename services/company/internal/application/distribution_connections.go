package application

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type DistributionCore interface {
	Revoke(context.Context, corebridge.Scope) error
	Confirm(context.Context, corebridge.Confirmation) (corebridge.Binding, error)
	GetBinding(context.Context, corebridge.Scope) (corebridge.Binding, error)
	References(context.Context, corebridge.Scope) (corebridge.References, error)
	Mappings(context.Context, corebridge.Scope, int64, []corebridge.Mapping) error
	Permission(context.Context, corebridge.Scope, corebridge.PermissionInput) (corebridge.Permission, error)
}

func WithDistributionCore(c DistributionCore) ServiceOption {
	return func(s *Service) { s.distributionCore = c }
}

type DistributionConnection struct {
	BindingID          uuid.UUID `json:"bindingId"`
	Revision           int64     `json:"revision"`
	InstallationID     uuid.UUID `json:"installationId"`
	IntegrationID      uuid.UUID `json:"integrationId"`
	AccountID          string    `json:"accountId"`
	State              string    `json:"state"`
	IntentID           uuid.UUID `json:"intentId"`
	ExpiresAt          time.Time `json:"expiresAt"`
	MappingRevision    int64     `json:"mappingRevision"`
	MappingAckRevision int64     `json:"mappingAckRevision"`
}
type DistributionLinkInput struct {
	InstallationID, IntegrationID, IntentID uuid.UUID
	AccountID, WidgetToken                  string
}
type DistributionEmployeeMapping struct {
	ID             uuid.UUID  `json:"id"`
	UserID         *uuid.UUID `json:"userId"`
	UserIDSnapshot uuid.UUID  `json:"userIdSnapshot"`
	CRMUserID      string     `json:"crmUserId"`
	State          string     `json:"state"`
	Revision       int64      `json:"revision"`
	VerifiedAt     time.Time  `json:"verifiedAt"`
}

func connectionFromDB(b db.DistributionBinding) DistributionConnection {
	return DistributionConnection{b.ID, b.Revision, b.InstallationID, b.IntegrationID, b.AccountID, b.State, b.IntentID, b.ExpiresAt, b.MappingRevision, b.MappingAckRevision}
}
func bindingScope(b db.DistributionBinding) corebridge.Scope {
	return corebridge.Scope{CompanyID: b.CompanyID, BindingID: b.ID, BindingRevision: b.Revision, InstallationID: b.InstallationID, IntegrationID: b.IntegrationID, AccountID: b.AccountID}
}
func mappingFromDB(m db.DistributionEmployeeMapping) DistributionEmployeeMapping {
	return DistributionEmployeeMapping{m.ID, uuidPointer(m.UserID), m.UserIDSnapshot, m.CrmUserID, m.State, m.Revision, m.VerifiedAt}
}
func validCRMID(id string) bool {
	n, e := strconv.ParseInt(id, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == id
}
func coreError(e error) error {
	var ce *corebridge.Error
	if errors.As(e, &ce) {
		switch ce.Status {
		case 401, 403:
			return forbidden("Недостаточно прав amoCRM или межсервисного доступа")
		case 409:
			return conflict("Связь amoCRM или версия изменилась")
		case 404:
			return notFound("Подключение amoCRM")
		}
	}
	return upstream("Сервис amoCRM временно недоступен", nil)
}

// Re-read current membership/sections: stale JWT claims never grant access.
func (s *Service) distributionActor(ctx context.Context, a Actor, admin bool) (Actor, error) {
	company, e := db.New(s.pool).GetCompany(ctx, a.CompanyID)
	if e != nil || company.Status != "active" {
		return Actor{}, forbidden("Компания недоступна для распределения")
	}
	u, e := db.New(s.pool).GetUserWithPositions(ctx, db.GetUserWithPositionsParams{CompanyID: a.CompanyID, ID: a.UserID})
	if e != nil {
		if isNoRows(e) {
			return Actor{}, forbidden("Нет доступа к компании")
		}
		return Actor{}, internal("Не удалось проверить пользователя", e)
	}
	if u.Status != "active" {
		return Actor{}, forbidden("Сотрудник неактивен")
	}
	a.Role = u.Role
	a.SectionAccess = u.SectionAccess
	if admin {
		if e = requireAdministrator(a); e != nil {
			return Actor{}, e
		}
	} else if !actorHasSection(a, "distribution") {
		return Actor{}, forbidden("Раздел «Распределение» недоступен")
	}
	return a, nil
}
func (s *Service) ListDistributionConnections(ctx context.Context, a Actor) ([]DistributionConnection, error) {
	if _, e := s.distributionActor(ctx, a, false); e != nil {
		return nil, e
	}
	rows, e := db.New(s.pool).ListDistributionBindings(ctx, a.CompanyID)
	if e != nil {
		return nil, internal("Не удалось получить подключения", e)
	}
	out := make([]DistributionConnection, len(rows))
	for i, b := range rows {
		out[i] = connectionFromDB(b)
	}
	return out, nil
}
func (s *Service) LinkDistributionConnection(ctx context.Context, a Actor, in DistributionLinkInput) (DistributionConnection, error) {
	a, e := s.distributionActor(ctx, a, true)
	if e != nil {
		return DistributionConnection{}, e
	}
	if s.distributionCore == nil {
		return DistributionConnection{}, upstream("Core не настроен", nil)
	}
	if in.InstallationID == uuid.Nil || in.IntegrationID == uuid.Nil || in.IntentID == uuid.Nil || !validCRMID(in.AccountID) || len(in.WidgetToken) > 8192 {
		return DistributionConnection{}, validation("Некорректное подключение amoCRM")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return DistributionConnection{}, internal("Не удалось начать подключение", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, e = q.LockDistributionCompany(ctx, a.CompanyID); e != nil {
		return DistributionConnection{}, internal("Не удалось заблокировать компанию", e)
	}
	b, e := q.CurrentDistributionBinding(ctx, a.CompanyID)
	if isNoRows(e) {
		b, e = q.CreateDistributionBinding(ctx, db.CreateDistributionBindingParams{ID: uuid.New(), CompanyID: a.CompanyID, InstallationID: in.InstallationID, IntegrationID: in.IntegrationID, AccountID: in.AccountID, IntentID: in.IntentID, InitiatedBy: uuid.NullUUID{UUID: a.UserID, Valid: true}, ExpiresAt: s.now().Add(15 * time.Minute)})
	}
	if e != nil {
		if isUniqueViolation(e) {
			return DistributionConnection{}, conflict("Установка уже связана с компанией")
		}
		return DistributionConnection{}, internal("Не удалось сохранить подключение", e)
	}
	if b.InstallationID != in.InstallationID || b.IntegrationID != in.IntegrationID || b.AccountID != in.AccountID || b.IntentID != in.IntentID {
		return DistributionConnection{}, conflict("Компания уже имеет другое подключение")
	}
	if e = tx.Commit(ctx); e != nil {
		return DistributionConnection{}, internal("Не удалось сохранить намерение", e)
	}
	scope := bindingScope(b)
	remote, e := s.distributionCore.GetBinding(ctx, scope)
	// Read accepted outcome first: consuming a widget token must not be retried blindly.
	if e != nil {
		var ce *corebridge.Error
		if !errors.As(e, &ce) || ce.Status != 404 {
			return connectionFromDB(b), coreError(e)
		}
		if !s.now().Before(b.ExpiresAt) || in.WidgetToken == "" {
			return connectionFromDB(b), conflict("Намерение истекло; требуется сверка и новое подтверждение")
		}
		remote, e = s.distributionCore.Confirm(ctx, corebridge.Confirmation{Scope: scope, IntentID: b.IntentID, WidgetToken: in.WidgetToken, ExpiresAt: b.ExpiresAt})
		if e != nil {
			return connectionFromDB(b), coreError(e)
		}
	}
	if remote.Scope != scope || remote.State != "active" {
		return connectionFromDB(b), conflict("Core не подтвердил точную связь")
	}
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		return DistributionConnection{}, internal("Не удалось подтвердить связь", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	if _, e = q.LockDistributionCompany(ctx, a.CompanyID); e != nil {
		return DistributionConnection{}, internal("Не удалось заблокировать компанию", e)
	}
	b, e = q.ActivateDistributionBinding(ctx, db.ActivateDistributionBindingParams{CompanyID: a.CompanyID, ID: b.ID, Revision: b.Revision})
	if e != nil {
		return DistributionConnection{}, conflict("Связь была изменена")
	}
	if e = q.RecordDistributionBindingVersion(ctx, db.RecordDistributionBindingVersionParams{CompanyID: a.CompanyID, ID: b.ID}); e != nil {
		return DistributionConnection{}, internal("Не удалось сохранить версию", e)
	}
	if e = tx.Commit(ctx); e != nil {
		return DistributionConnection{}, internal("Не удалось подтвердить связь", e)
	}
	return connectionFromDB(b), nil
}
func (s *Service) activeDistributionBinding(ctx context.Context, a Actor, id uuid.UUID) (db.DistributionBinding, error) {
	b, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: a.CompanyID, ID: id})
	if e != nil {
		if isNoRows(e) {
			return b, notFound("Подключение")
		}
		return b, internal("Не удалось получить подключение", e)
	}
	if b.State != "active" {
		return b, conflict("Подключение не активно")
	}
	if s.distributionCore == nil {
		return b, upstream("Core не настроен", nil)
	}
	r, e := s.distributionCore.GetBinding(ctx, bindingScope(b))
	if e != nil {
		return b, coreError(e)
	}
	if r.Scope != bindingScope(b) || r.State != "active" {
		return b, conflict("Подключение изменено в Core")
	}
	return b, nil
}
func validateDistributionReferences(r corebridge.References, now time.Time) error {
	if r.Users == nil || r.Pipelines == nil || r.State != "fresh" || r.FetchedAt.IsZero() || r.FetchedAt.After(now.Add(time.Minute)) || !r.FreshUntil.After(now) || r.FreshUntil.Before(r.FetchedAt) {
		return errors.New("stale")
	}
	seen := map[string]bool{}
	for _, u := range r.Users {
		if !validCRMID(u.ID) || seen[u.ID] {
			return errors.New("users")
		}
		seen[u.ID] = true
	}
	seen = map[string]bool{}
	for _, p := range r.Pipelines {
		if !validCRMID(p.ID) || seen[p.ID] {
			return errors.New("pipelines")
		}
		seen[p.ID] = true
		ss := map[string]bool{}
		for _, st := range p.Statuses {
			if !validCRMID(st.ID) || ss[st.ID] {
				return errors.New("statuses")
			}
			ss[st.ID] = true
		}
	}
	return nil
}
func (s *Service) DistributionReferences(ctx context.Context, a Actor, id uuid.UUID) (corebridge.References, error) {
	if _, e := s.distributionActor(ctx, a, false); e != nil {
		return corebridge.References{}, e
	}
	b, e := s.activeDistributionBinding(ctx, a, id)
	if e != nil {
		return corebridge.References{}, e
	}
	r, e := s.distributionCore.References(ctx, bindingScope(b))
	if e != nil {
		return r, coreError(e)
	}
	if e = validateDistributionReferences(r, s.now()); e != nil {
		return corebridge.References{}, upstream("Справочники amoCRM неполны или устарели", nil)
	}
	if e = s.cacheDistributionTimezone(ctx, bindingScope(b), r); e != nil {
		return corebridge.References{}, e
	}
	return r, nil
}
func (s *Service) ListDistributionEmployeeMappings(ctx context.Context, a Actor, id uuid.UUID) ([]DistributionEmployeeMapping, error) {
	if _, e := s.distributionActor(ctx, a, false); e != nil {
		return nil, e
	}
	if _, e := s.activeDistributionBinding(ctx, a, id); e != nil {
		return nil, e
	}
	rows, e := db.New(s.pool).ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return nil, internal("Не удалось получить сопоставления", e)
	}
	out := make([]DistributionEmployeeMapping, len(rows))
	for i, m := range rows {
		out[i] = mappingFromDB(m)
	}
	return out, nil
}
func (s *Service) SetDistributionEmployeeMapping(ctx context.Context, a Actor, id, userID uuid.UUID, crmID string) (DistributionEmployeeMapping, error) {
	if _, e := s.distributionActor(ctx, a, true); e != nil {
		return DistributionEmployeeMapping{}, e
	}
	if !validCRMID(crmID) {
		return DistributionEmployeeMapping{}, validation("Некорректный ID пользователя amoCRM")
	}
	refs, e := s.DistributionReferences(ctx, a, id)
	if e != nil {
		return DistributionEmployeeMapping{}, e
	}
	found, active := false, false
	for _, u := range refs.Users {
		if u.ID == crmID {
			found = true
			active = u.IsActive
		}
	}
	if !found {
		return DistributionEmployeeMapping{}, validation("Пользователь отсутствует в полном справочнике amoCRM")
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return DistributionEmployeeMapping{}, internal("Не удалось начать сопоставление", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, e = q.LockDistributionCompany(ctx, a.CompanyID); e != nil {
		return DistributionEmployeeMapping{}, internal("Не удалось заблокировать компанию", e)
	}
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: a.CompanyID, ID: id})
	if e != nil || b.State != "active" {
		return DistributionEmployeeMapping{}, conflict("Подключение изменено")
	}
	u, e := q.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{CompanyID: a.CompanyID, ID: userID})
	if e != nil {
		if isNoRows(e) {
			return DistributionEmployeeMapping{}, notFound("Сотрудник")
		}
		return DistributionEmployeeMapping{}, internal("Не удалось проверить сотрудника", e)
	}
	state := "unavailable"
	if active && u.Status == "active" {
		state = "verified"
	}
	m, e := q.UpsertDistributionMapping(ctx, db.UpsertDistributionMappingParams{ID: uuid.New(), CompanyID: a.CompanyID, BindingID: id, UserID: uuid.NullUUID{UUID: userID, Valid: true}, CrmUserID: crmID, State: state, VerifiedAt: refs.FetchedAt})
	if e != nil {
		if isUniqueViolation(e) {
			return DistributionEmployeeMapping{}, conflict("Пользователь amoCRM уже сопоставлен с другим сотрудником")
		}
		return DistributionEmployeeMapping{}, internal("Не удалось сохранить сопоставление", e)
	}
	all, e := q.ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return DistributionEmployeeMapping{}, internal("Не удалось получить сопоставления", e)
	}
	payload := []corebridge.Mapping{}
	for _, item := range all {
		if item.State == "verified" && item.UserID.Valid {
			payload = append(payload, corebridge.Mapping{EmployeeID: item.UserID.UUID, UserID: item.CrmUserID})
		}
	}

	revision, e := q.NextDistributionMappingRevision(ctx, db.NextDistributionMappingRevisionParams{CompanyID: a.CompanyID, ID: id})
	if e != nil {
		return DistributionEmployeeMapping{}, internal("Не удалось сохранить версию сопоставлений", e)
	}
	raw, e := json.Marshal(payload)
	if e != nil {
		return DistributionEmployeeMapping{}, internal("Не удалось сформировать снимок", e)
	}
	if e = q.StoreDistributionMappingSnapshot(ctx, db.StoreDistributionMappingSnapshotParams{CompanyID: a.CompanyID, BindingID: id, Revision: revision, Payload: raw}); e != nil {
		return DistributionEmployeeMapping{}, internal("Не удалось сохранить снимок", e)
	}
	if e = tx.Commit(ctx); e != nil {
		return DistributionEmployeeMapping{}, internal("Не удалось сохранить сопоставление", e)
	}
	// A committed snapshot is replayable after any transport error. While pending,
	// both TeamOS and widget access fail closed instead of using a stale mirror.
	if e = s.SyncDistributionMappings(ctx, a, id); e != nil {
		return mappingFromDB(m), e
	}

	return mappingFromDB(m), nil
}
func (s *Service) DistributionLeadPermission(ctx context.Context, a Actor, id uuid.UUID, leadID string) (corebridge.Permission, error) {
	if _, e := s.distributionActor(ctx, a, false); e != nil {
		return corebridge.Permission{}, e
	}
	if !validCRMID(leadID) {
		return corebridge.Permission{}, validation("Некорректный ID сделки")
	}
	b, e := s.activeDistributionBinding(ctx, a, id)
	if e != nil {
		return corebridge.Permission{}, e
	}
	if b.MappingRevision != b.MappingAckRevision {
		return corebridge.Permission{}, conflict("Сопоставления ожидают синхронизации с Core")
	}
	rows, e := db.New(s.pool).ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return corebridge.Permission{}, internal("Не удалось получить сопоставление", e)
	}
	var crm string
	for _, m := range rows {
		if m.UserID.Valid && m.UserID.UUID == a.UserID && m.State == "verified" {
			crm = m.CrmUserID
		}
	}
	if crm == "" {
		return corebridge.Permission{}, forbidden("Сотрудник не сопоставлен с amoCRM")
	}
	r, e := s.distributionCore.Permission(ctx, bindingScope(b), corebridge.PermissionInput{EmployeeID: a.UserID, UserID: crm, LeadID: leadID})
	if e != nil {
		return r, coreError(e)
	}
	if r.UserID != crm || r.CheckedAt.IsZero() || s.now().Sub(r.CheckedAt) > time.Minute || r.CheckedAt.After(s.now().Add(time.Minute)) {
		return corebridge.Permission{}, forbidden("Не удалось подтвердить права на сделку")
	}

	if _, e = s.distributionActor(ctx, a, false); e != nil {
		return corebridge.Permission{}, e
	}
	current, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: a.CompanyID, ID: id})
	if e != nil || current.State != "active" || bindingScope(current) != bindingScope(b) || current.MappingRevision != b.MappingRevision || current.MappingAckRevision != current.MappingRevision {
		return corebridge.Permission{}, forbidden("Права или сопоставления изменились")
	}
	currentMapping, e := db.New(s.pool).GetDistributionMappingByCRM(ctx, db.GetDistributionMappingByCRMParams{CompanyID: a.CompanyID, BindingID: id, CrmUserID: crm})
	if e != nil || currentMapping.State != "verified" || !currentMapping.UserID.Valid || currentMapping.UserID.UUID != a.UserID || currentMapping.UserStatus != "active" {
		return corebridge.Permission{}, forbidden("Сопоставление изменилось")
	}
	return r, nil
}

// RevokeDistributionConnection fences Core first, including pending-but-not-yet-visible confirmations.
// Lost HTTP responses leave a retryable local reservation, never an active grant.
func (s *Service) RevokeDistributionConnection(ctx context.Context, a Actor, id uuid.UUID) error {
	if _, e := s.distributionActor(ctx, a, true); e != nil {
		return e
	}
	b, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: a.CompanyID, ID: id})
	if e != nil {
		if isNoRows(e) {
			return notFound("Подключение")
		}
		return internal("Не удалось получить подключение", e)
	}
	if b.State == "revoked" {
		return nil
	}
	if s.distributionCore == nil {
		return upstream("Core не настроен", nil)
	}
	if e = s.distributionCore.Revoke(ctx, bindingScope(b)); e != nil {
		return coreError(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return internal("Не удалось отозвать связь", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, e = q.LockDistributionCompany(ctx, a.CompanyID); e != nil {
		return internal("Не удалось заблокировать компанию", e)
	}
	_, e = q.RevokeDistributionBinding(ctx, db.RevokeDistributionBindingParams{CompanyID: a.CompanyID, ID: id})
	if e != nil && !isNoRows(e) {
		return internal("Не удалось отозвать связь", e)
	}
	if e = q.RecordDistributionBindingVersion(ctx, db.RecordDistributionBindingVersionParams{CompanyID: a.CompanyID, ID: id}); e != nil {
		return internal("Не удалось сохранить версию", e)
	}
	if e = tx.Commit(ctx); e != nil {
		return internal("Не удалось отозвать связь", e)
	}
	return nil
}

type DistributionWidgetAccessInput struct {
	corebridge.Scope
	UserID string `json:"userId"`
	LeadID string `json:"leadId"`
}
type DistributionWidgetAccess struct {
	Allowed    bool      `json:"allowed"`
	EmployeeID uuid.UUID `json:"employeeId"`
	Reason     string    `json:"reason"`
	CanManage  bool      `json:"canManage"`
}

// DistributionWidgetAccess accepts a verified CRM principal only from the authenticated Core transport.
func (s *Service) DistributionWidgetAccess(ctx context.Context, in DistributionWidgetAccessInput) (DistributionWidgetAccess, error) {
	deny := DistributionWidgetAccess{Reason: "mapping_or_section_unavailable"}
	company, e := db.New(s.pool).GetCompany(ctx, in.CompanyID)
	if e != nil || company.Status != "active" {
		return deny, nil
	}
	if !validCRMID(in.UserID) || (in.LeadID != "" && !validCRMID(in.LeadID)) {
		return deny, validation("Некорректный пользователь или сделка")
	}
	b, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: in.CompanyID, ID: in.BindingID})
	if e != nil {
		if isNoRows(e) {
			return deny, nil
		}
		return deny, internal("Не удалось проверить связь", e)
	}
	if b.State != "active" || b.MappingRevision != b.MappingAckRevision || bindingScope(b) != in.Scope {
		return deny, nil
	}
	m, e := db.New(s.pool).GetDistributionMappingByCRM(ctx, db.GetDistributionMappingByCRMParams{CompanyID: in.CompanyID, BindingID: in.BindingID, CrmUserID: in.UserID})
	if e != nil {
		if isNoRows(e) {
			return deny, nil
		}
		return deny, internal("Не удалось проверить сопоставление", e)
	}
	if !m.UserID.Valid || m.State != "verified" || m.UserStatus != "active" {
		return deny, nil
	}
	actor := Actor{CompanyID: in.CompanyID, UserID: m.UserID.UUID, Role: m.UserRole, SectionAccess: m.SectionAccess}
	if !actorHasSection(actor, "distribution") {
		return deny, nil
	}
	current, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: in.CompanyID, ID: in.BindingID})
	if e != nil || current.State != "active" || current.MappingRevision != current.MappingAckRevision || bindingScope(current) != in.Scope {
		return deny, nil
	}
	return DistributionWidgetAccess{Allowed: true, EmployeeID: m.UserID.UUID, Reason: "allowed", CanManage: actor.Role == "owner" || actor.Role == "admin"}, nil
}

// SyncDistributionMappings retries the latest durable snapshot without changing
// its revision or payload. A concurrent newer snapshot makes a late ack a no-op.
func (s *Service) SyncDistributionMappings(ctx context.Context, a Actor, id uuid.UUID) error {
	if _, e := s.distributionActor(ctx, a, true); e != nil {
		return e
	}
	b, e := s.activeDistributionBinding(ctx, a, id)
	if e != nil {
		return e
	}
	if b.MappingRevision == b.MappingAckRevision {
		return nil
	}
	raw, e := db.New(s.pool).GetDistributionMappingSnapshot(ctx, db.GetDistributionMappingSnapshotParams{CompanyID: a.CompanyID, BindingID: id, Revision: b.MappingRevision})
	if e != nil {
		return internal("Не удалось получить снимок сопоставлений", e)
	}
	var payload []corebridge.Mapping
	if json.Unmarshal(raw, &payload) != nil {
		return internal("Некорректный снимок сопоставлений", nil)
	}
	if e = s.distributionCore.Mappings(ctx, bindingScope(b), b.MappingRevision, payload); e != nil {
		return coreError(e)
	}
	_, e = db.New(s.pool).AckDistributionMappingSnapshot(ctx, db.AckDistributionMappingSnapshotParams{CompanyID: a.CompanyID, ID: id, MappingAckRevision: b.MappingRevision})
	if e != nil {
		return internal("Не удалось подтвердить синхронизацию", e)
	}
	return nil
}

// ReconcileDistributionEmployeeMappings imports only already-owned CRM IDs.
// Name/email are never identity evidence. It neither creates users nor replaces
// employee IDs, schedules, group membership, roles or the legacy integration.
func (s *Service) ReconcileDistributionEmployeeMappings(ctx context.Context, a Actor, id uuid.UUID) error {
	if _, e := s.distributionActor(ctx, a, true); e != nil {
		return e
	}
	refs, e := s.DistributionReferences(ctx, a, id)
	if e != nil {
		return e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return internal("Не удалось начать сверку сотрудников", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, e = q.LockDistributionCompany(ctx, a.CompanyID); e != nil {
		return internal("Не удалось заблокировать компанию", e)
	}
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: a.CompanyID, ID: id})
	if e != nil || b.State != "active" {
		return conflict("Подключение изменено")
	}
	candidates, e := q.ListDistributionIdentityCandidates(ctx, db.ListDistributionIdentityCandidatesParams{CompanyID: a.CompanyID, AccountID: pgText(&b.AccountID)})
	if e != nil {
		return internal("Не удалось получить существующие CRM ID", e)
	}
	crmUsers := map[string]corebridge.User{}
	for _, u := range refs.Users {
		crmUsers[u.ID] = u
	}
	owners := map[string]map[uuid.UUID]bool{}
	userCRM := map[uuid.UUID]map[string]bool{}
	for _, c := range candidates {
		if !c.CrmUserID.Valid || !validCRMID(c.CrmUserID.String) {
			continue
		}
		if owners[c.CrmUserID.String] == nil {
			owners[c.CrmUserID.String] = map[uuid.UUID]bool{}
		}
		owners[c.CrmUserID.String][c.UserID] = true
		if userCRM[c.UserID] == nil {
			userCRM[c.UserID] = map[string]bool{}
		}
		userCRM[c.UserID][c.CrmUserID.String] = true
	}
	existing, e := q.ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return internal("Не удалось получить сопоставления", e)
	}
	byUser := map[uuid.UUID]db.DistributionEmployeeMapping{}
	byCRM := map[string]db.DistributionEmployeeMapping{}
	for _, m := range existing {
		byCRM[m.CrmUserID] = m
		if m.UserID.Valid {
			byUser[m.UserID.UUID] = m
		}
		state := m.State
		if _, ok := crmUsers[m.CrmUserID]; !ok || !crmUsers[m.CrmUserID].IsActive || !m.UserID.Valid {
			state = "unavailable"
		} else if len(owners[m.CrmUserID]) > 1 {
			state = "ambiguous"
		}
		if state != m.State {
			if e = q.SetDistributionMappingState(ctx, db.SetDistributionMappingStateParams{CompanyID: a.CompanyID, BindingID: id, ID: m.ID, State: state, VerifiedAt: refs.FetchedAt}); e != nil {
				return internal("Не удалось обновить состояние сопоставления", e)
			}
		}
	}
	for _, c := range candidates {
		if !c.CrmUserID.Valid || !validCRMID(c.CrmUserID.String) || len(owners[c.CrmUserID.String]) != 1 || len(userCRM[c.UserID]) != 1 {
			continue
		}
		if _, ok := byUser[c.UserID]; ok {
			continue
		}
		if _, ok := byCRM[c.CrmUserID.String]; ok {
			continue
		}
		state := "unavailable"
		if crm, ok := crmUsers[c.CrmUserID.String]; ok && crm.IsActive && c.Status == "active" {
			state = "verified"
		}
		_, e = q.UpsertDistributionMapping(ctx, db.UpsertDistributionMappingParams{ID: uuid.New(), CompanyID: a.CompanyID, BindingID: id, UserID: uuid.NullUUID{UUID: c.UserID, Valid: true}, CrmUserID: c.CrmUserID.String, State: state, VerifiedAt: refs.FetchedAt})
		if e != nil {
			return internal("Не удалось переиспользовать CRM ID сотрудника", e)
		}
	}
	all, e := q.ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return internal("Не удалось получить снимок", e)
	}
	payload := []corebridge.Mapping{}
	for _, m := range all {
		if m.State == "verified" && m.UserID.Valid {
			payload = append(payload, corebridge.Mapping{EmployeeID: m.UserID.UUID, UserID: m.CrmUserID})
		}
	}
	revision, e := q.NextDistributionMappingRevision(ctx, db.NextDistributionMappingRevisionParams{CompanyID: a.CompanyID, ID: id})
	if e != nil {
		return internal("Не удалось сохранить версию сопоставлений", e)
	}
	raw, _ := json.Marshal(payload)
	if e = q.StoreDistributionMappingSnapshot(ctx, db.StoreDistributionMappingSnapshotParams{CompanyID: a.CompanyID, BindingID: id, Revision: revision, Payload: raw}); e != nil {
		return internal("Не удалось сохранить снимок", e)
	}
	if e = tx.Commit(ctx); e != nil {
		return internal("Не удалось завершить сверку", e)
	}
	return s.SyncDistributionMappings(ctx, a, id)
}

func (s *Service) CleanupDistributionServiceNonces(ctx context.Context) error {
	return db.New(s.pool).CleanupDistributionNonces(ctx)
}
