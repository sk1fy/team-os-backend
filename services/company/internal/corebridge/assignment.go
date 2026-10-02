package corebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type AssignmentCommand struct {
	OperationID             uuid.UUID       `json:"operationId"`
	EpisodeID               uuid.UUID       `json:"episodeId"`
	DecisionID              uuid.UUID       `json:"decisionId"`
	RuleID                  uuid.UUID       `json:"ruleId"`
	GroupID                 uuid.UUID       `json:"groupId"`
	RuleRevision            int64           `json:"ruleRevision"`
	AvailabilityRevision    int64           `json:"availabilityRevision"`
	ClaimRevision           int64           `json:"claimRevision"`
	DecisionKind            string          `json:"decisionKind"`
	TargetResponsibleUserID string          `json:"targetResponsibleUserId"`
	ExpectedSnapshot        LeadSnapshot    `json:"expectedSnapshot"`
	Actor                   AssignmentActor `json:"actor"`
	ValidUntil              time.Time       `json:"validUntil"`
}
type AssignmentActor struct {
	Kind string `json:"kind"`
}
type Assignment struct {
	SchemaVersion    int               `json:"schemaVersion"`
	MessageID        uuid.UUID         `json:"messageId"`
	Scope            Scope             `json:"scope"`
	EventID          uuid.UUID         `json:"eventId"`
	SourceEventID    *string           `json:"sourceEventId"`
	SourceOccurredAt time.Time         `json:"sourceOccurredAt"`
	ReceivedAt       time.Time         `json:"receivedAt"`
	EmittedAt        time.Time         `json:"emittedAt"`
	CorrelationID    uuid.UUID         `json:"correlationId"`
	CausationID      uuid.UUID         `json:"causationId"`
	Command          AssignmentCommand `json:"command"`
}
type AssignmentReceipt struct {
	OperationID   uuid.UUID `json:"operationId"`
	AcceptedAt    time.Time `json:"acceptedAt"`
	State         string    `json:"state"`
	ResultVersion int64     `json:"resultVersion"`
}

func (c *Client) Assign(ctx context.Context, a Assignment, key uuid.UUID) (AssignmentReceipt, error) {
	var out AssignmentReceipt
	e := c.assignmentCall(ctx, "/internal/v1/distribution/assignments", a.Scope, key, a, 202, &out)
	if e == nil && (out.OperationID != a.Command.OperationID || out.AcceptedAt.IsZero() || out.ResultVersion <= 0) {
		e = errors.New("Некорректное подтверждение Core")
	}
	return out, e
}
func (c *Client) CancelAssignment(ctx context.Context, s Scope, id, key uuid.UUID, version int64) (Operation, error) {
	var out Operation
	e := c.assignmentCall(ctx, "/internal/v1/distribution/operations/"+id.String()+"/cancel", s, key, struct {
		ExpectedResultVersion int64           `json:"expectedResultVersion"`
		Actor                 AssignmentActor `json:"actor"`
		Reason                string          `json:"reason"`
	}{version, AssignmentActor{"system"}, "business_preconditions_changed"}, 200, &out)
	return out, e
}
func (c *Client) ReconcileAssignment(ctx context.Context, s Scope, id, key uuid.UUID, version int64) (Operation, error) {
	var out Operation
	e := c.assignmentCall(ctx, "/internal/v1/distribution/operations/"+id.String()+"/reconcile", s, key, struct {
		ExpectedResultVersion int64           `json:"expectedResultVersion"`
		Actor                 AssignmentActor `json:"actor"`
		Reason                string          `json:"reason"`
	}{version, AssignmentActor{"system"}, "business_reconciliation"}, 200, &out)
	return out, e
}
func (c *Client) ControlAssignment(ctx context.Context, s Scope, id, key uuid.UUID, action string, payload json.RawMessage) (Operation, error) {
	var out Operation
	if action != "cancel" && action != "reconcile" {
		return out, errors.New("Некорректное действие Core")
	}
	e := c.assignmentCall(ctx, "/internal/v1/distribution/operations/"+id.String()+"/"+action, s, key, payload, 200, &out)
	return out, e
}
func (c *Client) assignmentCall(ctx context.Context, path string, s Scope, key uuid.UUID, in any, status int, out any) error {
	if c == nil {
		return errors.New("Core не настроен")
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", c.base+path, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	Sign(r, c.keyID, c.secret, s, raw, c.now())
	r.Header.Set("Idempotency-Key", key.String())
	resp, e := c.http.Do(r)
	if e != nil {
		return errors.New("Core недоступен")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != status {
		return &Error{resp.StatusCode}
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if e != nil || len(b) > 4<<20 {
		return errors.New("Некорректный ответ Core")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return errors.New("Некорректный ответ Core")
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return errors.New("Лишние данные Core")
	}
	return nil
}
