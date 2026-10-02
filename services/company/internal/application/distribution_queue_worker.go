package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) ProcessDistributionQueue(ctx context.Context) (bool, error) {
	core, ok := s.deliveryCore.(DistributionAssignmentCore)
	if !ok {
		return false, nil
	}
	q := db.New(s.pool)
	if _, e := q.ConsumeDistributionAvailabilityWake(ctx); e != nil {
		return false, e
	}
	if _, e := q.AdmitDistributionEntries(ctx); e != nil {
		return false, e
	}
	row, e := q.ClaimDistributionQueue(ctx, nullID(uuid.New()))
	if isNoRows(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if row.OperationID.Valid {
		return true, s.resumeDistributionAssignment(ctx, core, row)
	}
	r, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: row.CompanyID, ID: row.RuleID})
	if e != nil {
		return true, e
	}
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: row.CompanyID, ID: r.BindingID})
	if e != nil {
		return true, e
	}
	observed, e := core.ReadLead(ctx, bindingScope(b), row.LeadID)
	if e != nil {
		return true, s.queueState(ctx, q, row, "waiting", "source_unavailable", s.now().Add(time.Minute), false)
	}
	refs, re := s.readDistributionReferences(ctx, bindingScope(b))
	if re != nil {
		return true, s.queueState(ctx, q, row, "waiting", "crm_users_unavailable", s.now().Add(time.Minute), false)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return true, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	if e = q.EnsureDistributionAvailabilityVersion(ctx, row.CompanyID); e != nil {
		return true, e
	}
	v, e := q.LockDistributionAvailabilityVersion(ctx, row.CompanyID)
	if e != nil {
		return true, e
	}
	row, e = q.LockDistributionQueueLease(ctx, db.LockDistributionQueueLeaseParams{ID: row.ID, LeaseToken: row.LeaseToken})
	if e != nil {
		return true, e
	}
	r, e = q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: row.CompanyID, ID: row.RuleID})
	if e != nil {
		return true, e
	}
	b, e = q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: row.CompanyID, ID: r.BindingID})
	if e != nil {
		return true, e
	}
	g, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: row.CompanyID, ID: row.GroupID})
	if e != nil {
		return true, e
	}
	entry, e := q.GetDistributionObservedEntry(ctx, row.EntryID)
	if e != nil {
		return true, e
	}
	head, e := q.LockDistributionLeadHead(ctx, db.LockDistributionLeadHeadParams{AccountID: row.AccountID, LeadID: row.LeadID})
	if e != nil {
		return true, e
	}
	reason := ""
	state := "waiting"
	switch {
	case entry.State == "cancelled" || !head.CurrentEntryID.Valid || head.CurrentEntryID.UUID != row.EntryID || observed.Snapshot == nil || observed.Snapshot.PipelineID != r.PipelineID || observed.Snapshot.StatusID != r.StatusID:
		reason = "observed_stage_exit"
		state = "cancelled"
	case entry.State == "needs_configuration":
		reason = "ambiguous_reentry"
		state = "requires_configuration"
	case !r.Active || !g.Active:
		reason = "group_paused"
	case b.State != "active" || bindingScope(b) != observed.Scope:
		reason = "binding_unavailable"
	case g.Algorithm != "round_robin":
		reason = "algorithm_unsupported"
		state = "requires_configuration"
	}
	if reason != "" {
		if e = s.queueState(ctx, q, row, state, reason, s.now().Add(time.Minute), false); e != nil {
			return true, e
		}
		return true, tx.Commit(ctx)
	}
	settings, e := q.GetDistributionSettings(ctx, row.CompanyID)
	if e != nil {
		if e = s.queueState(ctx, q, row, "requires_configuration", "timezone_required", s.now().Add(time.Minute), false); e != nil {
			return true, e
		}
		return true, tx.Commit(ctx)
	}
	available, hash, e := s.distributionAvailability(ctx, q, b, g, settings.Timezone, s.now(), &refs)
	if e != nil {
		return true, e
	}
	selected := uuid.Nil
	kind := "assign"
	if r.KeepCurrent {
		for _, a := range available {
			if a.Available && a.CRMUserID != nil && *a.CRMUserID == observed.Snapshot.ResponsibleUserID {
				selected = a.EmployeeID
				kind = "keep"
			}
		}
	}
	if e = q.EnsureDistributionGroupClaim(ctx, db.EnsureDistributionGroupClaimParams{CompanyID: row.CompanyID, GroupID: row.GroupID}); e != nil {
		return true, e
	}
	claim, e := q.LockDistributionGroupClaim(ctx, db.LockDistributionGroupClaimParams{CompanyID: row.CompanyID, GroupID: row.GroupID})
	if e != nil {
		return true, e
	}
	if claim.QueueID.Valid && claim.QueueID.UUID != row.ID {
		if e = s.queueState(ctx, q, row, "waiting", "group_operation_unfinished", s.now().Add(5*time.Second), false); e != nil {
			return true, e
		}
		return true, tx.Commit(ctx)
	}
	if selected == uuid.Nil {
		for _, id := range nextMemberOrder(g.MemberIds, claim.CursorOrder, int(claim.CursorNext)) {
			for _, a := range available {
				if a.EmployeeID == id && a.Available {
					selected = id
					break
				}
			}
			if selected != uuid.Nil {
				break
			}
		}
	}
	if selected == uuid.Nil {
		next := s.now().Add(time.Minute)
		var shift *time.Time
		for _, a := range available {
			if a.NextShift != nil && (shift == nil || a.NextShift.Before(*shift)) {
				shift = a.NextShift
			}
		}
		if shift != nil {
			next = *shift
		}
		state, reason := "waiting", "no_available_members"
		if shift == nil {
			state, reason = "requires_configuration", "no_valid_members_within_horizon"
		}
		if e = s.queueState(ctx, q, row, state, reason, next, false); e != nil {
			return true, e
		}
		return true, tx.Commit(ctx)
	}
	n, e := q.ReserveDistributionLeadClaim(ctx, db.ReserveDistributionLeadClaimParams{AccountID: row.AccountID, LeadID: row.LeadID, QueueID: row.ID})
	if e != nil {
		return true, e
	}
	if n == 0 {
		old, e := q.GetDistributionLeadClaim(ctx, db.GetDistributionLeadClaimParams{AccountID: row.AccountID, LeadID: row.LeadID})
		if e != nil {
			return true, e
		}
		if old.QueueID != row.ID {
			if e = s.queueState(ctx, q, row, "waiting", "lead_operation_unfinished", s.now().Add(5*time.Second), false); e != nil {
				return true, e
			}
			return true, tx.Commit(ctx)
		}
	}
	if e = q.ReserveDistributionGroupClaim(ctx, db.ReserveDistributionGroupClaimParams{CompanyID: row.CompanyID, GroupID: row.GroupID, QueueID: nullID(row.ID)}); e != nil {
		return true, e
	}
	if _, e = q.LockDistributionQueueLease(ctx, db.LockDistributionQueueLeaseParams{ID: row.ID, LeaseToken: row.LeaseToken}); e != nil {
		return true, e
	}
	claim.Revision++
	target := ""
	until := s.now().Add(10 * time.Minute)
	for _, a := range available {
		if a.EmployeeID == selected {
			target = *a.CRMUserID
			if a.Until != nil && a.Until.Before(until) {
				until = *a.Until
			}
		}
	}
	now := s.now().UTC()
	a := corebridge.Assignment{SchemaVersion: 1, MessageID: uuid.New(), Scope: bindingScope(b), EventID: entry.EntryEventID, SourceOccurredAt: entry.CreatedAt, ReceivedAt: row.CreatedAt, EmittedAt: now, CorrelationID: uuid.New(), CausationID: entry.EntryEventID, Command: corebridge.AssignmentCommand{OperationID: uuid.New(), EpisodeID: row.EntryID, DecisionID: uuid.New(), RuleID: r.ID, GroupID: g.ID, RuleRevision: r.Revision, AvailabilityRevision: v.Revision, ClaimRevision: claim.Revision, DecisionKind: kind, TargetResponsibleUserID: target, ExpectedSnapshot: *observed.Snapshot, Actor: corebridge.AssignmentActor{Kind: "system"}, ValidUntil: until}}
	raw, _ := json.Marshal(a)
	e = q.SaveDistributionQueueDecision(ctx, db.SaveDistributionQueueDecisionParams{ID: row.ID, OperationID: nullID(a.Command.OperationID), DecisionID: nullID(a.Command.DecisionID), Command: raw, IdempotencyKey: nullID(uuid.New()), CancelKey: nullID(uuid.New()), ReconcileKey: nullID(uuid.New()), AvailabilityHash: hash, ClaimRevision: claim.Revision, PlannedEmployeeID: nullID(selected), SelectionOrder: g.MemberIds})
	if e != nil {
		return true, e
	}
	if e = q.AddDistributionQueueHistory(ctx, db.AddDistributionQueueHistoryParams{QueueID: row.ID, State: "dispatching", Reason: "decision_ready", Payload: raw}); e != nil {
		return true, e
	}
	if e = tx.Commit(ctx); e != nil {
		return true, e
	}
	row, e = db.New(s.pool).LockDistributionQueueLease(ctx, db.LockDistributionQueueLeaseParams{ID: row.ID, LeaseToken: row.LeaseToken})
	if e != nil {
		return true, e
	}
	return true, s.resumeDistributionAssignment(ctx, core, row)
}
func (s *Service) resumeDistributionAssignment(ctx context.Context, core DistributionAssignmentCore, row db.DistributionQueue) error {
	var a corebridge.Assignment
	if json.Unmarshal(row.Command, &a) != nil {
		return errors.New("invalid frozen assignment")
	}
	op, e := core.Operation(ctx, a.Scope, a.Command.OperationID)
	var remote *corebridge.Error
	if errors.As(e, &remote) && remote.Status == 404 {
		_, e = core.Assign(ctx, a, row.IdempotencyKey.UUID)
		if e == nil {
			op, e = core.Operation(ctx, a.Scope, a.Command.OperationID)
		}
	}
	if e != nil {
		return s.queueState(ctx, db.New(s.pool), row, "uncertain", "operation_unavailable", s.now().Add(15*time.Second), row.CancelRequested)
	}
	if !validOperation(op) || !operationMatchesAssignment(op, a) {
		return errors.New("invalid Core operation")
	}
	deny, e := s.ValidateDistributionDecision(ctx, DistributionDecisionValidationInput{Scope: a.Scope, Actor: DistributionDecisionActor{Kind: "system"}, OperationID: a.Command.OperationID, DecisionID: a.Command.DecisionID, EpisodeID: a.Command.EpisodeID, RuleID: a.Command.RuleID, GroupID: a.Command.GroupID, RuleRevision: a.Command.RuleRevision, AvailabilityRevision: a.Command.AvailabilityRevision, ClaimRevision: a.Command.ClaimRevision, WorkerFence: 1, TargetEmployeeID: row.PlannedEmployeeID.UUID, TargetResponsibleUserID: a.Command.TargetResponsibleUserID, LeadID: row.LeadID, DecisionKind: a.Command.DecisionKind, ValidUntil: a.Command.ValidUntil})
	if e != nil {
		return e
	}
	if !deny.Allowed && !op.ResolutionEvidence.GuardReleasable && op.CancelRequestedAt == nil {
		cancelled, ce := s.controlRuntimeOperation(ctx, core, row, a.Scope, op, "cancel")
		if ce == nil {
			op = cancelled
		} else {
			return s.queueState(ctx, db.New(s.pool), row, "uncertain", "cancel_pending", s.now().Add(15*time.Second), true)
		}
	}
	if !op.ResolutionEvidence.GuardReleasable && (op.State == "confirming" || op.State == "outcome_unknown") {
		reconciled, re := s.controlRuntimeOperation(ctx, core, row, a.Scope, op, "reconcile")
		if re == nil {
			op = reconciled
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.EnsureDistributionAvailabilityVersion(ctx, row.CompanyID); e != nil {
		return e
	}
	if _, e = q.LockDistributionAvailabilityVersion(ctx, row.CompanyID); e != nil {
		return e
	}
	row, e = q.LockDistributionQueueLease(ctx, db.LockDistributionQueueLeaseParams{ID: row.ID, LeaseToken: row.LeaseToken})
	if e != nil {
		return e
	}
	if row.Settled {
		return tx.Commit(ctx)
	}
	if !validOperation(op) || !operationMatchesAssignment(op, a) {
		return errors.New("operation identity mismatch")
	}
	if e = s.registerRuntimeMirror(ctx, q, row, op); e != nil {
		return e
	}
	if e = s.applyOperationResult(ctx, q, op); e != nil {
		return e
	}
	if !op.ResolutionEvidence.GuardReleasable {
		if e = s.queueState(ctx, q, row, "uncertain", "operation_unfinished", s.now().Add(5*time.Second), op.CancelRequestedAt != nil); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	return tx.Commit(ctx)
}

func (s *Service) settleRuntimeOperation(ctx context.Context, q *db.Queries, row db.DistributionQueue, op corebridge.Operation) error {
	var a corebridge.Assignment
	if json.Unmarshal(row.Command, &a) != nil || !validOperation(op) || !operationMatchesAssignment(op, a) {
		return errors.New("invalid runtime result")
	}
	if row.Settled || !op.ResolutionEvidence.GuardReleasable {
		return nil
	}
	state, reason := "failed", op.State
	advance := false
	if (op.State == "succeeded" || op.State == "no_change") && op.Outcome != nil && op.ConfirmedSnapshot != nil && op.ConfirmedSnapshot.ResponsibleUserID == a.Command.TargetResponsibleUserID {
		if *op.Outcome == "kept" && a.Command.DecisionKind == "keep" {
			state, reason = "kept", "current_owner_kept"
		} else if a.Command.DecisionKind == "assign" && (*op.Outcome == "assigned" || *op.Outcome == "already_target") {
			state, reason, advance = "confirmed", "assignment_confirmed", true
		}
	} else if op.State == "cancelled" {
		state = "cancelled"
	}
	claim, e := q.LockDistributionGroupClaim(ctx, db.LockDistributionGroupClaimParams{CompanyID: row.CompanyID, GroupID: row.GroupID})
	if e != nil {
		return e
	}
	order, next := claim.CursorOrder, claim.CursorNext
	if advance {
		order = row.SelectionOrder
		for i, id := range order {
			if id == row.PlannedEmployeeID.UUID {
				next = int32((i + 1) % len(order))
				break
			}
		}
	}
	entry, ee := q.GetDistributionObservedEntry(ctx, row.EntryID)
	if ee != nil {
		return ee
	}
	retry := state == "cancelled"
	if op.Outcome != nil && *op.Outcome == "source_changed" {
		retry = true
	}
	if op.Error != nil {
		switch op.Error.Code {
		case "decision_expired", "recipient_unavailable", "policy_unavailable":
			retry = true
		}
	}
	retry = retry && op.ExternalEffectState == "no_attempt" && entry.State == "checking"
	if retry {
		state, reason = "waiting", "decision_recalculation"
	}
	if retry {
		if e = q.ResetDistributionQueueDecision(ctx, db.ResetDistributionQueueDecisionParams{ID: row.ID, Reason: reason}); e != nil {
			return e
		}
	} else {
		if _, e = q.SettleDistributionQueue(ctx, db.SettleDistributionQueueParams{ID: row.ID, State: state, Reason: reason}); e != nil {
			return e
		}
	}
	if e = q.ReleaseDistributionLeadClaim(ctx, row.ID); e != nil {
		return e
	}
	if e = q.ReleaseDistributionGroupClaim(ctx, db.ReleaseDistributionGroupClaimParams{CompanyID: row.CompanyID, GroupID: row.GroupID, CursorOrder: order, CursorNext: next, QueueID: nullID(row.ID)}); e != nil {
		return e
	}
	raw, _ := json.Marshal(op)
	if e = q.AddDistributionQueueHistory(ctx, db.AddDistributionQueueHistoryParams{QueueID: row.ID, State: state, Reason: reason, Payload: raw}); e != nil {
		return e
	}
	return nil
}
func (s *Service) registerRuntimeMirror(ctx context.Context, q *db.Queries, row db.DistributionQueue, op corebridge.Operation) error {
	if !validOperation(op) {
		return errors.New("invalid Core operation")
	}
	old, e := q.GetDistributionOperationMirror(ctx, op.OperationID)
	if e == nil {
		var registered corebridge.Operation
		if json.Unmarshal(old.RegistrationPayload, &registered) != nil || !bytes.Equal(operationIdentity(registered), operationIdentity(op)) {
			return conflict("Операция имеет другую идентичность")
		}
		return nil
	}
	if !isNoRows(e) {
		return e
	}
	raw, _ := json.Marshal(op)
	if e = q.RegisterDistributionOperationMirror(ctx, db.RegisterDistributionOperationMirrorParams{OperationID: op.OperationID, CompanyID: row.CompanyID, BindingID: op.Scope.BindingID, BindingRevision: op.Scope.BindingRevision, InstallationID: op.Scope.InstallationID, IntegrationID: op.Scope.IntegrationID, AccountID: op.Scope.AccountID, LeadID: op.LeadID, EpisodeID: op.EpisodeID, DecisionID: op.DecisionID, RuleID: op.RuleID, GroupID: op.GroupID, TargetResponsibleID: op.TargetResponsibleUserID, RegistrationPayload: raw, RegisteredBy: row.ID}); e != nil {
		return e
	}
	return q.WakeDistributionResultInbox(ctx, op.OperationID.String())
}
