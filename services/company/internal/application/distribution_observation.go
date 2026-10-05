package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type distributionPlan struct {
	Employee     uuid.UUID
	Kind, Reason string
	Next         *time.Time
}

// chooseDistributionPlan is shared by live execution and observation. It never
// reserves a turn or writes state; the caller owns all execution boundaries.
func chooseDistributionPlan(owner string, keep bool, available []DistributionAvailability, members, order []uuid.UUID, next int) distributionPlan {
	if keep {
		for _, a := range available {
			if a.Available && a.CRMUserID != nil && *a.CRMUserID == owner {
				return distributionPlan{Employee: a.EmployeeID, Kind: "keep", Reason: "current_responsible_available"}
			}
		}
	}
	for _, id := range nextMemberOrder(members, order, next) {
		for _, a := range available {
			if a.EmployeeID == id && a.Available {
				return distributionPlan{Employee: id, Kind: "assign", Reason: "next_available_member"}
			}
		}
	}
	var shift *time.Time
	for _, a := range available {
		if a.NextShift != nil && (shift == nil || a.NextShift.Before(*shift)) {
			copy := *a.NextShift
			shift = &copy
		}
	}
	if shift == nil {
		return distributionPlan{Kind: "requires_configuration", Reason: "no_valid_members_within_horizon"}
	}
	return distributionPlan{Kind: "wait", Reason: "no_available_members", Next: shift}
}

type DistributionObservation struct {
	BindingRevision          int64      `json:"bindingRevision"`
	SourceOccurredAt         *time.Time `json:"sourceOccurredAt"`
	SourceReceivedAt         *time.Time `json:"sourceReceivedAt"`
	CRMObservedAt            time.Time  `json:"crmObservedAt"`
	ID                       uuid.UUID  `json:"id"`
	RuleID                   uuid.UUID  `json:"ruleId"`
	GroupID                  uuid.UUID  `json:"groupId"`
	EntryID                  uuid.UUID  `json:"entryId"`
	EventID                  uuid.UUID  `json:"eventId"`
	ExecutionEpoch           int64      `json:"executionEpoch"`
	RuleRevision             int64      `json:"ruleRevision"`
	AvailabilityRevision     int64      `json:"availabilityRevision"`
	ObservationRevision      int64      `json:"observationRevision"`
	CheckedAt                time.Time  `json:"checkedAt"`
	DecisionKind             string     `json:"decisionKind"`
	Reason                   string     `json:"reason"`
	LeadID                   string     `json:"leadId"`
	CurrentResponsibleUserID *string    `json:"currentResponsibleUserId"`
	PlannedEmployeeID        *uuid.UUID `json:"plannedEmployeeId"`
	PlannedResponsibleUserID *string    `json:"plannedResponsibleUserId"`
	NextShiftAt              *time.Time `json:"nextShiftAt"`
}

func (s *Service) ProcessDistributionObservation(ctx context.Context) (found bool, err error) {
	q := db.New(s.pool)
	job, e := q.ClaimDistributionObservation(ctx, nullID(uuid.New()))
	if isNoRows(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			_ = q.RetryDistributionObservation(cleanup, db.RetryDistributionObservationParams{ID: job.ID, LeaseToken: job.LeaseToken})
		}
	}()
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: job.CompanyID, ID: job.BindingID})
	if e != nil {
		return true, e
	}
	if s.deliveryCore == nil {
		return true, upstream("Core недоступен", nil)
	}
	observed, e := s.deliveryCore.ReadLead(ctx, bindingScope(b), job.LeadID)
	if e != nil {
		return true, coreError(e)
	}
	if observed.Scope != bindingScope(b) || observed.LeadID != job.LeadID || !safeRevision(observed.ObservationRevision) || !validSnapshot(observed.Snapshot, job.LeadID) {
		return true, upstream("Наблюдение CRM не подтверждено", nil)
	}
	refs, e := s.readDistributionReferences(ctx, bindingScope(b))
	if e != nil {
		return true, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return true, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current := db.New(tx)
	if e = current.EnsureDistributionAvailabilityVersion(ctx, job.CompanyID); e != nil {
		return true, e
	}
	version, e := current.LockDistributionAvailabilityVersion(ctx, job.CompanyID)
	if e != nil {
		return true, e
	}
	job, e = current.LockDistributionObservation(ctx, db.LockDistributionObservationParams{ID: job.ID, LeaseToken: job.LeaseToken})
	if e != nil {
		return true, e
	}
	r, e := current.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: job.CompanyID, ID: job.RuleID})
	if e != nil {
		return true, e
	}
	g, e := current.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: job.CompanyID, ID: r.GroupID})
	if e != nil {
		return true, e
	}
	latest, e := current.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: job.CompanyID, ID: job.BindingID})
	if e != nil {
		return true, e
	}
	entry, e := current.GetDistributionObservedEntry(ctx, job.EntryID)
	if e != nil {
		return true, e
	}
	head, e := current.LockDistributionLeadHead(ctx, db.LockDistributionLeadHeadParams{AccountID: job.AccountID, LeadID: job.LeadID})
	if e != nil {
		return true, e
	}
	out := DistributionObservation{ID: job.ID, RuleID: r.ID, GroupID: r.GroupID, EntryID: job.EntryID, EventID: job.EventID, ExecutionEpoch: job.ExecutionEpoch, RuleRevision: r.Revision, AvailabilityRevision: version.Revision, ObservationRevision: observed.ObservationRevision, CheckedAt: s.now().UTC(), CRMObservedAt: observed.ObservedAt, BindingRevision: job.BindingRevision, LeadID: job.LeadID, DecisionKind: "skipped", Reason: "execution_mode_changed"}
	if entry.SourceOccurredAt.Valid {
		out.SourceOccurredAt = &entry.SourceOccurredAt.Time
	}
	if entry.SourceReceivedAt.Valid {
		out.SourceReceivedAt = &entry.SourceReceivedAt.Time
	}
	company, ce := current.GetCompany(ctx, job.CompanyID)
	if ce != nil {
		return true, ce
	}
	switch {
	case company.Status != "active":
		out.Reason = "company_unavailable"
	case r.ExecutionMode != "observe" || r.ExecutionEpoch != job.ExecutionEpoch:
	case !r.Active || !g.Active:
		out.Reason = "group_paused"
	case g.Algorithm != "round_robin":
		out.DecisionKind = "requires_configuration"
		out.Reason = "algorithm_unsupported"
	case latest.State != "active" || bindingScope(latest) != observed.Scope || latest.Revision != job.BindingRevision || latest.MappingRevision != latest.MappingAckRevision:
		out.Reason = "binding_unavailable"
	case !head.CurrentEntryID.Valid || head.CurrentEntryID.UUID != job.EntryID || entry.State == "cancelled" || observed.Snapshot == nil || observed.Snapshot.PipelineID != r.PipelineID || observed.Snapshot.StatusID != r.StatusID:
		out.Reason = "observed_stage_exit"
	case entry.State == "needs_configuration" || !entry.SourceOccurredAt.Valid:
		out.DecisionKind = "requires_configuration"
		out.Reason = "source_entry_unproven"
	default:
		if !refs.FreshUntil.After(s.now()) {
			return true, upstream("Справочник CRM устарел", nil)
		}
		owner := observed.Snapshot.ResponsibleUserID
		out.CurrentResponsibleUserID = &owner
		settings, se := current.GetDistributionSettings(ctx, job.CompanyID)
		if se != nil {
			out.DecisionKind = "requires_configuration"
			out.Reason = "timezone_required"
		} else {
			available, _, ae := s.distributionAvailability(ctx, current, latest, g, settings.Timezone, s.now(), &refs)
			if ae != nil {
				return true, ae
			}
			cursor, ce := current.GetDistributionObservationCursor(ctx, db.GetDistributionObservationCursorParams{CompanyID: job.CompanyID, GroupID: g.ID})
			if ce != nil && !isNoRows(ce) {
				return true, ce
			}
			plan := chooseDistributionPlan(owner, r.KeepCurrent, available, g.MemberIds, cursor.CursorOrder, int(cursor.CursorNext))
			out.DecisionKind = plan.Kind
			out.Reason = plan.Reason
			out.NextShiftAt = plan.Next
			if plan.Employee != uuid.Nil {
				out.PlannedEmployeeID = &plan.Employee
				for _, a := range available {
					if a.EmployeeID == plan.Employee {
						out.PlannedResponsibleUserID = a.CRMUserID
					}
				}
			}
		}
	}
	payload, e := json.Marshal(out)
	if e != nil {
		return true, e
	}
	if _, e = current.LockDistributionObservation(ctx, db.LockDistributionObservationParams{ID: job.ID, LeaseToken: job.LeaseToken}); e != nil {
		return true, e
	}
	if e = current.SaveDistributionObservation(ctx, db.SaveDistributionObservationParams{ID: job.ID, CompanyID: job.CompanyID, RuleID: r.ID, GroupID: g.ID, ExecutionEpoch: job.ExecutionEpoch, EntryID: job.EntryID, EventID: job.EventID, BindingID: job.BindingID, BindingRevision: job.BindingRevision, AccountID: job.AccountID, LeadID: job.LeadID, Payload: payload}); e != nil {
		return true, e
	}
	if e = current.FinishDistributionObservation(ctx, db.FinishDistributionObservationParams{ID: job.ID, LeaseToken: job.LeaseToken}); e != nil {
		return true, e
	}
	return true, tx.Commit(ctx)
}
func (s *Service) visibleObservation(ctx context.Context, actor Actor, row db.DistributionObservation) (*DistributionObservation, error) {
	p, e := s.DistributionLeadPermission(ctx, actor, row.BindingID, row.LeadID)
	if e != nil {
		return nil, upstream("Не удалось проверить доступ к сделке наблюдения", e)
	}
	if !p.CanViewLead {
		return nil, nil
	}
	current, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: row.BindingID})
	if e != nil {
		return nil, e
	}
	if current.State != "active" || current.Revision != row.BindingRevision {
		return nil, forbidden("Связь наблюдения недоступна")
	}
	var out DistributionObservation
	if json.Unmarshal(row.Payload, &out) != nil {
		return nil, internal("Повреждено наблюдение", nil)
	}
	if _, e = s.distributionActor(ctx, actor, false); e != nil {
		return nil, e
	}
	return &out, nil
}
func (s *Service) DistributionObservations(ctx context.Context, actor Actor, id uuid.UUID, limit, offset int32) (json.RawMessage, error) {
	if _, e := s.distributionActor(ctx, actor, false); e != nil {
		return nil, e
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, validation("Некорректная страница наблюдения")
	}
	if id == uuid.Nil {
		return nil, validation("Укажите правило")
	}
	q := db.New(s.pool)
	if _, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: id}); e != nil {
		if isNoRows(e) {
			return nil, notFound("Правило")
		}
		return nil, upstream("Источник правил недоступен", e)
	}
	rows, e := q.ListDistributionObservations(ctx, db.ListDistributionObservationsParams{CompanyID: actor.CompanyID, RuleID: id, Limit: limit + 1, Offset: offset})
	if e != nil {
		return nil, e
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	permissionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	items := []DistributionObservation{}
	for _, row := range rows {
		out, err := s.visibleObservation(permissionCtx, actor, row)
		if err != nil {
			return nil, err
		}
		if out != nil {
			items = append(items, *out)
		}
	}
	return json.Marshal(map[string]any{"items": items, "limit": limit, "offset": offset, "hasMore": hasMore, "checkedAt": s.now().UTC()})
}
func (s *Service) latestLeadObservation(ctx context.Context, actor Actor, scope corebridge.Scope, lead string) (*DistributionObservation, error) {
	row, e := db.New(s.pool).LatestDistributionLeadObservation(ctx, db.LatestDistributionLeadObservationParams{CompanyID: actor.CompanyID, BindingID: scope.BindingID, LeadID: lead})
	if isNoRows(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return s.visibleObservation(ctx, actor, row)
}

func (s *Service) widgetCurrentLead(ctx context.Context, actor Actor, scope corebridge.Scope, lead string) (map[string]any, error) {
	if s.deliveryCore == nil {
		return nil, upstream("Core недоступен", nil)
	}
	before, e := s.DistributionLeadPermission(ctx, actor, scope.BindingID, lead)
	if e != nil {
		return nil, upstream("Не удалось проверить доступ к сделке", e)
	}
	if !before.CanViewLead {
		return nil, forbidden("Нет доступа к сделке")
	}
	actual, e := s.deliveryCore.ReadLead(ctx, scope, lead)
	if e != nil {
		return nil, coreError(e)
	}
	if actual.Scope != scope || actual.LeadID != lead || !safeRevision(actual.ObservationRevision) || ((actual.Deleted || actual.Absent) != (actual.Snapshot == nil)) || !validSnapshot(actual.Snapshot, lead) || actual.ObservedAt.IsZero() || actual.ObservedAt.After(s.now().Add(time.Minute)) || s.now().Sub(actual.ObservedAt) > time.Minute {
		return nil, upstream("Актуальная сделка не подтверждена", nil)
	}
	var user, name *string
	if actual.Snapshot != nil {
		value := actual.Snapshot.ResponsibleUserID
		user = &value
		refs, err := s.readDistributionReferences(ctx, scope)
		if err == nil {
			for _, u := range refs.Users {
				if u.ID == value && u.Name != "" {
					copy := u.Name
					name = &copy
				}
			}
		}
	}
	after, e := s.DistributionLeadPermission(ctx, actor, scope.BindingID, lead)
	if e != nil {
		return nil, upstream("Не удалось повторно проверить доступ к сделке", e)
	}
	if !after.CanViewLead {
		return nil, forbidden("Нет доступа к сделке")
	}
	return map[string]any{"leadId": lead, "responsibleUserId": user, "responsibleUserName": name, "observedAt": actual.ObservedAt, "leadName": actual.LeadName, "leadUrl": safeDistributionLeadURL(actual.LeadURL, lead), "deleted": actual.Deleted, "absent": actual.Absent}, nil
}
