package application

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) validateRegisteredDistributionDecision(ctx context.Context, in DistributionDecisionValidationInput, deny DistributionDecisionValidation, b db.DistributionBinding) (DistributionDecisionValidation, error) {
	if _, e := db.New(s.pool).GetDistributionQueueByOperation(ctx, nullID(in.OperationID)); isNoRows(e) {
		return deny, nil
	} else if e != nil {
		return deny, e
	}
	refs, e := s.readDistributionReferences(ctx, in.Scope)
	if e != nil {
		deny.Reason = "crm_users_unavailable"
		return deny, nil
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return deny, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.EnsureDistributionAvailabilityVersion(ctx, in.CompanyID); e != nil {
		return deny, e
	}
	version, e := q.LockDistributionAvailabilityVersion(ctx, in.CompanyID)
	if e != nil {
		return deny, e
	}
	row, e := q.GetDistributionQueueByOperation(ctx, nullID(in.OperationID))
	if isNoRows(e) {
		return deny, nil
	}
	if e != nil {
		return deny, e
	}
	row, e = q.LockDistributionQueue(ctx, row.ID)
	if e != nil {
		return deny, e
	}
	var a corebridge.Assignment
	if json.Unmarshal(row.Command, &a) != nil {
		return deny, nil
	}
	c := a.Command
	if row.Settled || row.CancelRequested || a.Scope != in.Scope || in.Actor.Kind != "system" || c.OperationID != in.OperationID || c.DecisionID != in.DecisionID || c.EpisodeID != in.EpisodeID || c.RuleID != in.RuleID || c.GroupID != in.GroupID || c.RuleRevision != in.RuleRevision || c.AvailabilityRevision != in.AvailabilityRevision || c.ClaimRevision != in.ClaimRevision || row.PlannedEmployeeID.UUID != in.TargetEmployeeID || c.TargetResponsibleUserID != in.TargetResponsibleUserID || c.ExpectedSnapshot.LeadID != in.LeadID || c.DecisionKind != in.DecisionKind || !c.ValidUntil.Equal(in.ValidUntil) {
		deny.Reason = "decision_mismatch"
		return deny, nil
	}
	r, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: in.CompanyID, ID: in.RuleID})
	if e != nil {
		return deny, e
	}
	g, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: in.CompanyID, ID: in.GroupID})
	if e != nil {
		deny.Reason = "group_unavailable"
		return deny, nil
	}
	b, e = q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: in.CompanyID, ID: in.BindingID})
	if e != nil {
		return deny, e
	}
	company, ce := q.GetCompany(ctx, in.CompanyID)
	if ce != nil {
		return deny, ce
	}
	if company.Status != "active" {
		deny.Reason = "company_unavailable"
		return deny, nil
	}
	if r.ExecutionMode != "live" || r.ExecutionEpoch != row.ExecutionEpoch {
		deny.Reason = "execution_mode_changed"
		return deny, nil
	}
	if b.State != "active" || bindingScope(b) != in.Scope || b.MappingAckRevision != b.MappingRevision || !r.Active || !g.Active || r.Revision != c.RuleRevision || version.Revision != c.AvailabilityRevision {
		deny.Reason = "decision_stale"
		return deny, nil
	}
	claim, e := q.LockDistributionGroupClaim(ctx, db.LockDistributionGroupClaimParams{CompanyID: in.CompanyID, GroupID: in.GroupID})
	if e != nil {
		return deny, e
	}
	lead, e := q.GetDistributionLeadClaim(ctx, db.GetDistributionLeadClaimParams{AccountID: in.AccountID, LeadID: in.LeadID})
	if e != nil {
		deny.Reason = "claim_unavailable"
		return deny, nil
	}
	if !claim.QueueID.Valid || claim.QueueID.UUID != row.ID || lead.QueueID != row.ID || claim.Revision != in.ClaimRevision {
		deny.Reason = "claim_stale"
		return deny, nil
	}
	entry, e := q.GetDistributionObservedEntry(ctx, row.EntryID)
	if e != nil {
		return deny, e
	}
	head, e := q.LockDistributionLeadHead(ctx, db.LockDistributionLeadHeadParams{AccountID: in.AccountID, LeadID: in.LeadID})
	if e != nil {
		return deny, e
	}
	if !ruleEntrySnapshotMatches(r, entry, &c.ExpectedSnapshot) || entry.State != "checking" || !head.CurrentEntryID.Valid || head.CurrentEntryID.UUID != entry.ID || head.BindingID != in.BindingID || head.Deleted || head.Absent {
		deny.Reason = "episode_cancelled"
		return deny, nil
	}
	if !row.WaitingDeadlineAt.After(s.now()) {
		deny.Reason = "waiting_expired"
		return deny, nil
	}
	settings, e := s.distributionBindingSettings(ctx, q, b)
	if e != nil {
		deny.Reason = "timezone_required"
		return deny, nil
	}
	available, hash, e := s.distributionAvailability(ctx, q, b, g, settings.Timezone, s.now(), &refs)
	if e != nil {
		return deny, e
	}
	if !bytes.Equal(hash, row.AvailabilityHash) {
		deny.Reason = "availability_stale"
		return deny, nil
	}
	until := s.now().Add(5 * time.Second)
	if in.ValidUntil.Before(until) {
		until = in.ValidUntil
	}
	found := false
	for _, member := range available {
		if member.EmployeeID == in.TargetEmployeeID && member.Available && member.CRMUserID != nil && *member.CRMUserID == in.TargetResponsibleUserID {
			found = true
			if member.Until != nil && member.Until.Before(until) {
				until = *member.Until
			}
		}
	}
	if !found || !until.After(s.now()) {
		deny.Reason = "recipient_unavailable"
		return deny, nil
	}
	if e = tx.Commit(ctx); e != nil {
		return deny, e
	}
	deny.Allowed = true
	deny.Reason = "validated"
	deny.ValidUntil = until
	return deny, nil
}
