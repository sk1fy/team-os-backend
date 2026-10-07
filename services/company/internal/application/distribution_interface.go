package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type DistributionQueueFilter struct {
	Tab      string
	GroupID  uuid.UUID
	From, To *time.Time
}

func (f DistributionQueueFilter) valid() bool {
	switch f.Tab {
	case "", "waiting", "assigning", "completed", "errors", "cancelled":
	default:
		return false
	}
	return f.From == nil || f.To == nil || f.From.Before(*f.To)
}
func filterParams(actor Actor, f DistributionQueueFilter, limit, offset int32) db.ListDistributionQueueFilteredParams {
	p := db.ListDistributionQueueFilteredParams{CompanyID: actor.CompanyID, Tab: f.Tab, PageLimit: limit, PageOffset: offset}
	if f.GroupID != uuid.Nil {
		p.GroupID = nullID(f.GroupID)
	}
	if f.From != nil {
		p.FromTime = pgtype.Timestamptz{Time: *f.From, Valid: true}
	}
	if f.To != nil {
		p.ToTime = pgtype.Timestamptz{Time: *f.To, Valid: true}
	}
	return p
}
func (s *Service) distributionQueuePermission(ctx context.Context, actor Actor, row db.DistributionQueue) (bool, error) {
	if s.distributionCore == nil {
		return false, upstream("Core не настроен", nil)
	}
	q := db.New(s.pool)
	r, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: row.RuleID})
	if e != nil {
		return false, e
	}
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: r.BindingID})
	if e != nil {
		return false, e
	}
	if b.Revision != r.BindingRevision || b.AccountID != row.AccountID {
		return false, forbidden("Связь сделки недоступна")
	}
	p, e := s.DistributionLeadPermission(ctx, actor, r.BindingID, row.LeadID)
	return p.CanViewLead, e
}

type distributionPermissionResult struct {
	visible bool
	err     error
}

// Bound CRM fan-out while sharing one fresh check between episodes of the same
// rule/account/lead in this request. Never reuse permissions across requests.
func distributionQueuePermissions(ctx context.Context, rows []db.DistributionQueue, check func(context.Context, db.DistributionQueue) (bool, error)) []distributionPermissionResult {
	type key struct {
		rule          uuid.UUID
		account, lead string
	}
	indexes := make([]int, len(rows))
	unique := make([]db.DistributionQueue, 0, len(rows))
	seen := make(map[key]int)
	for i, row := range rows {
		k := key{row.RuleID, row.AccountID, row.LeadID}
		index, ok := seen[k]
		if !ok {
			index = len(unique)
			seen[k] = index
			unique = append(unique, row)
		}
		indexes[i] = index
	}
	results := make([]distributionPermissionResult, len(unique))
	jobs := make(chan int, len(unique))
	for i := range unique {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for n := 0; n < 4 && n < len(unique); n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					results[i].err = err
					continue
				}
				visible, err := check(ctx, unique[i])
				results[i] = distributionPermissionResult{visible: visible && err == nil, err: err}
			}
		}()
	}
	workers.Wait()
	out := make([]distributionPermissionResult, len(rows))
	for i, index := range indexes {
		out[i] = results[index]
	}
	return out
}

func (s *Service) distributionPagePermissions(ctx context.Context, actor Actor, rows []db.DistributionQueue) []distributionPermissionResult {
	return distributionQueuePermissions(ctx, rows, func(ctx context.Context, row db.DistributionQueue) (bool, error) {
		return s.distributionQueuePermission(ctx, actor, row)
	})
}
func (s *Service) queueActions(ctx context.Context, q *db.Queries, row db.DistributionQueue) []string {
	out := []string{}
	if row.Settled {
		if row.State == "failed" && s.distributionRetrySafe(ctx, q, row) {
			out = append(out, "retry")
		}
		return out
	}
	if row.CancelRequested {
		if row.OperationID.Valid {
			out = append(out, "check")
		}
		return out
	}
	if row.OperationID.Valid {
		return []string{"check", "cancel"}
	}
	return []string{"recalculate", "cancel"}
}
func (s *Service) distributionRetrySafe(ctx context.Context, q *db.Queries, row db.DistributionQueue) bool {
	if !row.WaitingDeadlineAt.After(s.now()) || !row.Settled || row.State != "failed" {
		return false
	}
	if !row.OperationID.Valid {
		return true
	}
	m, e := q.GetDistributionOperationMirror(ctx, row.OperationID.UUID)
	if e != nil {
		return false
	}
	var op corebridge.Operation
	var command corebridge.Assignment
	return json.Unmarshal(m.ResultPayload, &op) == nil && json.Unmarshal(row.Command, &command) == nil && validOperation(op) && operationMatchesAssignment(op, command) && op.ResolutionEvidence.GuardReleasable && op.ExternalEffectState == "no_attempt"
}
func (s *Service) distributionQueueDTO(ctx context.Context, actor Actor, row db.DistributionQueue, visible bool) map[string]any {
	var op, employee *uuid.UUID
	var planned *time.Time
	var lead *string
	var version *int64
	if row.OperationID.Valid {
		v := row.OperationID.UUID
		op = &v
		if m, e := db.New(s.pool).GetDistributionOperationMirror(ctx, v); e == nil && visible {
			v := m.ResultVersion
			version = &v
		}
	}
	if row.PlannedEmployeeID.Valid {
		v := row.PlannedEmployeeID.UUID
		employee = &v
	}
	if row.PlannedAt.Valid {
		v := row.PlannedAt.Time
		planned = &v
	}
	if visible {
		v := row.LeadID
		lead = &v
	}
	actions := []string{}
	if visible {
		if current, e := s.distributionActor(ctx, actor, false); e == nil && (current.Role == "owner" || current.Role == "admin") {
			actions = s.queueActions(ctx, db.New(s.pool), row)
		}
	}
	return map[string]any{"id": row.ID, "entryId": row.EntryID, "ruleId": row.RuleID, "groupId": row.GroupID, "accountId": row.AccountID, "leadId": lead, "state": row.State, "reason": queueVisibleReason(row, s.now()), "nextAttemptAt": row.NextAttemptAt, "operationId": op, "plannedEmployeeId": employee, "plannedAt": planned, "createdAt": row.CreatedAt, "waitingDeadlineAt": row.WaitingDeadlineAt, "nextShiftAt": nullableTimestamp(row.NextShiftAt), "updatedAt": row.UpdatedAt, "actions": actions, "resultVersion": version, "leadName": nil, "leadUrl": s.distributionQueueLeadURL(ctx, actor, row, visible), "currentEmployeeId": confirmedQueueEmployee(row, visible, employee), "previousEmployeeId": nil}
}
func (s *Service) DistributionQueuePage(ctx context.Context, actor Actor, f DistributionQueueFilter, limit, offset int32) (json.RawMessage, error) {
	currentActor, e := s.distributionActor(ctx, actor, false)
	if e != nil {
		return nil, e
	}
	actor = currentActor
	if !f.valid() || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, validation("Некорректные фильтры очереди")
	}
	if limit > 15 {
		limit = 15
	}
	rows, e := db.New(s.pool).ListDistributionQueueFiltered(ctx, filterParams(actor, f, limit+1, offset))
	if e != nil {
		return nil, e
	}
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	permissions, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	items := make([]any, 0, len(rows))
	checked := s.distributionPagePermissions(permissions, actor, rows)
	for i, row := range rows {
		items = append(items, s.distributionQueueDTO(ctx, actor, row, checked[i].visible))
	}
	return json.Marshal(map[string]any{"items": items, "limit": limit, "offset": offset, "hasMore": more, "checkedAt": s.now()})
}
func (s *Service) DistributionQueueDetail(ctx context.Context, actor Actor, id uuid.UUID) (json.RawMessage, error) {
	currentActor, e := s.distributionActor(ctx, actor, false)
	if e != nil {
		return nil, e
	}
	actor = currentActor
	row, e := db.New(s.pool).LockDistributionQueue(ctx, id)
	if isNoRows(e) || e == nil && row.CompanyID != actor.CompanyID {
		return nil, notFound("Сделка очереди")
	}
	if e != nil {
		return nil, e
	}
	permission, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	visible, e := s.distributionQueuePermission(permission, actor, row)
	if e != nil {
		return nil, e
	}
	if !visible {
		return nil, forbidden("Нет доступа к сделке amoCRM")
	}
	out := s.distributionQueueDTO(ctx, actor, row, true)
	r, e := db.New(s.pool).GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: row.RuleID})
	if e == nil {
		b, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: r.BindingID})
		if e == nil {
			mappings, e := db.New(s.pool).ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: actor.CompanyID, BindingID: b.ID})
			if e == nil {
				employee := func(crm string) *uuid.UUID {
					for _, m := range mappings {
						if m.State == "verified" && m.UserID.Valid && m.CrmUserID == crm {
							id := m.UserID.UUID
							return &id
						}
					}
					return nil
				}
				var frozen corebridge.Assignment
				if json.Unmarshal(row.Command, &frozen) == nil {
					out["previousEmployeeId"] = employee(frozen.Command.ExpectedSnapshot.ResponsibleUserID)
				}
				if core, ok := s.deliveryCore.(DistributionAssignmentCore); ok {
					observed, re := core.ReadLead(permission, bindingScope(b), row.LeadID)
					if re == nil && observed.Scope == bindingScope(b) && observed.LeadID == row.LeadID && observed.Snapshot != nil && s.now().Sub(observed.ObservedAt) <= time.Minute && !observed.ObservedAt.After(s.now().Add(time.Minute)) {
						// Core constructs the URL from its validated installation domain. Verify
						// it again at this boundary and disclose only after a fresh permission.
						allowed, pe := s.distributionQueuePermission(permission, actor, row)
						if pe == nil && allowed {
							out["leadName"] = observed.LeadName
							out["currentEmployeeId"] = employee(observed.Snapshot.ResponsibleUserID)
							out["leadUrl"] = safeDistributionLeadURL(observed.LeadURL, row.LeadID)
						}
					}
				}
			}
		}
	}
	return json.Marshal(out)
}

// DistributionSummary is exact or explicitly unknown. Never count hidden resources and never
// turn an unavailable CRM permission source into an apparently authoritative zero.
func (s *Service) DistributionSummary(ctx context.Context, actor Actor, group uuid.UUID) (json.RawMessage, error) {
	currentActor, e := s.distributionActor(ctx, actor, false)
	if e != nil {
		return nil, e
	}
	actor = currentActor
	out := map[string]any{"timezone": nil, "checkedAt": s.now(), "metricsAvailable": false, "metricsReason": "configuration_or_permissions_unavailable", "waiting": nil, "assigning": nil, "errors": nil, "confirmedToday": nil, "keptToday": nil}
	q := db.New(s.pool)
	setting, e := q.GetDistributionSettings(ctx, actor.CompanyID)
	bindings, be := q.ListDistributionBindings(ctx, actor.CompanyID)
	if be != nil {
		return nil, be
	}
	confirmedTimezone := false
	for _, b := range bindings {
		if b.State == "active" && b.AccountTimezone.Valid && b.TimezoneFetchedAt.Valid && validAccountTimezone(b.AccountTimezone.String) {
			setting.Timezone = b.AccountTimezone.String
			confirmedTimezone = true
			break
		}
	}
	if !confirmedTimezone {
		return json.Marshal(out)
	}
	if isNoRows(e) {
		return json.Marshal(out)
	}
	if e != nil {
		return nil, e
	}
	out["timezone"] = setting.Timezone
	loc, e := time.LoadLocation(setting.Timezone)
	if e != nil {
		return json.Marshal(out)
	}
	// Bounded whole population; any incomplete scan remains unknown.
	now := s.now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	rows, e := q.ListDistributionSummaryCandidates(ctx, db.ListDistributionSummaryCandidatesParams{CompanyID: actor.CompanyID, GroupID: nullableGroup(group), DayStart: start, DayEnd: end})
	if e != nil {
		return nil, e
	}
	if len(rows) > 100 {
		out["metricsReason"] = "permission_budget_exceeded"
		return json.Marshal(out)
	}
	permissions, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	counts := map[string]int64{"waiting": 0, "assigning": 0, "errors": 0, "confirmedToday": 0, "keptToday": 0}
	today := start.Format("2006-01-02")
	checked := s.distributionPagePermissions(permissions, actor, rows)
	if permissions.Err() != nil {
		out["metricsReason"] = "permission_budget_exceeded"
		return json.Marshal(out)
	}
	for i, row := range rows {
		if checked[i].err != nil {
			return json.Marshal(out)
		}
		if !checked[i].visible {
			continue
		}
		switch row.State {
		case "waiting":
			counts["waiting"]++
		case "dispatching", "uncertain":
			counts["waiting"]++
			counts["assigning"]++
		case "requires_configuration", "failed":
			counts["errors"]++
		}
		if row.Settled && row.UpdatedAt.In(loc).Format("2006-01-02") == today {
			if row.State == "confirmed" {
				counts["confirmedToday"]++
			}
			if row.State == "kept" {
				counts["keptToday"]++
			}
		}
	}
	currentSetting, e := q.GetDistributionSettings(ctx, actor.CompanyID)
	if e != nil || currentSetting.Revision != setting.Revision {
		out["metricsReason"] = "configuration_changed"
		return json.Marshal(out)
	}
	if _, e = s.distributionActor(ctx, actor, false); e != nil {
		return nil, e
	}
	for k, v := range counts {
		out[k] = v
	}
	out["metricsAvailable"] = true
	out["metricsReason"] = "available"
	return json.Marshal(out)
}

type distributionUIAction struct {
	Action            string    `json:"action"`
	RequestID         uuid.UUID `json:"requestId"`
	ExpectedUpdatedAt time.Time `json:"expectedUpdatedAt"`
}

func (s *Service) DistributionQueueAction(ctx context.Context, actor Actor, id uuid.UUID, raw json.RawMessage) (json.RawMessage, error) {
	currentActor, e := s.distributionActor(ctx, actor, true)
	if e != nil {
		return nil, e
	}
	actor = currentActor
	var in distributionUIAction
	if e := decodeRuntime(raw, &in); e != nil {
		return nil, e
	}
	if in.RequestID == uuid.Nil || in.ExpectedUpdatedAt.IsZero() {
		return nil, validation("Укажите идентификатор действия и версию сделки")
	}
	switch in.Action {
	case "recalculate", "check", "retry", "cancel":
	default:
		return nil, validation("Неизвестное действие")
	}
	in.ExpectedUpdatedAt = in.ExpectedUpdatedAt.UTC()
	canonical, _ := json.Marshal(in)
	q := db.New(s.pool)
	row, e := q.LockDistributionQueue(ctx, id)
	if isNoRows(e) || e == nil && row.CompanyID != actor.CompanyID {
		return nil, notFound("Сделка очереди")
	}
	if e != nil {
		return nil, e
	}
	if e = q.EnsureDistributionAvailabilityVersion(ctx, actor.CompanyID); e != nil {
		return nil, e
	}
	checkedInputs, e := q.LockDistributionAvailabilityVersion(ctx, actor.CompanyID)
	if e != nil {
		return nil, e
	}
	permission, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	visible, e := s.distributionQueuePermission(permission, actor, row)
	if e != nil {
		return nil, e
	}
	if !visible {
		return nil, forbidden("Нет доступа к сделке amoCRM")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	// Same lock order as runtime decision and result processing. Serialize request
	// identities company-wide before queue rows, including cross-queue duplicate keys.
	if e = q.EnsureDistributionAvailabilityVersion(ctx, actor.CompanyID); e != nil {
		return nil, e
	}
	currentInputs, e := q.LockDistributionAvailabilityVersion(ctx, actor.CompanyID)
	if e != nil {
		return nil, e
	}

	if e = s.checkWidgetMutation(ctx); e != nil {
		return nil, e
	}
	if _, e = s.distributionActor(ctx, actor, true); e != nil {
		return nil, e
	}
	old, e := q.GetDistributionUIAction(ctx, db.GetDistributionUIActionParams{CompanyID: actor.CompanyID, RequestID: in.RequestID})
	if e == nil {
		var stored distributionUIAction
		_ = json.Unmarshal(old.Payload, &stored)
		body, _ := json.Marshal(stored)
		if old.QueueID != id || old.ActorID != actor.UserID || !bytes.Equal(body, canonical) {
			return nil, conflict("Идентификатор действия уже использован с другими параметрами")
		}
		if e = tx.Commit(ctx); e != nil {
			return nil, e
		}
		return s.DistributionQueueDetail(ctx, actor, id)
	}
	if !isNoRows(e) {
		return nil, e
	}
	if currentInputs.Revision != checkedInputs.Revision {
		return nil, conflict("Права или настройки изменились: повторите проверку")
	}
	row, e = q.LockDistributionQueue(ctx, id)
	if e != nil {
		return nil, e
	}
	if row.CompanyID != actor.CompanyID {
		return nil, notFound("Сделка очереди")
	}
	if !row.UpdatedAt.Equal(in.ExpectedUpdatedAt) {
		return nil, conflict("Сделка изменилась: обновите данные")
	}
	if row.LeaseUntil.Valid && row.LeaseUntil.Time.After(s.now()) {
		return nil, conflict("Сделка обрабатывается: дождитесь обновления")
	}
	allowed := false
	for _, a := range s.queueActions(ctx, q, row) {
		if a == in.Action {
			allowed = true
		}
	}
	if !allowed {
		return nil, conflict("Действие недоступно в текущем состоянии сделки")
	}
	// Recheck current binding/mapping revisions after the network permission check.
	r, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: row.RuleID})
	if e != nil {
		return nil, e
	}
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: r.BindingID})
	if e != nil {
		return nil, e
	}
	if b.State != "active" || b.Revision != r.BindingRevision || b.MappingRevision != b.MappingAckRevision {
		return nil, conflict("Связь или сопоставления изменились")
	}
	state, reason := row.State, "user_"+in.Action
	switch in.Action {
	case "retry":
		if e = q.RetryDistributionQueueUI(ctx, id); e != nil {
			return nil, e
		}
		state = "waiting"
	case "cancel":
		if !row.OperationID.Valid {
			e = q.CancelUndispatchedDistributionQueueUI(ctx, id)
			state = "cancelled"
			reason = "user_cancelled"
		} else {
			e = q.WakeDistributionQueueUI(ctx, db.WakeDistributionQueueUIParams{ID: id, CancelRequested: true})
		}
	default:
		e = q.WakeDistributionQueueUI(ctx, db.WakeDistributionQueueUIParams{ID: id, CancelRequested: row.CancelRequested})
	}
	if e != nil {
		return nil, e
	}
	if e = q.AddDistributionQueueHistory(ctx, db.AddDistributionQueueHistoryParams{QueueID: id, State: state, Reason: reason, Payload: canonical}); e != nil {
		return nil, e
	}
	if e = q.SaveDistributionUIAction(ctx, db.SaveDistributionUIActionParams{CompanyID: actor.CompanyID, RequestID: in.RequestID, QueueID: id, ActorID: actor.UserID, Payload: canonical}); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return s.DistributionQueueDetail(ctx, actor, id)
}
func (s *Service) ConfigureDistributionGroup(ctx context.Context, actor Actor, id uuid.UUID, raw json.RawMessage) (json.RawMessage, error) {
	currentActor, e := s.distributionActor(ctx, actor, true)
	if e != nil {
		return nil, e
	}
	actor = currentActor
	var in struct {
		ExpectedRevision int64       `json:"expectedRevision"`
		Name             string      `json:"name"`
		Members          []uuid.UUID `json:"memberIds"`
		Disabled         []uuid.UUID `json:"disabledMemberIds"`
		Active           *bool       `json:"active"`
		Algorithm        string      `json:"algorithm"`
	}
	if e := decodeRuntime(raw, &in); e != nil {
		return nil, e
	}
	name, e := requiredText(in.Name, "Укажите название группы")
	if e != nil {
		return nil, e
	}
	if !safeRevision(in.ExpectedRevision) || in.Active == nil || in.Algorithm != "round_robin" || len(in.Members) == 0 || len(uniqueUUIDs(in.Members)) != len(in.Members) || len(uniqueUUIDs(in.Disabled)) != len(in.Disabled) || len(intersection(in.Disabled, in.Members)) != len(in.Disabled) {
		return nil, validation("Проверьте участников, алгоритм и версию группы")
	}
	if e = s.validateDistributionMembers(ctx, actor.CompanyID, in.Members); e != nil {
		return nil, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.EnsureDistributionAvailabilityVersion(ctx, actor.CompanyID); e != nil {
		return nil, e
	}
	if _, e = q.LockDistributionAvailabilityVersion(ctx, actor.CompanyID); e != nil {
		return nil, e
	}
	if e = s.checkWidgetMutation(ctx); e != nil {
		return nil, e
	}
	if _, e = s.distributionActor(ctx, actor, true); e != nil {
		return nil, e
	}
	if e = s.validateDistributionMembers(ctx, actor.CompanyID, in.Members); e != nil {
		return nil, e
	}
	current, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: actor.CompanyID, ID: id})
	if isNoRows(e) {
		return nil, notFound("Группа")
	}
	if e != nil {
		return nil, e
	}
	changed := !uuidOrderEqual(current.MemberIds, in.Members) || !uuidOrderEqual(current.DisabledMemberIds, in.Disabled)
	if changed {
		busy, e := q.DistributionGroupHasUnfinishedOperation(ctx, db.DistributionGroupHasUnfinishedOperationParams{CompanyID: actor.CompanyID, GroupID: id})
		if e != nil {
			return nil, e
		}
		if busy {
			return nil, conflict("Сначала завершите сверку операции группы; сейчас можно приостановить распределение")
		}
	}
	row, e := q.UpdateDistributionGroupConfiguration(ctx, db.UpdateDistributionGroupConfigurationParams{CompanyID: actor.CompanyID, ID: id, Revision: in.ExpectedRevision, Name: name, MemberIds: in.Members, DisabledMemberIds: in.Disabled, Active: *in.Active})
	if isNoRows(e) {
		return nil, conflict("Группа изменилась: обновите данные")
	}
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return json.Marshal(map[string]any{"id": row.ID, "name": row.Name, "description": textPointer(row.Description), "active": row.Active, "algorithm": row.Algorithm, "memberIds": row.MemberIds, "disabledMemberIds": row.DisabledMemberIds, "source": row.Source, "dealLimit": row.DealLimit, "unclaimedMinutes": row.UnclaimedMinutes, "createdAt": row.CreatedAt, "revision": row.Revision})
}

var distributionCRMHost = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.(amocrm\.ru|amocrm\.com|kommo\.com)$`)

func safeDistributionLeadURL(raw *string, lead string) *string {
	if raw == nil {
		return nil
	}
	u, e := url.Parse(*raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !distributionCRMHost.MatchString(strings.ToLower(u.Host)) || u.RawQuery != "" || u.Fragment != "" || u.Path != "/leads/detail/"+lead {
		return nil
	}
	return raw
}

func nullableGroup(id uuid.UUID) uuid.NullUUID {
	if id == uuid.Nil {
		return uuid.NullUUID{}
	}
	return nullID(id)
}

func uuidOrderEqual(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Service) distributionQueueLeadURL(ctx context.Context, actor Actor, row db.DistributionQueue, visible bool) *string {
	if !visible {
		return nil
	}
	q := db.New(s.pool)
	r, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: row.RuleID})
	if e != nil {
		return nil
	}
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: r.BindingID})
	if e != nil || b.State != "active" || b.Revision != r.BindingRevision || b.AccountID != row.AccountID || !b.AccountDomain.Valid {
		return nil
	}
	url := "https://" + b.AccountDomain.String + "/leads/detail/" + row.LeadID
	return safeDistributionLeadURL(&url, row.LeadID)
}
