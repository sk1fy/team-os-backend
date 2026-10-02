package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type DistributionDecisionActor struct {
	Kind         string     `json:"kind"`
	TeamOSUserID *uuid.UUID `json:"teamosUserId,omitempty"`
	CRMUserID    *string    `json:"crmUserId,omitempty"`
	OperatorID   *uuid.UUID `json:"operatorId,omitempty"`
}
type DistributionDecisionValidationInput struct {
	corebridge.Scope
	Actor                   DistributionDecisionActor `json:"actor"`
	OperationID             uuid.UUID                 `json:"operationId"`
	DecisionID              uuid.UUID                 `json:"decisionId"`
	EpisodeID               uuid.UUID                 `json:"episodeId"`
	RuleID                  uuid.UUID                 `json:"ruleId"`
	GroupID                 uuid.UUID                 `json:"groupId"`
	RuleRevision            int64                     `json:"ruleRevision"`
	AvailabilityRevision    int64                     `json:"availabilityRevision"`
	ClaimRevision           int64                     `json:"claimRevision"`
	WorkerFence             int64                     `json:"workerFence"`
	TargetEmployeeID        uuid.UUID                 `json:"targetEmployeeId"`
	TargetResponsibleUserID string                    `json:"targetResponsibleUserId"`
	LeadID                  string                    `json:"leadId"`
	DecisionKind            string                    `json:"decisionKind"`
	ValidUntil              time.Time                 `json:"validUntil"`
}
type DistributionDecisionValidation struct {
	Allowed     bool      `json:"allowed"`
	OperationID uuid.UUID `json:"operationId"`
	DecisionID  uuid.UUID `json:"decisionId"`
	WorkerFence int64     `json:"workerFence"`
	ValidUntil  time.Time `json:"validUntil"`
	Reason      string    `json:"reason"`
}

// ValidateDistributionDecision is a real authenticated pre-effect boundary.
// Stage 04 has no TeamOS rule/episode/decision registry yet (stage 06), so it
// must never manufacture an allow-token from binding/employee checks alone.
func (s *Service) ValidateDistributionDecision(ctx context.Context, in DistributionDecisionValidationInput) (DistributionDecisionValidation, error) {
	deny := DistributionDecisionValidation{OperationID: in.OperationID, DecisionID: in.DecisionID, WorkerFence: in.WorkerFence, ValidUntil: s.now(), Reason: "decision_not_ready"}
	if !in.ValidUntil.After(s.now()) {
		deny.Reason = "decision_expired"
		return deny, nil
	}
	if in.ValidUntil.After(s.now().Add(15 * time.Minute)) {
		deny.Reason = "invalid_decision"
		return deny, nil
	}
	ids := []uuid.UUID{in.OperationID, in.DecisionID, in.EpisodeID, in.RuleID, in.GroupID, in.TargetEmployeeID}
	for _, id := range ids {
		if id == uuid.Nil {
			deny.Reason = "invalid_decision"
			return deny, nil
		}
	}
	if !validCRMID(in.LeadID) || !validCRMID(in.TargetResponsibleUserID) || (in.DecisionKind != "assign" && in.DecisionKind != "keep") {
		deny.Reason = "invalid_decision"
		return deny, nil
	}
	for _, revision := range []int64{in.BindingRevision, in.RuleRevision, in.AvailabilityRevision, in.ClaimRevision, in.WorkerFence} {
		if revision <= 0 || revision > 9007199254740991 {
			deny.Reason = "invalid_decision"
			return deny, nil
		}
	}

	if in.Actor.OperatorID != nil || (in.Actor.Kind != "system" && in.Actor.Kind != "user") || (in.Actor.Kind == "system" && (in.Actor.TeamOSUserID != nil || in.Actor.CRMUserID != nil)) || (in.Actor.Kind == "user" && (in.Actor.TeamOSUserID == nil || *in.Actor.TeamOSUserID == uuid.Nil || in.Actor.CRMUserID == nil || !validCRMID(*in.Actor.CRMUserID))) {
		deny.Reason = "invalid_decision"
		return deny, nil
	}
	company, e := db.New(s.pool).GetCompany(ctx, in.CompanyID)
	if e != nil {
		if isNoRows(e) {
			deny.Reason = "binding_unavailable"
			return deny, nil
		}
		return deny, internal("Не удалось проверить компанию", e)
	}
	if company.Status != "active" {
		deny.Reason = "company_unavailable"
		return deny, nil
	}
	b, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: in.CompanyID, ID: in.BindingID})
	if e != nil {
		if isNoRows(e) {
			deny.Reason = "binding_unavailable"
			return deny, nil
		}
		return deny, internal("Не удалось проверить связь", e)
	}
	if b.State != "active" || bindingScope(b) != in.Scope || b.MappingRevision != b.MappingAckRevision {
		deny.Reason = "binding_unavailable"
		return deny, nil
	}

	if in.Actor.Kind == "user" {
		if _, err := s.distributionActor(ctx, Actor{CompanyID: in.CompanyID, UserID: *in.Actor.TeamOSUserID}, false); err != nil {
			deny.Reason = "actor_unavailable"
			return deny, nil
		}
		actorMapping, err := db.New(s.pool).GetDistributionMappingByCRM(ctx, db.GetDistributionMappingByCRMParams{CompanyID: in.CompanyID, BindingID: in.BindingID, CrmUserID: *in.Actor.CRMUserID})
		if err != nil {
			if !isNoRows(err) {
				return deny, internal("Не удалось проверить пользователя решения", err)
			}
			deny.Reason = "actor_unavailable"
			return deny, nil
		}
		if actorMapping.State != "verified" || !actorMapping.UserID.Valid || actorMapping.UserID.UUID != *in.Actor.TeamOSUserID || actorMapping.UserStatus != "active" {
			deny.Reason = "actor_unavailable"
			return deny, nil
		}
	}
	mapping, e := db.New(s.pool).GetDistributionMappingByCRM(ctx, db.GetDistributionMappingByCRMParams{CompanyID: in.CompanyID, BindingID: in.BindingID, CrmUserID: in.TargetResponsibleUserID})
	if e != nil {
		if isNoRows(e) {
			deny.Reason = "mapping_unavailable"
			return deny, nil
		}
		return deny, internal("Не удалось проверить сотрудника", e)
	}
	if mapping.State != "verified" || !mapping.UserID.Valid || mapping.UserID.UUID != in.TargetEmployeeID || mapping.UserStatus != "active" {
		deny.Reason = "mapping_unavailable"
		return deny, nil
	}
	// Binding and mapping are necessary, never sufficient. No business decision
	// store currently proves ownership/revisions/schedule/cancellation/episode.
	return deny, nil
}
