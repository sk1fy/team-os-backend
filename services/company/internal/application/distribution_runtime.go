package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	schedule "github.com/sk1fy/team-os-backend/services/company/internal/domain/schedule"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type DistributionAssignmentCore interface {
	DistributionDeliveryCore
	Assign(context.Context, corebridge.Assignment, uuid.UUID) (corebridge.AssignmentReceipt, error)
	CancelAssignment(context.Context, corebridge.Scope, uuid.UUID, uuid.UUID, int64) (corebridge.Operation, error)
	ReconcileAssignment(context.Context, corebridge.Scope, uuid.UUID, uuid.UUID, int64) (corebridge.Operation, error)
	ControlAssignment(context.Context, corebridge.Scope, uuid.UUID, uuid.UUID, string, json.RawMessage) (corebridge.Operation, error)
}

func nullID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }

type DistributionAvailability struct {
	EmployeeID uuid.UUID  `json:"employeeId"`
	CRMUserID  *string    `json:"crmUserId"`
	Available  bool       `json:"available"`
	Reason     string     `json:"reason"`
	Until      *time.Time `json:"until"`
	NextShift  *time.Time `json:"nextShift"`
}

func (s *Service) distributionAvailability(ctx context.Context, q *db.Queries, b db.DistributionBinding, g db.DistributionGroup, timezone string, now time.Time, refs *corebridge.References) ([]DistributionAvailability, []byte, error) {
	rows, e := q.DistributionMemberInputs(ctx, db.DistributionMemberInputsParams{CompanyID: b.CompanyID, BindingID: b.ID, Column3: g.MemberIds})
	if e != nil {
		return nil, nil, e
	}
	loc, e := time.LoadLocation(timezone)
	if e != nil || timezone == "" || timezone == "Local" {
		loc = time.UTC
	}
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	exceptions, e := q.DistributionMemberExceptions(ctx, db.DistributionMemberExceptionsParams{CompanyID: b.CompanyID, Column2: g.MemberIds, Date: day.AddDate(0, 0, -1), Date_2: day.AddDate(0, 0, 35)})
	if e != nil {
		return nil, nil, e
	}
	raw, _ := json.Marshal(struct {
		Group           db.DistributionGroup
		Rows            []db.DistributionMemberInputsRow
		Exceptions      []db.ShiftException
		Timezone        string
		MappingRevision int64
	}{g, rows, exceptions, timezone, b.MappingRevision})
	hash := sha256.Sum256(raw)
	inputs := map[uuid.UUID]db.DistributionMemberInputsRow{}
	for _, r := range rows {
		inputs[r.ID] = r
	}
	disabled := map[uuid.UUID]bool{}
	for _, id := range g.DisabledMemberIds {
		disabled[id] = true
	}
	out := make([]DistributionAvailability, 0, len(g.MemberIds))
	for _, id := range g.MemberIds {
		r, ok := inputs[id]
		a := DistributionAvailability{EmployeeID: id, Reason: "employee_deleted"}
		if ok {
			switch {
			case r.Status != "active":
				a.Reason = "employee_inactive"
			case r.ExternalDeletedAt.Valid:
				a.Reason = "crm_user_deleted"
			case disabled[id]:
				a.Reason = "member_disabled"
			case !r.CrmUserID.Valid || r.MappingState.String != "verified":
				a.Reason = "mapping_unavailable"
			case refs == nil || !refs.FreshUntil.After(now):
				a.Reason = "crm_users_unavailable"
			case !crmUserActive(refs, r.CrmUserID.String):
				a.Reason = "crm_user_inactive"
			case b.MappingRevision != b.MappingAckRevision:
				a.Reason = "mapping_pending"
			default:
				crm := r.CrmUserID.String
				a.CRMUserID = &crm
				var template *schedule.Template
				if len(r.Template) > 0 {
					var t schedule.Template
					if json.Unmarshal(r.Template, &t) == nil {
						template = &t
					}
				}
				ex := map[string]schedule.Exception{}
				for _, x := range exceptions {
					if x.UserID == id {
						ex[x.Date.Format(time.DateOnly)] = schedule.Exception{Type: schedule.ShiftType(x.Type), Start: pgTimeText(x.StartTime), End: pgTimeText(x.EndTime)}
					}
				}
				v := schedule.AvailableAt(template, ex, timezone, now)
				a.Available, a.Reason, a.Until, a.NextShift = v.Available, v.Reason, v.Until, v.Next
			}
		}
		out = append(out, a)
	}
	return out, hash[:], nil
}
func (s *Service) GetDistributionAvailability(ctx context.Context, actor Actor, groupID, ruleID uuid.UUID) ([]DistributionAvailability, error) {
	if _, e := s.distributionActor(ctx, actor, false); e != nil {
		return nil, e
	}
	q := db.New(s.pool)
	r, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: ruleID})
	if e != nil || r.GroupID != groupID {
		return nil, notFound("Правило распределения")
	}
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: r.BindingID})
	if e != nil {
		return nil, e
	}
	g, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: actor.CompanyID, ID: groupID})
	if e != nil {
		return nil, e
	}
	settings, e := q.GetDistributionSettings(ctx, actor.CompanyID)
	if e != nil {
		return nil, validation("Укажите часовой пояс компании")
	}
	refs, re := s.readDistributionReferences(ctx, bindingScope(b))
	if re != nil {
		return nil, re
	}
	out, _, e := s.distributionAvailability(ctx, q, b, g, settings.Timezone, s.now(), &refs)
	return out, e
}
func (s *Service) SaveDistributionTimezone(ctx context.Context, actor Actor, timezone string) (db.DistributionSetting, error) {
	if _, e := s.distributionActor(ctx, actor, true); e != nil {
		return db.DistributionSetting{}, e
	}
	if _, e := time.LoadLocation(timezone); e != nil || timezone == "" || timezone == "Local" {
		return db.DistributionSetting{}, validation("Укажите часовой пояс IANA")
	}
	return db.New(s.pool).SaveDistributionSettings(ctx, db.SaveDistributionSettingsParams{CompanyID: actor.CompanyID, Timezone: timezone})
}
func (s *Service) CreateDistributionRuntimeRule(ctx context.Context, actor Actor, input db.CreateDistributionRuleParams) (db.DistributionRule, error) {
	if _, e := s.distributionActor(ctx, actor, true); e != nil {
		return db.DistributionRule{}, e
	}
	if input.ExecutionMode == "" {
		input.ExecutionMode = "live"
	}
	if input.ExecutionMode != "live" && input.ExecutionMode != "observe" {
		return db.DistributionRule{}, validation("Неизвестный режим распределения")
	}
	input.CompanyID = actor.CompanyID
	input.ID = uuid.New()
	q := db.New(s.pool)
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: input.BindingID})
	if e != nil || b.State != "active" || b.Revision != input.BindingRevision || b.AccountID != input.AccountID || b.MappingRevision != b.MappingAckRevision {
		return db.DistributionRule{}, validation("Связь или сопоставления недоступны")
	}
	g, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: actor.CompanyID, ID: input.GroupID})
	if e != nil || g.Algorithm != "round_robin" {
		return db.DistributionRule{}, validation("Для первой версии нужен round_robin")
	}
	if _, e = q.GetDistributionSettings(ctx, actor.CompanyID); e != nil {
		return db.DistributionRule{}, validation("Укажите часовой пояс компании")
	}
	if !validCRMID(input.PipelineID) || !validCRMID(input.StatusID) {
		return db.DistributionRule{}, validation("Укажите этап amoCRM")
	}
	refs, e := s.readDistributionReferences(ctx, bindingScope(b))
	if e != nil {
		return db.DistributionRule{}, internal("Не удалось проверить этап amoCRM", e)
	}
	found := false
	for _, p := range refs.Pipelines {
		for _, st := range p.Statuses {
			if p.ID == input.PipelineID && st.ID == input.StatusID {
				found = true
			}
		}
	}
	if !found {
		return db.DistributionRule{}, validation("Этап amoCRM недоступен")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return db.DistributionRule{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	if e = q.EnsureDistributionAvailabilityVersion(ctx, actor.CompanyID); e != nil {
		return db.DistributionRule{}, e
	}
	if _, e = q.LockDistributionAvailabilityVersion(ctx, actor.CompanyID); e != nil {
		return db.DistributionRule{}, e
	}
	if e = s.checkWidgetMutation(ctx); e != nil {
		return db.DistributionRule{}, e
	}
	if _, e = s.distributionActor(ctx, actor, true); e != nil {
		return db.DistributionRule{}, e
	}
	current, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: input.BindingID})
	if e != nil {
		return db.DistributionRule{}, e
	}
	if current.State != "active" || bindingScope(current) != bindingScope(b) || current.MappingRevision != current.MappingAckRevision {
		return db.DistributionRule{}, conflict("Связь изменилась")
	}
	if !refs.FreshUntil.After(s.now()) {
		return db.DistributionRule{}, conflict("Справочник amoCRM устарел")
	}
	if input.Active {
		busy, e := q.DistributionPointHasUnfinishedOperation(ctx, db.DistributionPointHasUnfinishedOperationParams{AccountID: input.AccountID, PipelineID: input.PipelineID, StatusID: input.StatusID})
		if e != nil {
			return db.DistributionRule{}, e
		}
		if busy {
			return db.DistributionRule{}, conflict("Сначала завершите сверку операций этапа")
		}
	}
	exists, e := q.DistributionGroupHasRule(ctx, db.DistributionGroupHasRuleParams{CompanyID: actor.CompanyID, GroupID: input.GroupID})
	if e != nil {
		return db.DistributionRule{}, e
	}
	if exists {
		return db.DistributionRule{}, conflict("У группы уже есть правило; для новой точки создайте отдельную группу")
	}
	r, e := q.CreateDistributionRule(ctx, input)
	if isUniqueViolation(e) {
		return r, conflict("Для этого этапа уже есть активное правило")
	}
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (s *Service) queueState(ctx context.Context, q *db.Queries, row db.DistributionQueue, state, reason string, next time.Time, cancel bool) error {
	return q.UpdateDistributionQueueState(ctx, db.UpdateDistributionQueueStateParams{ID: row.ID, State: state, Reason: reason, NextAttemptAt: next, CancelRequested: cancel, LeaseToken: row.LeaseToken})
}

func nextMemberOrder(current, old []uuid.UUID, next int) []uuid.UUID {
	start := 0
	if len(old) > 0 {
		for n := 0; n < len(old); n++ {
			id := old[(next+n)%len(old)]
			found := false
			for i, x := range current {
				if x == id {
					start = i
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	out := make([]uuid.UUID, 0, len(current))
	for n := range current {
		out = append(out, current[(start+n)%len(current)])
	}
	return out
}
func operationMatchesAssignment(o corebridge.Operation, a corebridge.Assignment) bool {
	c := a.Command
	return o.OperationID == c.OperationID && o.Scope == a.Scope && o.LeadID == c.ExpectedSnapshot.LeadID && o.EpisodeID == c.EpisodeID && o.DecisionID == c.DecisionID && o.RuleID == c.RuleID && o.GroupID == c.GroupID && o.RuleRevision == c.RuleRevision && o.AvailabilityRevision == c.AvailabilityRevision && o.ClaimRevision == c.ClaimRevision && o.EventID == a.EventID && o.CorrelationID == a.CorrelationID && o.TargetResponsibleUserID == c.TargetResponsibleUserID
}

func pgTimeText(t pgtype.Time) string {
	if !t.Valid || t.Microseconds < 0 || t.Microseconds >= 86400000000 || t.Microseconds%60000000 != 0 {
		return ""
	}
	minutes := t.Microseconds / 60000000
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func (s *Service) readDistributionReferences(ctx context.Context, scope corebridge.Scope) (corebridge.References, error) {
	if s.distributionCore == nil {
		return corebridge.References{}, validation("Core не настроен")
	}
	r, e := s.distributionCore.References(ctx, scope)
	if e != nil {
		return r, e
	}
	if validateDistributionReferences(r, s.now()) != nil {
		return r, validation("Справочник amoCRM недоступен")
	}
	if !r.FreshUntil.After(s.now()) || r.FetchedAt.After(s.now().Add(time.Minute)) {
		return r, validation("Справочник amoCRM устарел")
	}
	return r, nil
}
func crmUserActive(refs *corebridge.References, id string) bool {
	for _, u := range refs.Users {
		if u.ID == id {
			return u.IsActive
		}
	}
	return false
}
