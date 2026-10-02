package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) currentDeliveryBinding(ctx context.Context, q *db.Queries, scope corebridge.Scope) bool {
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: scope.CompanyID, ID: scope.BindingID})
	return e == nil && b.State == "active" && bindingScope(b) == scope
}
func operationIdentity(op corebridge.Operation) []byte {
	raw, _ := json.Marshal(struct {
		ID                                                 uuid.UUID
		Scope                                              corebridge.Scope
		Lead                                               string
		Episode, Decision, Rule, Group, Event, Correlation uuid.UUID
		RuleRevision, AvailabilityRevision, ClaimRevision  int64
		Target                                             string
	}{op.OperationID, op.Scope, op.LeadID, op.EpisodeID, op.DecisionID, op.RuleID, op.GroupID, op.EventID, op.CorrelationID, op.RuleRevision, op.AvailabilityRevision, op.ClaimRevision, op.TargetResponsibleUserID})
	return raw
}

// RegisterDistributionOperationMirror is an explicit Team-owned action. A pushed
// result never registers a business decision. Future stage06 calls this only
// after its own durable decision/command receipt is saved.
func (s *Service) RegisterDistributionOperationMirror(ctx context.Context, actor Actor, expected corebridge.Operation) error {
	if _, e := s.distributionActor(ctx, actor, true); e != nil {
		return e
	}
	if expected.Scope.CompanyID != actor.CompanyID || s.deliveryCore == nil {
		return forbidden("Регистрация операции недоступна")
	}
	current, e := s.deliveryCore.Operation(ctx, expected.Scope, expected.OperationID)
	if e != nil {
		return coreError(e)
	}
	if !validOperation(current) || !validCRMID(current.LeadID) || !bytes.Equal(operationIdentity(current), operationIdentity(expected)) {
		return conflict("Immutable identity операции не подтверждена Core")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return internal("Не удалось начать регистрацию операции", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.EnsureDistributionAvailabilityVersion(ctx, current.Scope.CompanyID); e != nil {
		return e
	}
	if _, e = q.LockDistributionAvailabilityVersion(ctx, current.Scope.CompanyID); e != nil {
		return e
	}
	if e = s.deliveryScope(ctx, q, current.Scope); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", current.OperationID.String()); e != nil {
		return internal("Не удалось заблокировать операцию", e)
	}
	old, e := q.GetDistributionOperationMirror(ctx, current.OperationID)
	if e == nil {
		var registered corebridge.Operation
		if json.Unmarshal(old.RegistrationPayload, &registered) != nil || !bytes.Equal(operationIdentity(registered), operationIdentity(current)) {
			return conflict("Операция уже зарегистрирована с другим scope")
		}
	} else if isNoRows(e) {
		raw, _ := json.Marshal(current)
		e = q.RegisterDistributionOperationMirror(ctx, db.RegisterDistributionOperationMirrorParams{OperationID: current.OperationID, CompanyID: actor.CompanyID, BindingID: current.Scope.BindingID, BindingRevision: current.Scope.BindingRevision, InstallationID: current.Scope.InstallationID, IntegrationID: current.Scope.IntegrationID, AccountID: current.Scope.AccountID, LeadID: current.LeadID, EpisodeID: current.EpisodeID, DecisionID: current.DecisionID, RuleID: current.RuleID, GroupID: current.GroupID, TargetResponsibleID: current.TargetResponsibleUserID, RegistrationPayload: raw, RegisteredBy: actor.UserID})
		if e != nil {
			return internal("Не удалось сохранить регистрацию", e)
		}
	} else {
		return internal("Не удалось проверить регистрацию", e)
	}
	if e = s.applyOperationResult(ctx, q, current); e != nil {
		return e
	}
	if e = q.WakeDistributionResultInbox(ctx, current.OperationID.String()); e != nil {
		return internal("Не удалось восстановить pending результат", e)
	}
	return tx.Commit(ctx)
}
func (s *Service) applyOperationResult(ctx context.Context, q *db.Queries, op corebridge.Operation) error {
	if e := q.EnsureDistributionAvailabilityVersion(ctx, op.Scope.CompanyID); e != nil {
		return e
	}
	if _, e := q.LockDistributionAvailabilityVersion(ctx, op.Scope.CompanyID); e != nil {
		return e
	}
	mirror, e := q.GetDistributionOperationMirror(ctx, op.OperationID)
	if e != nil {
		if isNoRows(e) {
			return coded(ErrorConflict, "operation_not_registered", "Операция ожидает регистрации владельцем очереди")
		}
		return internal("Не удалось получить mirror", e)
	}
	var registered corebridge.Operation
	if json.Unmarshal(mirror.RegistrationPayload, &registered) != nil {
		return internal("Повреждена регистрация операции", nil)
	}
	if op.LeadID == "" {
		op.LeadID = registered.LeadID
	}
	if !validOperation(op) || !bytes.Equal(operationIdentity(registered), operationIdentity(op)) {
		return conflict("Результат не соответствует зарегистрированной операции")
	}
	normalizeOperation(&op)
	raw, _ := json.Marshal(op)
	hash := sha256.Sum256(raw)
	old, e := q.GetDistributionMirrorVersion(ctx, db.GetDistributionMirrorVersionParams{OperationID: op.OperationID, ResultVersion: op.ResultVersion})
	if e == nil {
		if !bytes.Equal(old.PayloadHash, hash[:]) {
			return conflict("Одна версия операции содержит разные результаты")
		}
		return nil
	}
	if !isNoRows(e) {
		return internal("Не удалось проверить историю результата", e)
	}
	if e = q.CreateDistributionMirrorVersion(ctx, db.CreateDistributionMirrorVersionParams{OperationID: op.OperationID, ResultVersion: op.ResultVersion, PayloadHash: hash[:], Payload: raw}); e != nil {
		return internal("Не удалось сохранить историю результата", e)
	}
	if op.ResultVersion > mirror.ResultVersion {
		if e = q.UpdateDistributionOperationMirror(ctx, db.UpdateDistributionOperationMirrorParams{OperationID: op.OperationID, ResultVersion: op.ResultVersion, ResultHash: hash[:], ResultPayload: raw, State: op.State, Unfinished: !op.ResolutionEvidence.GuardReleasable}); e != nil {
			return internal("Не удалось обновить mirror", e)
		}
	}
	row, e := q.GetDistributionQueueByOperation(ctx, nullID(op.OperationID))
	if isNoRows(e) {
		return nil
	}
	if e != nil {
		return e
	}
	row, e = q.LockDistributionQueue(ctx, row.ID)
	if e != nil {
		return e
	}
	return s.settleRuntimeOperation(ctx, q, row, op)
}
func (s *Service) ProcessDistributionDelivery(ctx context.Context) (bool, error) {
	token := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	row, e := db.New(s.pool).ClaimDistributionDelivery(ctx, token)
	if isNoRows(e) {
		return false, nil
	}
	if e != nil {
		return false, internal("Не удалось получить inbox lease", e)
	}
	if row.MessageKind == "event" {
		e = s.processDistributionEvent(ctx, row)
	} else {
		e = s.processDistributionResult(ctx, row)
	}
	if e != nil {
		code := "processing_unavailable"
		var app *Error
		if errors.As(e, &app) && app.Code != "" {
			code = app.Code
		}
		delay := time.Duration(int64(row.Attempts)*int64(row.Attempts)) * time.Second
		if delay > 5*time.Minute {
			delay = 5 * time.Minute
		}
		_ = db.New(s.pool).RetryDistributionDelivery(ctx, db.RetryDistributionDeliveryParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken, ErrorCode: pgText(&code), NextAttemptAt: s.now().Add(delay)})
	}
	return true, e
}
func (s *Service) processDistributionResult(ctx context.Context, row db.DistributionDeliveryInbox) error {
	var envelope DistributionResultEnvelope
	if json.Unmarshal(row.Payload, &envelope) != nil {
		return validation("Повреждён inbox результата")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, e = q.LockDistributionDeliveryLease(ctx, db.LockDistributionDeliveryLeaseParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken}); e != nil {
		return e
	}
	if e = s.applyOperationResult(ctx, q, envelope.Result); e != nil {
		return e
	}
	if _, e = q.LockDistributionDeliveryLease(ctx, db.LockDistributionDeliveryLeaseParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken}); e != nil {
		return e
	}
	if e = q.FinishDistributionDelivery(ctx, db.FinishDistributionDeliveryParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken, State: "applied"}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) processDistributionEvent(ctx context.Context, row db.DistributionDeliveryInbox) error {
	var in DistributionEventEnvelope
	if json.Unmarshal(row.Payload, &in) != nil {
		return validation("Повреждён inbox события")
	}
	scope := in.Scope
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, e = q.LockDistributionDeliveryLease(ctx, db.LockDistributionDeliveryLeaseParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken}); e != nil {
		return e
	}
	if !s.currentDeliveryBinding(ctx, q, scope) {
		reason := "historical_binding_event"
		if _, e = q.LockDistributionDeliveryLease(ctx, db.LockDistributionDeliveryLeaseParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken}); e != nil {
			return e
		}
		if e = q.FinishDistributionDelivery(ctx, db.FinishDistributionDeliveryParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken, State: "ignored", ErrorCode: pgText(&reason)}); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	if e = q.EnsureDistributionLeadHead(ctx, db.EnsureDistributionLeadHeadParams{AccountID: scope.AccountID, LeadID: in.Event.LeadID, CompanyID: scope.CompanyID, BindingID: scope.BindingID, BindingRevision: scope.BindingRevision}); e != nil {
		return e
	}
	head, e := q.LockDistributionLeadHead(ctx, db.LockDistributionLeadHeadParams{AccountID: scope.AccountID, LeadID: in.Event.LeadID})
	if e != nil {
		return e
	}
	if head.CompanyID != scope.CompanyID {
		return coded(ErrorForbidden, "projection_ownership_conflict", "Lead head принадлежит другой компании; перенос требует отдельной процедуры")
	}
	generation, e := q.NextDistributionObservationGeneration(ctx, db.NextDistributionObservationGenerationParams{AccountID: scope.AccountID, LeadID: in.Event.LeadID, CompanyID: scope.CompanyID})
	if e != nil {
		return e
	}
	if e = q.SetDistributionDeliveryGeneration(ctx, db.SetDistributionDeliveryGenerationParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken, ObservationGeneration: pgtype.Int8{Int64: generation, Valid: true}}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if s.deliveryCore == nil {
		return upstream("Core не настроен", nil)
	}
	observed, e := s.deliveryCore.ReadLead(ctx, scope, in.Event.LeadID)
	if e != nil {
		return coreError(e)
	}
	if observed.Scope != scope || observed.LeadID != in.Event.LeadID || !safeRevision(observed.ObservationRevision) || observed.ObservedAt.IsZero() || observed.ObservedAt.After(s.now().Add(time.Minute)) || s.now().Sub(observed.ObservedAt) > time.Minute || (observed.Deleted && observed.Absent) || (!observed.Absent && observed.AbsenceReason != nil) || (observed.Deleted || observed.Absent) != (observed.Snapshot == nil) || (observed.Absent && (observed.AbsenceReason == nil || *observed.AbsenceReason != "not_found_or_deleted")) || !validSnapshot(observed.Snapshot, in.Event.LeadID) {
		return upstream("Актуальное состояние CRM не подтверждено", nil)
	}
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	if _, e = q.LockDistributionDeliveryLease(ctx, db.LockDistributionDeliveryLeaseParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken}); e != nil {
		return e
	}
	head, e = q.LockDistributionLeadHead(ctx, db.LockDistributionLeadHeadParams{AccountID: scope.AccountID, LeadID: in.Event.LeadID})
	if e != nil {
		return e
	}
	reason := ""
	if !s.currentDeliveryBinding(ctx, q, scope) {
		reason = "historical_binding_event"
	} else if head.AppliedGeneration >= generation || observed.ObservationRevision <= head.ObservationRevision {
		reason = "stale_observation"
	}
	if reason != "" {
		if _, e = q.LockDistributionDeliveryLease(ctx, db.LockDistributionDeliveryLeaseParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken}); e != nil {
			return e
		}
		if e = q.FinishDistributionDelivery(ctx, db.FinishDistributionDeliveryParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken, State: "ignored", ErrorCode: pgText(&reason)}); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	if _, e = q.LockDistributionDeliveryLease(ctx, db.LockDistributionDeliveryLeaseParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken}); e != nil {
		return e
	}
	var previous *corebridge.LeadSnapshot
	if len(head.Snapshot) > 0 && json.Unmarshal(head.Snapshot, &previous) != nil {
		return internal("Повреждён lead head", nil)
	}
	// A same-stage notification can describe an unseen exit/reentry. Timestamp
	// equality is not entry identity; pause the old candidate without inventing a
	// new sequence. Clearly older source claims do not demote newer observations.
	source := in.Event.SourceEvidence
	sourceMatches := source != nil && source.StatusID != nil && observed.Snapshot != nil && *source.StatusID == observed.Snapshot.StatusID && (source.PipelineID == nil || *source.PipelineID == observed.Snapshot.PipelineID)
	oldMatches := source != nil && source.OldStatusID != nil && previous != nil && *source.OldStatusID == previous.StatusID && (source.OldPipelineID == nil || *source.OldPipelineID == previous.PipelineID)
	ambiguous := previous != nil && observed.Snapshot != nil && previous.PipelineID == observed.Snapshot.PipelineID && previous.StatusID == observed.Snapshot.StatusID && in.Event.Kind == "lead.status_changed" && source != nil && source.StatusID != nil && source.OldStatusID != nil && (sourceMatches != oldMatches) && (in.SourceOccurredAt == nil || previous.SourceUpdatedAt == nil || !in.SourceOccurredAt.Before(*previous.SourceUpdatedAt))
	if ambiguous && head.CurrentEntryID.Valid {
		if e = q.PauseDistributionObservedEntry(ctx, head.CurrentEntryID.UUID); e != nil {
			return e
		}
	}
	changed := observed.Deleted || observed.Absent || head.Deleted || head.Absent || previous == nil || observed.Snapshot != nil && (previous.PipelineID != observed.Snapshot.PipelineID || previous.StatusID != observed.Snapshot.StatusID) || head.BindingID != scope.BindingID
	entry := head.CurrentEntryID
	sequence := head.LastSequence
	if changed && entry.Valid {
		cancelReason := "observed_stage_exit"
		if observed.Absent {
			cancelReason = "observed_missing"
		}
		if observed.Deleted {
			cancelReason = "observed_deleted"
		}
		if head.BindingID != scope.BindingID {
			cancelReason = "binding_changed"
		}
		if e = q.CancelDistributionObservedEntry(ctx, db.CancelDistributionObservedEntryParams{ID: entry.UUID, CancellationReason: pgText(&cancelReason)}); e != nil {
			return e
		}
		entry = uuid.NullUUID{}
	}
	if changed && observed.Snapshot != nil {
		sequence++
		id := uuid.New()
		entry = uuid.NullUUID{UUID: id, Valid: true}
		evidence, state := "reconciled", "needs_configuration"
		if previous != nil && !head.Deleted && !head.Absent {
			evidence, state = "observed_transition", "checking"
		} else if in.Event.Kind == "lead.created" && in.Event.SourceEvidence != nil && in.Event.SourceEvidence.PipelineID != nil && in.Event.SourceEvidence.StatusID != nil && *in.Event.SourceEvidence.PipelineID == observed.Snapshot.PipelineID && *in.Event.SourceEvidence.StatusID == observed.Snapshot.StatusID {
			evidence, state = "created_in_stage", "checking"
		}
		if in.SourceOccurredAt == nil {
			state = "needs_configuration"
			evidence = "source_time_unknown"
		}
		if e = q.CreateDistributionObservedEntry(ctx, db.CreateDistributionObservedEntryParams{ID: id, CompanyID: scope.CompanyID, AccountID: scope.AccountID, LeadID: in.Event.LeadID, BindingID: scope.BindingID, BindingRevision: scope.BindingRevision, Sequence: sequence, PipelineID: observed.Snapshot.PipelineID, StatusID: observed.Snapshot.StatusID, EntryEventID: in.EventID, Evidence: evidence, State: state, SourceReceivedAt: pgtype.Timestamptz{Time: in.ReceivedAt, Valid: true}, SourceOccurredAt: optionalDeliveryTime(in.SourceOccurredAt)}); e != nil {
			return e
		}
	}
	normalizeSnapshot(observed.Snapshot)
	raw, _ := json.Marshal(observed.Snapshot)
	if e = q.UpdateDistributionLeadHead(ctx, db.UpdateDistributionLeadHeadParams{AccountID: scope.AccountID, LeadID: in.Event.LeadID, CompanyID: scope.CompanyID, BindingID: scope.BindingID, BindingRevision: scope.BindingRevision, AppliedGeneration: generation, ObservationRevision: observed.ObservationRevision, LastSequence: sequence, Snapshot: raw, Deleted: observed.Deleted, Absent: observed.Absent, AbsenceReason: pgText(observed.AbsenceReason), CurrentEntryID: entry, ObservedAt: pgtype.Timestamptz{Time: observed.ObservedAt.UTC(), Valid: true}}); e != nil {
		return e
	}
	if _, e = q.LockDistributionDeliveryLease(ctx, db.LockDistributionDeliveryLeaseParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken}); e != nil {
		return e
	}
	if e = q.FinishDistributionDelivery(ctx, db.FinishDistributionDeliveryParams{ReceiptID: row.ReceiptID, LeaseToken: row.LeaseToken, State: "applied"}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) ReconcileDistributionOperations(ctx context.Context, limit int32) error {
	if limit < 1 || limit > 100 {
		return validation("Размер сверки должен быть от 1 до 100")
	}
	if s.deliveryCore == nil {
		return upstream("Core не настроен", nil)
	}
	rows, e := db.New(s.pool).DueDistributionOperationMirrors(ctx, limit)
	if e != nil {
		return e
	}
	var reconcileErr error
	for _, mirror := range rows {
		reserved, reserveErr := db.New(s.pool).ReserveDistributionMirrorReconcile(ctx, mirror.OperationID)
		if reserveErr != nil {
			return reserveErr
		}
		if reserved != 1 {
			continue
		}
		scope := corebridge.Scope{CompanyID: mirror.CompanyID, BindingID: mirror.BindingID, BindingRevision: mirror.BindingRevision, InstallationID: mirror.InstallationID, IntegrationID: mirror.IntegrationID, AccountID: mirror.AccountID}
		op, err := s.deliveryCore.Operation(ctx, scope, mirror.OperationID)
		if err == nil {
			tx, txerr := s.pool.Begin(ctx)
			if txerr == nil {
				q := db.New(tx)
				err = s.applyOperationResult(ctx, q, op)
				if err == nil {
					err = tx.Commit(ctx)
				}
				_ = tx.Rollback(ctx)
			} else {
				err = txerr
			}
		}
		if err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
			code := "operation_reconcile_unavailable"
			delay := time.Duration((int64(mirror.ReconcileAttempts)+1)*(int64(mirror.ReconcileAttempts)+1)) * time.Second
			if delay > 5*time.Minute {
				delay = 5 * time.Minute
			}
			_ = db.New(s.pool).RetryDistributionMirror(ctx, db.RetryDistributionMirrorParams{OperationID: mirror.OperationID, ErrorCode: pgText(&code), NextReconcileAt: s.now().Add(delay)})
		}
	}
	return reconcileErr
}

func optionalDeliveryTime(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: v.UTC(), Valid: true}
}
