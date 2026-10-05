package application

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) frozenRuntimeControl(ctx context.Context, row db.DistributionQueue, operation uuid.UUID, action string, version int64) (db.DistributionControlRequest, error) {
	q := db.New(s.pool)
	old, e := q.GetDistributionControlRequest(ctx, db.GetDistributionControlRequestParams{OperationID: operation, Action: action, ExpectedResultVersion: version})
	if e == nil {
		if old.QueueID != row.ID {
			return old, conflict("Запрос управления принадлежит другой очереди")
		}
		return old, nil
	}
	if !isNoRows(e) {
		return old, e
	}
	base := row.ReconcileKey.UUID
	reason := "business_reconciliation"
	if action == "cancel" {
		base = row.CancelKey.UUID
		reason = "business_preconditions_changed"
	}
	key := uuid.NewSHA1(base, []byte(action+":"+strconv.FormatInt(version, 10)))
	raw, _ := json.Marshal(struct {
		ExpectedResultVersion int64                      `json:"expectedResultVersion"`
		Actor                 corebridge.AssignmentActor `json:"actor"`
		Reason                string                     `json:"reason"`
	}{version, corebridge.AssignmentActor{Kind: "system"}, reason})
	if e = q.CreateDistributionControlRequest(ctx, db.CreateDistributionControlRequestParams{Key: key, QueueID: row.ID, OperationID: operation, Action: action, ExpectedResultVersion: version, Payload: raw}); e != nil {
		return old, e
	}
	old, e = q.GetDistributionControlRequest(ctx, db.GetDistributionControlRequestParams{OperationID: operation, Action: action, ExpectedResultVersion: version})
	if e != nil {
		return old, e
	}
	var canonical any
	var expected any
	_ = json.Unmarshal(old.Payload, &canonical)
	_ = json.Unmarshal(raw, &expected)
	a, _ := json.Marshal(canonical)
	b, _ := json.Marshal(expected)
	if old.Key != key || old.QueueID != row.ID || !bytes.Equal(a, b) {
		return old, conflict("Запрос управления имеет другую идентичность")
	}
	return old, nil
}
func (s *Service) controlRuntimeOperation(ctx context.Context, core DistributionAssignmentCore, row db.DistributionQueue, scope corebridge.Scope, op corebridge.Operation, action string) (corebridge.Operation, error) {
	frozen, e := s.frozenRuntimeControl(ctx, row, op.OperationID, action, op.ResultVersion)
	if e != nil {
		return corebridge.Operation{}, e
	}
	return core.ControlAssignment(ctx, scope, op.OperationID, frozen.Key, action, json.RawMessage(frozen.Payload))
}
