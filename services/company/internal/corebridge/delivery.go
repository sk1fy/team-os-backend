package corebridge

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type LeadSnapshot struct {
	LeadID            string     `json:"leadId"`
	PipelineID        string     `json:"pipelineId"`
	StatusID          string     `json:"statusId"`
	ResponsibleUserID string     `json:"responsibleUserId"`
	SourceUpdatedAt   *time.Time `json:"sourceUpdatedAt"`
	ObservedAt        time.Time  `json:"observedAt"`
}
type LeadObservation struct {
	Absent              bool          `json:"absent"`
	AbsenceReason       *string       `json:"absenceReason"`
	Scope               Scope         `json:"scope"`
	LeadID              string        `json:"leadId"`
	Snapshot            *LeadSnapshot `json:"snapshot"`
	Deleted             bool          `json:"deleted"`
	ObservedAt          time.Time     `json:"observedAt"`
	ObservationRevision int64         `json:"observationRevision"`
}
type OperationEvidence struct {
	Kind            string    `json:"kind"`
	ObservedAt      time.Time `json:"observedAt"`
	GuardReleasable bool      `json:"guardReleasable"`
	Explanation     string    `json:"explanation"`
}
type OperationError struct {
	RetryAfterSeconds *int   `json:"retryAfterSeconds"`
	Code              string `json:"code"`
	Message           string `json:"message"`
	Retryable         bool   `json:"retryable"`
	Terminal          bool   `json:"terminal"`
}
type Operation struct {
	LeadID                  string            `json:"leadId"`
	OperationID             uuid.UUID         `json:"operationId"`
	Scope                   Scope             `json:"scope"`
	EpisodeID               uuid.UUID         `json:"episodeId"`
	DecisionID              uuid.UUID         `json:"decisionId"`
	RuleID                  uuid.UUID         `json:"ruleId"`
	GroupID                 uuid.UUID         `json:"groupId"`
	RuleRevision            int64             `json:"ruleRevision"`
	AvailabilityRevision    int64             `json:"availabilityRevision"`
	ClaimRevision           int64             `json:"claimRevision"`
	EventID                 uuid.UUID         `json:"eventId"`
	CorrelationID           uuid.UUID         `json:"correlationId"`
	TargetResponsibleUserID string            `json:"targetResponsibleUserId"`
	State                   string            `json:"state"`
	ExternalEffectState     string            `json:"externalEffectState"`
	Outcome                 *string           `json:"outcome"`
	ResultVersion           int64             `json:"resultVersion"`
	ConfirmedSnapshot       *LeadSnapshot     `json:"confirmedSnapshot"`
	ResolutionEvidence      OperationEvidence `json:"resolutionEvidence"`
	Error                   *OperationError   `json:"error"`
	AcceptedAt              time.Time         `json:"acceptedAt"`
	UpdatedAt               time.Time         `json:"updatedAt"`
	CancelRequestedAt       *time.Time        `json:"cancelRequestedAt"`
}

func (c *Client) ReadLead(ctx context.Context, s Scope, id string) (LeadObservation, error) {
	var out LeadObservation
	err := c.call(ctx, "GET", "/internal/v1/distribution/bindings/"+s.BindingID.String()+"/leads/"+id+"?bindingRevision="+strconv.FormatInt(s.BindingRevision, 10), s, nil, &out)
	if err == nil && (out.Scope != s || out.LeadID != id || out.ObservationRevision <= 0 || out.ObservationRevision > 9007199254740991 || out.ObservedAt.IsZero() || (out.Deleted && out.Absent) || (!out.Absent && out.AbsenceReason != nil) || (out.Deleted || out.Absent) != (out.Snapshot == nil) || (out.Absent && (out.AbsenceReason == nil || *out.AbsenceReason != "not_found_or_deleted"))) {
		err = errors.New("Core lead observation scope or shape mismatch")
	}
	return out, err
}
func (c *Client) Operation(ctx context.Context, s Scope, id uuid.UUID) (Operation, error) {
	var out Operation
	err := c.call(ctx, "GET", "/internal/v1/distribution/operations/"+id.String(), s, nil, &out)
	if err == nil && (out.OperationID != id || out.Scope != s || out.ResultVersion <= 0 || out.ResultVersion > 9007199254740991 || out.CorrelationID == uuid.Nil || out.EventID == uuid.Nil) {
		err = errors.New("Core operation scope or version mismatch")
	}
	return out, err
}
