package application

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type manualAssignmentInput struct {
	ObservationID uuid.UUID `json:"observationId"`
	RequestID     uuid.UUID `json:"requestId"`
	EmployeeID    uuid.UUID `json:"employeeId"`
	ExpectedOwner string    `json:"expectedResponsibleUserId"`
}

type manualAssignmentStatus struct {
	RequestID  uuid.UUID `json:"requestId"`
	EmployeeID uuid.UUID `json:"employeeId"`
	State      string    `json:"state"`
	Reason     string    `json:"reason"`
}

type manualAssignmentCandidate struct {
	EmployeeID uuid.UUID `json:"employeeId"`
	CRMUserID  string    `json:"crmUserId"`
	Name       string    `json:"name"`
}

type manualAssignmentOptions struct {
	CurrentOwner string                      `json:"currentResponsibleUserId"`
	Candidates   []manualAssignmentCandidate `json:"candidates"`
	Assignment   *manualAssignmentStatus     `json:"assignment,omitempty"`
}

func manualStatus(row db.DistributionQueue) manualAssignmentStatus {
	return manualAssignmentStatus{row.ManualRequestID.UUID, row.ManualEmployeeID.UUID, row.State, row.Reason}
}

func (s *Service) manualObservation(ctx context.Context, actor Actor, ruleID, observationID uuid.UUID) (db.DistributionObservation, error) {
	if _, err := s.distributionActor(ctx, actor, true); err != nil {
		return db.DistributionObservation{}, err
	}
	row, err := db.New(s.pool).GetDistributionObservation(ctx, db.GetDistributionObservationParams{CompanyID: actor.CompanyID, RuleID: ruleID, ID: observationID})
	if isNoRows(err) {
		return row, notFound("Наблюдение")
	}
	if err != nil {
		return row, err
	}
	permission, err := s.DistributionLeadPermission(ctx, actor, row.BindingID, row.LeadID)
	if err != nil {
		return row, err
	}
	if !permission.CanViewLead {
		return row, notFound("Наблюдение")
	}
	return row, nil
}

// Manual assignments use the current CRM snapshot and explicit operator
// confirmation. Unproven transition history still blocks automatic admission.
func manualEntryMatches(entry db.DistributionObservedEntry, head db.DistributionLeadHead, snapshot *corebridge.LeadSnapshot) bool {
	return (entry.State == "checking" || (entry.State == "needs_configuration" && entry.Evidence == "ambiguous_reentry")) &&
		head.CurrentEntryID.Valid && head.CurrentEntryID.UUID == entry.ID &&
		head.BindingID == entry.BindingID && head.BindingRevision == entry.BindingRevision &&
		!head.Deleted && !head.Absent && snapshot != nil &&
		snapshot.PipelineID == entry.PipelineID && snapshot.StatusID == entry.StatusID
}

func (s *Service) manualCandidates(ctx context.Context, q *db.Queries, observation db.DistributionObservation) (manualAssignmentOptions, error) {
	out := manualAssignmentOptions{Candidates: []manualAssignmentCandidate{}}
	rule, err := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: observation.CompanyID, ID: observation.RuleID})
	if err != nil {
		return out, err
	}
	group, err := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: observation.CompanyID, ID: observation.GroupID})
	if err != nil {
		return out, err
	}
	binding, err := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: observation.CompanyID, ID: observation.BindingID})
	if err != nil {
		return out, err
	}
	if !rule.Active || !group.Active || rule.ExecutionMode != "observe" || rule.ExecutionEpoch != observation.ExecutionEpoch ||
		rule.GroupID != observation.GroupID || rule.BindingID != observation.BindingID || rule.BindingRevision != observation.BindingRevision ||
		binding.State != "active" || binding.Revision != observation.BindingRevision || binding.MappingRevision != binding.MappingAckRevision {
		return out, conflict("Правило наблюдения изменилось")
	}
	core, ok := s.deliveryCore.(DistributionAssignmentCore)
	if !ok {
		return out, upstream("Core недоступен", nil)
	}
	lead, err := core.ReadLead(ctx, bindingScope(binding), observation.LeadID)
	if err != nil {
		return out, upstream("Не удалось прочитать сделку", err)
	}
	entry, err := q.GetDistributionObservedEntry(ctx, observation.EntryID)
	if err != nil {
		return out, err
	}
	head, err := q.LockDistributionLeadHead(ctx, db.LockDistributionLeadHeadParams{AccountID: observation.AccountID, LeadID: observation.LeadID})
	if err != nil {
		return out, err
	}
	if lead.Scope != bindingScope(binding) || lead.LeadID != observation.LeadID || lead.Deleted || lead.Absent ||
		!validSnapshot(lead.Snapshot, observation.LeadID) || !manualEntryMatches(entry, head, lead.Snapshot) ||
		!ruleEntrySnapshotMatches(rule, entry, lead.Snapshot) {
		return out, conflict("Сделка изменилась")
	}
	out.CurrentOwner = lead.Snapshot.ResponsibleUserID
	refs, err := s.readDistributionReferences(ctx, bindingScope(binding))
	if err != nil {
		return out, upstream("Не удалось получить сотрудников amoCRM", err)
	}
	settings, err := s.distributionBindingSettings(ctx, q, binding)
	if err != nil {
		return out, err
	}
	available, _, err := s.distributionAvailability(ctx, q, binding, group, settings.Timezone, s.now(), &refs)
	if err != nil {
		return out, err
	}
	names := make(map[string]string, len(refs.Users))
	for _, user := range refs.Users {
		names[user.ID] = user.Name
	}
	for _, candidate := range available {
		if candidate.Available && candidate.CRMUserID != nil {
			id := *candidate.CRMUserID
			out.Candidates = append(out.Candidates, manualAssignmentCandidate{candidate.EmployeeID, id, names[id]})
		}
	}
	return out, nil
}

func (s *Service) DistributionManualAssignmentOptions(ctx context.Context, actor Actor, ruleID, observationID uuid.UUID) (json.RawMessage, error) {
	observation, err := s.manualObservation(ctx, actor, ruleID, observationID)
	if err != nil {
		return nil, err
	}
	q := db.New(s.pool)
	row, err := q.GetDistributionManualAssignment(ctx, db.GetDistributionManualAssignmentParams{CompanyID: actor.CompanyID, ManualObservationID: nullID(observationID)})
	if err == nil {
		status := manualStatus(row)
		return json.Marshal(manualAssignmentOptions{CurrentOwner: strconv.FormatInt(row.ManualExpectedResponsibleUserID.Int64, 10), Candidates: []manualAssignmentCandidate{}, Assignment: &status})
	}
	if !isNoRows(err) {
		return nil, err
	}
	out, err := s.manualCandidates(ctx, q, observation)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (s *Service) AssignDistributionObservation(ctx context.Context, actor Actor, ruleID uuid.UUID, raw json.RawMessage) (json.RawMessage, error) {
	var in manualAssignmentInput
	if json.Unmarshal(raw, &in) != nil || in.ObservationID == uuid.Nil || in.RequestID == uuid.Nil || in.EmployeeID == uuid.Nil || !validCRMID(in.ExpectedOwner) {
		return nil, validation("Некорректные параметры назначения")
	}
	expected, _ := strconv.ParseInt(in.ExpectedOwner, 10, 64)
	observation, err := s.manualObservation(ctx, actor, ruleID, in.ObservationID)
	if err != nil {
		return nil, err
	}
	request := db.GetDistributionManualAssignmentRequestParams{CompanyID: actor.CompanyID, ManualRequestID: nullID(in.RequestID)}
	q := db.New(s.pool)
	replay := func(row db.DistributionQueue) (json.RawMessage, error) {
		if row.ManualObservationID.UUID != in.ObservationID || row.ManualActorID.UUID != actor.UserID ||
			row.ManualEmployeeID.UUID != in.EmployeeID || row.ManualExpectedResponsibleUserID.Int64 != expected {
			return nil, conflict("Идентификатор уже использован для другого назначения")
		}
		return json.Marshal(manualStatus(row))
	}
	if row, err := q.GetDistributionManualAssignmentRequest(ctx, request); err == nil {
		return replay(row)
	} else if !isNoRows(err) {
		return nil, err
	}
	options, err := s.manualCandidates(ctx, q, observation)
	if err != nil {
		return nil, err
	}
	if options.CurrentOwner != in.ExpectedOwner {
		return nil, conflict("Ответственный сделки изменился")
	}
	found := false
	for _, candidate := range options.Candidates {
		if candidate.EmployeeID == in.EmployeeID {
			found = true
			break
		}
	}
	if !found {
		return nil, conflict("Сотрудник больше недоступен")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	rule, err := q.LockDistributionRule(ctx, db.LockDistributionRuleParams{CompanyID: actor.CompanyID, ID: ruleID})
	if err != nil {
		return nil, err
	}
	if row, err := q.GetDistributionManualAssignmentRequest(ctx, request); err == nil {
		return replay(row)
	} else if !isNoRows(err) {
		return nil, err
	}
	if !rule.Active || rule.ExecutionMode != "observe" || rule.ExecutionEpoch != observation.ExecutionEpoch ||
		rule.GroupID != observation.GroupID || rule.BindingID != observation.BindingID || rule.BindingRevision != observation.BindingRevision {
		return nil, conflict("Правило наблюдения изменилось")
	}
	_, err = q.CreateDistributionManualAssignment(ctx, db.CreateDistributionManualAssignmentParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, EntryID: observation.EntryID, RuleID: ruleID,
		GroupID: observation.GroupID, AccountID: observation.AccountID, LeadID: observation.LeadID,
		ExecutionEpoch: observation.ExecutionEpoch, ManualObservationID: nullID(in.ObservationID),
		ManualRequestID: nullID(in.RequestID), ManualEmployeeID: nullID(in.EmployeeID), ManualActorID: nullID(actor.UserID),
		ManualExpectedResponsibleUserID: pgtype.Int8{Int64: expected, Valid: true},
	})
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" &&
			(pg.ConstraintName == "distribution_queue_entry_id_key" || pg.ConstraintName == "distribution_queue_manual_observation_id_key") {
			return nil, conflict("Для сделки или наблюдения уже существует назначение")
		}
		return nil, err
	}
	row, err := q.GetDistributionManualAssignmentRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return replay(row)
}
