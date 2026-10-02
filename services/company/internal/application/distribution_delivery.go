package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const distributionConsumer = "teamos-distribution-v1"

type DistributionDeliveryCore interface {
	ReadLead(context.Context, corebridge.Scope, string) (corebridge.LeadObservation, error)
	Operation(context.Context, corebridge.Scope, uuid.UUID) (corebridge.Operation, error)
}

func WithDistributionDeliveryCore(c DistributionDeliveryCore) ServiceOption {
	return func(s *Service) { s.deliveryCore = c }
}

type DistributionSourceEvidence struct {
	PipelineID        *string `json:"pipelineId"`
	StatusID          *string `json:"statusId"`
	OldPipelineID     *string `json:"oldPipelineId"`
	OldStatusID       *string `json:"oldStatusId"`
	ResponsibleUserID *string `json:"responsibleUserId"`
}
type DistributionCRMEvent struct {
	SourceEvidence      *DistributionSourceEvidence `json:"sourceEvidence"`
	Kind                string                      `json:"kind"`
	LeadID              string                      `json:"leadId"`
	EntryFingerprint    *string                     `json:"entryFingerprint"`
	Before              *corebridge.LeadSnapshot    `json:"before"`
	After               *corebridge.LeadSnapshot    `json:"after"`
	ObservationRevision int64                       `json:"observationRevision"`
}
type DistributionEventEnvelope struct {
	SchemaVersion    int                  `json:"schemaVersion"`
	MessageID        uuid.UUID            `json:"messageId"`
	Scope            corebridge.Scope     `json:"scope"`
	EventID          uuid.UUID            `json:"eventId"`
	SourceEventID    *string              `json:"sourceEventId"`
	SourceOccurredAt *time.Time           `json:"sourceOccurredAt"`
	ReceivedAt       time.Time            `json:"receivedAt"`
	EmittedAt        time.Time            `json:"emittedAt"`
	CorrelationID    uuid.UUID            `json:"correlationId"`
	CausationID      *uuid.UUID           `json:"causationId"`
	Event            DistributionCRMEvent `json:"event"`
}
type DistributionResultEnvelope struct {
	SchemaVersion    int                  `json:"schemaVersion"`
	MessageID        uuid.UUID            `json:"messageId"`
	Scope            corebridge.Scope     `json:"scope"`
	EventID          uuid.UUID            `json:"eventId"`
	SourceEventID    *string              `json:"sourceEventId"`
	SourceOccurredAt *time.Time           `json:"sourceOccurredAt"`
	ReceivedAt       time.Time            `json:"receivedAt"`
	EmittedAt        time.Time            `json:"emittedAt"`
	CorrelationID    uuid.UUID            `json:"correlationId"`
	CausationID      *uuid.UUID           `json:"causationId"`
	Result           corebridge.Operation `json:"result"`
}
type DistributionDeliveryReceipt struct {
	MessageID   uuid.UUID `json:"messageId"`
	ReceiptID   uuid.UUID `json:"receiptId"`
	Disposition string    `json:"disposition"`
	AcceptedAt  time.Time `json:"acceptedAt"`
}

func safeRevision(n int64) bool { return n > 0 && n <= 9007199254740991 }
func validSnapshot(p *corebridge.LeadSnapshot, id string) bool {
	return p == nil || (p.LeadID == id && validCRMID(p.LeadID) && validCRMID(p.PipelineID) && validCRMID(p.StatusID) && validCRMID(p.ResponsibleUserID) && !p.ObservedAt.IsZero())
}
func normalizeSnapshot(p *corebridge.LeadSnapshot) {
	if p != nil {
		p.ObservedAt = p.ObservedAt.UTC()
		if p.SourceUpdatedAt != nil {
			u := p.SourceUpdatedAt.UTC()
			p.SourceUpdatedAt = &u
		}
	}
}
func validSourceEvidence(e *DistributionSourceEvidence) bool {
	if e == nil {
		return true
	}
	for _, id := range []*string{e.PipelineID, e.StatusID, e.OldPipelineID, e.OldStatusID, e.ResponsibleUserID} {
		if id != nil && !validCRMID(*id) {
			return false
		}
	}
	return true
}
func validScope(s corebridge.Scope) bool {
	return s.CompanyID != uuid.Nil && s.BindingID != uuid.Nil && s.InstallationID != uuid.Nil && s.IntegrationID != uuid.Nil && safeRevision(s.BindingRevision) && validCRMID(s.AccountID)
}
func validOperation(op corebridge.Operation) bool {
	if !validScope(op.Scope) || !validCRMID(op.LeadID) || op.OperationID == uuid.Nil || op.EpisodeID == uuid.Nil || op.DecisionID == uuid.Nil || op.RuleID == uuid.Nil || op.GroupID == uuid.Nil || op.EventID == uuid.Nil || op.CorrelationID == uuid.Nil || !safeRevision(op.RuleRevision) || !safeRevision(op.AvailabilityRevision) || !safeRevision(op.ClaimRevision) || !safeRevision(op.ResultVersion) || !validCRMID(op.TargetResponsibleUserID) || op.AcceptedAt.IsZero() || op.UpdatedAt.IsZero() {
		return false
	}
	switch op.State {
	case "queued", "prechecking", "applying", "confirming", "outcome_unknown", "succeeded", "no_change", "rejected", "conflict", "cancelled":
	default:
		return false
	}
	switch op.ExternalEffectState {
	case "no_attempt", "in_flight", "unknown", "settled":
	default:
		return false
	}
	terminal := op.State == "succeeded" || op.State == "no_change" || op.State == "rejected" || op.State == "conflict" || op.State == "cancelled"
	if terminal != op.ResolutionEvidence.GuardReleasable || op.ResolutionEvidence.ObservedAt.IsZero() || (terminal && op.ExternalEffectState != "no_attempt" && op.ExternalEffectState != "settled") {
		return false
	}
	switch op.ResolutionEvidence.Kind {
	case "no_request_sent", "response_and_observation":
	case "observed_state_only":
		if terminal {
			return false
		}
	default:
		return false
	}
	outcome := ""
	if op.Outcome != nil {
		outcome = *op.Outcome
	}
	switch outcome {
	case "", "assigned", "kept", "already_target", "rejected", "conflict", "source_changed", "cancelled":
	default:
		return false
	}
	if terminal == (outcome == "") || !validSnapshot(op.ConfirmedSnapshot, op.LeadID) {
		return false
	}
	switch op.State {
	case "succeeded":
		return outcome == "assigned" && op.ExternalEffectState == "settled" && op.ResolutionEvidence.Kind == "response_and_observation" && op.ConfirmedSnapshot != nil && op.ConfirmedSnapshot.ResponsibleUserID == op.TargetResponsibleUserID
	case "no_change":
		return (outcome == "kept" || outcome == "already_target") && op.ExternalEffectState == "no_attempt" && op.ResolutionEvidence.Kind == "no_request_sent" && op.ConfirmedSnapshot != nil && op.ConfirmedSnapshot.ResponsibleUserID == op.TargetResponsibleUserID
	case "rejected":
		return outcome == "rejected" && op.ExternalEffectState == "no_attempt" && op.ResolutionEvidence.Kind == "no_request_sent"
	case "cancelled":
		return outcome == "cancelled" && op.ExternalEffectState == "no_attempt" && op.ResolutionEvidence.Kind == "no_request_sent"
	case "conflict":
		return (outcome == "source_changed" && op.ExternalEffectState == "no_attempt" && op.ResolutionEvidence.Kind == "no_request_sent") || (outcome == "conflict" && op.ExternalEffectState == "settled" && op.ResolutionEvidence.Kind == "response_and_observation" && op.ConfirmedSnapshot != nil)
	}
	return true
}

// Legacy stage04 pushes have no leadId. This validates shape only; the missing
// immutable identity can be resolved exclusively from a registered Core mirror.
func validResultPayload(op corebridge.Operation) bool {
	if op.LeadID != "" {
		return validOperation(op)
	}
	check := op
	check.LeadID = "1"
	if check.ConfirmedSnapshot != nil {
		check.LeadID = check.ConfirmedSnapshot.LeadID
	}
	return validOperation(check)
}
func normalizeOperation(op *corebridge.Operation) {
	op.AcceptedAt = op.AcceptedAt.UTC()
	op.UpdatedAt = op.UpdatedAt.UTC()
	op.ResolutionEvidence.ObservedAt = op.ResolutionEvidence.ObservedAt.UTC()
	normalizeSnapshot(op.ConfirmedSnapshot)
	if op.CancelRequestedAt != nil {
		u := op.CancelRequestedAt.UTC()
		op.CancelRequestedAt = &u
	}
}
func (s *Service) deliveryScope(ctx context.Context, q *db.Queries, scope corebridge.Scope) error {
	v, e := q.GetDistributionBindingVersion(ctx, db.GetDistributionBindingVersionParams{CompanyID: scope.CompanyID, BindingID: scope.BindingID, Revision: scope.BindingRevision})
	if e != nil {
		if isNoRows(e) {
			return conflict("Историческая связь не подтверждена")
		}
		return internal("Не удалось проверить историческую связь", e)
	}
	if v.InstallationID != scope.InstallationID || v.IntegrationID != scope.IntegrationID || v.AccountID != scope.AccountID {
		return forbidden("Scope доставки не совпадает с подтверждённой связью")
	}
	return nil
}
func (s *Service) ReceiveDistributionEvent(ctx context.Context, in DistributionEventEnvelope) (DistributionDeliveryReceipt, error) {
	if in.SchemaVersion != 1 || in.MessageID == uuid.Nil || in.EventID == uuid.Nil || in.CorrelationID == uuid.Nil || !validScope(in.Scope) || !validCRMID(in.Event.LeadID) || !safeRevision(in.Event.ObservationRevision) || in.ReceivedAt.IsZero() || in.EmittedAt.IsZero() || !validSourceEvidence(in.Event.SourceEvidence) || !validSnapshot(in.Event.Before, in.Event.LeadID) || !validSnapshot(in.Event.After, in.Event.LeadID) {
		return DistributionDeliveryReceipt{}, validation("Некорректное событие распределения")
	}
	switch in.Event.Kind {
	case "lead.created", "lead.status_changed", "lead.responsible_changed", "lead.deleted", "lead.snapshot_reconciled":
	default:
		return DistributionDeliveryReceipt{}, validation("Неизвестное событие")
	}
	in.ReceivedAt = in.ReceivedAt.UTC()
	in.EmittedAt = in.EmittedAt.UTC()
	if in.SourceOccurredAt != nil {
		u := in.SourceOccurredAt.UTC()
		in.SourceOccurredAt = &u
	}
	normalizeSnapshot(in.Event.Before)
	normalizeSnapshot(in.Event.After)
	raw, _ := json.Marshal(in)
	business, _ := json.Marshal(struct {
		Scope            corebridge.Scope
		Event            DistributionCRMEvent
		SourceOccurredAt *time.Time
	}{in.Scope, in.Event, in.SourceOccurredAt})
	return s.receiveDelivery(ctx, "event", in.MessageID, in.EventID, in.Scope, in.Event.LeadID, raw, business, nil)
}
func (s *Service) ReceiveDistributionResult(ctx context.Context, in DistributionResultEnvelope) (DistributionDeliveryReceipt, error) {
	if in.SchemaVersion != 1 || in.MessageID == uuid.Nil || in.EventID == uuid.Nil || !validResultPayload(in.Result) || in.Result.Scope != in.Scope || in.Result.EventID != in.EventID || in.Result.CorrelationID != in.CorrelationID || in.ReceivedAt.IsZero() || in.EmittedAt.IsZero() {
		return DistributionDeliveryReceipt{}, validation("Некорректный результат операции")
	}
	in.ReceivedAt = in.ReceivedAt.UTC()
	in.EmittedAt = in.EmittedAt.UTC()
	if in.SourceOccurredAt != nil {
		u := in.SourceOccurredAt.UTC()
		in.SourceOccurredAt = &u
	}
	normalizeOperation(&in.Result)
	raw, _ := json.Marshal(in)
	business, _ := json.Marshal(in.Result)
	lead := in.Result.LeadID
	return s.receiveDelivery(ctx, "result", in.MessageID, in.EventID, in.Scope, lead, raw, business, &in.Result)
}
func (s *Service) receiveDelivery(ctx context.Context, kind string, message, event uuid.UUID, scope corebridge.Scope, lead string, raw, business []byte, op *corebridge.Operation) (DistributionDeliveryReceipt, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return DistributionDeliveryReceipt{}, internal("Не удалось начать приём доставки", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = s.deliveryScope(ctx, q, scope); e != nil {
		return DistributionDeliveryReceipt{}, e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", distributionConsumer+":"+kind+":"+message.String()); e != nil {
		return DistributionDeliveryReceipt{}, internal("Не удалось заблокировать доставку", e)
	}
	hash := sha256.Sum256(raw)
	old, e := q.GetDistributionDelivery(ctx, db.GetDistributionDeliveryParams{ConsumerID: distributionConsumer, MessageKind: kind, MessageID: message})
	if e == nil {
		if !bytes.Equal(old.PayloadHash, hash[:]) {
			return DistributionDeliveryReceipt{}, conflict("Message ID повторён с другим payload")
		}
		if e = tx.Commit(ctx); e != nil {
			return DistributionDeliveryReceipt{}, internal("Не удалось подтвердить доставку", e)
		}
		return DistributionDeliveryReceipt{message, old.ReceiptID, "duplicate", old.AcceptedAt.UTC()}, nil
	}
	if !isNoRows(e) {
		return DistributionDeliveryReceipt{}, internal("Не удалось проверить доставку", e)
	}
	semantic := sha256.Sum256(business)
	if kind == "event" {
		if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", distributionConsumer+":event:"+event.String()); e != nil {
			return DistributionDeliveryReceipt{}, internal("Не удалось проверить бизнес-событие", e)
		}
		r, err := q.GetDistributionEventReceipt(ctx, db.GetDistributionEventReceiptParams{ConsumerID: distributionConsumer, EventID: event})
		if err == nil && !bytes.Equal(r.PayloadHash, semantic[:]) {
			return DistributionDeliveryReceipt{}, conflict("Event ID повторён с другим бизнес-содержимым")
		}
		if err != nil && !isNoRows(err) {
			return DistributionDeliveryReceipt{}, internal("Не удалось проверить событие", err)
		}
	}
	if op != nil {
		if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", op.OperationID.String()+":"+stringVersion(op.ResultVersion)); e != nil {
			return DistributionDeliveryReceipt{}, internal("Не удалось проверить версию результата", e)
		}
		mirror, mirrorErr := q.GetDistributionOperationMirror(ctx, op.OperationID)
		if mirrorErr == nil {
			var registered corebridge.Operation
			if json.Unmarshal(mirror.RegistrationPayload, &registered) != nil {
				return DistributionDeliveryReceipt{}, internal("Повреждена регистрация операции", nil)
			}
			if op.LeadID == "" {
				op.LeadID = registered.LeadID
			}
			if !validOperation(*op) || !bytes.Equal(operationIdentity(registered), operationIdentity(*op)) {
				return DistributionDeliveryReceipt{}, conflict("Результат не совпадает с зарегистрированной операцией")
			}
			canonical, _ := json.Marshal(op)
			semantic = sha256.Sum256(canonical)
		}
		if mirrorErr != nil && !isNoRows(mirrorErr) {
			return DistributionDeliveryReceipt{}, internal("Не удалось проверить операцию", mirrorErr)
		}
		version, verErr := q.GetDistributionMirrorVersion(ctx, db.GetDistributionMirrorVersionParams{OperationID: op.OperationID, ResultVersion: op.ResultVersion})
		if verErr == nil && !bytes.Equal(version.PayloadHash, semantic[:]) {
			return DistributionDeliveryReceipt{}, conflict("Версия результата конфликтует с сохранённой сверкой")
		}
		if verErr != nil && !isNoRows(verErr) {
			return DistributionDeliveryReceipt{}, internal("Не удалось проверить сверку", verErr)
		}
		rows, err := tx.Query(ctx, "SELECT payload->'result' FROM distribution_delivery_inbox WHERE message_kind='result' AND payload->'result'->>'operationId'=$1 AND payload->'result'->>'resultVersion'=$2", op.OperationID.String(), stringVersion(op.ResultVersion))
		if err != nil {
			return DistributionDeliveryReceipt{}, internal("Не удалось проверить результаты", err)
		}
		for rows.Next() {
			var stored []byte
			if err = rows.Scan(&stored); err != nil {
				rows.Close()
				return DistributionDeliveryReceipt{}, internal("Не удалось проверить результат", err)
			}
			var existing corebridge.Operation
			if json.Unmarshal(stored, &existing) != nil {
				rows.Close()
				return DistributionDeliveryReceipt{}, internal("Повреждён сохранённый результат", nil)
			}
			compare := *op
			if existing.LeadID == "" {
				existing.LeadID = compare.LeadID
			}
			if compare.LeadID == "" {
				compare.LeadID = existing.LeadID
			}
			normalizeOperation(&compare)
			expectedJSON, _ := json.Marshal(compare)
			expectedHash := sha256.Sum256(expectedJSON)
			normalizeOperation(&existing)
			canon, _ := json.Marshal(existing)
			h := sha256.Sum256(canon)
			if !bytes.Equal(h[:], expectedHash[:]) {
				rows.Close()
				return DistributionDeliveryReceipt{}, conflict("Версия результата повторена с другим содержимым")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return DistributionDeliveryReceipt{}, internal("Не удалось прочитать результаты", err)
		}
	}
	row, e := q.CreateDistributionDelivery(ctx, db.CreateDistributionDeliveryParams{ReceiptID: uuid.New(), ConsumerID: distributionConsumer, MessageKind: kind, MessageID: message, EventID: event, CompanyID: scope.CompanyID, BindingID: scope.BindingID, BindingRevision: scope.BindingRevision, InstallationID: scope.InstallationID, IntegrationID: scope.IntegrationID, AccountID: scope.AccountID, LeadID: lead, PayloadHash: hash[:], Payload: raw})
	if e != nil {
		return DistributionDeliveryReceipt{}, internal("Не удалось сохранить inbox", e)
	}
	if kind == "event" {
		_, err := q.GetDistributionEventReceipt(ctx, db.GetDistributionEventReceiptParams{ConsumerID: distributionConsumer, EventID: event})
		if isNoRows(err) {
			if e = q.CreateDistributionEventReceipt(ctx, db.CreateDistributionEventReceiptParams{ConsumerID: distributionConsumer, EventID: event, CompanyID: scope.CompanyID, PayloadHash: semantic[:], ReceiptID: row.ReceiptID}); e != nil {
				return DistributionDeliveryReceipt{}, internal("Не удалось сохранить identity события", e)
			}
		} else if err == nil {
			if _, e = tx.Exec(ctx, "UPDATE distribution_delivery_inbox SET state='ignored',error_code='duplicate_business_event' WHERE receipt_id=$1", row.ReceiptID); e != nil {
				return DistributionDeliveryReceipt{}, internal("Не удалось сохранить disposition", e)
			}
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return DistributionDeliveryReceipt{}, internal("Не удалось подтвердить inbox", e)
	}
	return DistributionDeliveryReceipt{message, row.ReceiptID, "accepted", row.AcceptedAt.UTC()}, nil
}

func stringVersion(n int64) string { return strconv.FormatInt(n, 10) }
