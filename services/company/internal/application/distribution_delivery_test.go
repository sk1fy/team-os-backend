package application

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
)

func validDeliveryOperation() corebridge.Operation {
	now := time.Now().UTC()
	outcome := "assigned"
	return corebridge.Operation{LeadID: "10", Scope: corebridge.Scope{CompanyID: uuid.New(), BindingID: uuid.New(), InstallationID: uuid.New(), IntegrationID: uuid.New(), BindingRevision: 1, AccountID: "123"}, OperationID: uuid.New(), EpisodeID: uuid.New(), DecisionID: uuid.New(), RuleID: uuid.New(), GroupID: uuid.New(), EventID: uuid.New(), CorrelationID: uuid.New(), RuleRevision: 1, AvailabilityRevision: 1, ClaimRevision: 1, TargetResponsibleUserID: "2", State: "succeeded", ExternalEffectState: "settled", Outcome: &outcome, ResultVersion: 2, ConfirmedSnapshot: &corebridge.LeadSnapshot{LeadID: "10", PipelineID: "20", StatusID: "30", ResponsibleUserID: "2", ObservedAt: now}, ResolutionEvidence: corebridge.OperationEvidence{Kind: "response_and_observation", ObservedAt: now, GuardReleasable: true}, AcceptedAt: now, UpdatedAt: now}
}
func TestDeliveryRejectsContradictoryCompletion(t *testing.T) {
	if !validOperation(validDeliveryOperation()) {
		t.Fatal("legitimate success rejected")
	}
	cases := map[string]func(*corebridge.Operation){
		"unknown effect":                func(o *corebridge.Operation) { o.ExternalEffectState = "unknown" },
		"observation proves authorship": func(o *corebridge.Operation) { o.ResolutionEvidence.Kind = "observed_state_only" },
		"missing evidence time":         func(o *corebridge.Operation) { o.ResolutionEvidence.ObservedAt = time.Time{} },
		"missing snapshot":              func(o *corebridge.Operation) { o.ConfirmedSnapshot = nil },
		"another lead":                  func(o *corebridge.Operation) { o.ConfirmedSnapshot.LeadID = "11" },
		"another owner":                 func(o *corebridge.Operation) { o.ConfirmedSnapshot.ResponsibleUserID = "3" },
		"inconsistent outcome":          func(o *corebridge.Operation) { s := "kept"; o.Outcome = &s },
		"false terminal":                func(o *corebridge.Operation) { o.ResolutionEvidence.GuardReleasable = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			o := validDeliveryOperation()
			mutate(&o)
			if validOperation(o) {
				t.Fatal("false completion accepted")
			}
		})
	}
}
