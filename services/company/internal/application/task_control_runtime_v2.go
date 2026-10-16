package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type TaskControlRuntimeEnvelopeV2 struct {
	RuntimeSchemaVersion int                        `json:"runtimeSchemaVersion"`
	Scope                TaskControlScope           `json:"scope"`
	ActorID              string                     `json:"actorId"`
	PublicationID        uuid.UUID                  `json:"publicationId"`
	PublicationVersion   int64                      `json:"publicationVersion"`
	IntentChecksum       string                     `json:"intentChecksum"`
	SnapshotID           uuid.UUID                  `json:"snapshotId"`
	RuntimeRevision      int64                      `json:"runtimeRevision"`
	SourceRevisions      TaskControlSourceRevisions `json:"sourceRevisions"`
	ResolverVersion      int                        `json:"resolverVersion"`
	ResolvedChecksum     string                     `json:"resolvedChecksum"`
	PayloadChecksum      string                     `json:"payloadChecksum"`
	RuntimeRules         []tc.LegacyRule            `json:"runtimeRules"`
	IssuedAt             time.Time                  `json:"issuedAt"`
	ValidUntil           time.Time                  `json:"validUntil"`
	MinRuntimeVersion    string                     `json:"minRuntimeVersion"`
}
type TaskControlAckInputV2 struct {
	Scope              TaskControlScope `json:"scope"`
	RuntimeInstanceID  uuid.UUID        `json:"runtimeInstanceId"`
	ActorID            string           `json:"actorId"`
	SnapshotID         uuid.UUID        `json:"snapshotId"`
	PublicationID      uuid.UUID        `json:"publicationId"`
	PublicationVersion int64            `json:"publicationVersion"`
	RuntimeRevision    int64            `json:"runtimeRevision"`
	PayloadChecksum    string           `json:"payloadChecksum"`
	AppliedAt          time.Time        `json:"appliedAt"`
	RuntimeVersion     string           `json:"runtimeVersion"`
}

func (s *Service) TaskControlBootstrapV2(ctx context.Context, token string) (TaskControlRuntimeEnvelopeV2, error) {
	var out TaskControlRuntimeEnvelopeV2
	conn, actor, e := s.AuthorizeTaskControlRuntime(ctx, token)
	if e != nil {
		return out, e
	}
	if !taskControlV2Descriptor(conn.Installation) {
		return out, coded(ErrorConflict, "runtime_upgrade_required", "Требуется подтверждённый runtime версии 1.2.0")
	}
	if conn.Mode != "managed" {
		return out, conflict("Подключение находится в автономном режиме")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.LockTaskControlAccount(ctx, conn.Scope.AccountID); e != nil {
		return out, e
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: conn.Scope.CompanyID, ID: conn.Scope.BindingID})
	if e != nil {
		return out, e
	}
	if taskControlScope(c) != conn.Scope || c.State != "active" || c.Mode != "managed" {
		return out, forbidden("Сессия подключения отозвана")
	}
	if e = taskControlV2Required(c); e != nil {
		return out, e
	}
	if e = s.currentTaskControlInstallation(ctx, q, c.CompanyID, c.AccountID, c.InstallationID, c.IntegrationID, c.Generation); e != nil {
		return out, e
	}
	if e = s.taskControlV2DescriptorTx(ctx, q, c); e != nil {
		return out, e
	}
	// Re-check the current source, including access revocation, before every issue.
	refs, e := s.taskControlFreshReferencesTx(ctx, q, c)
	if e != nil {
		return out, e
	}
	state, e := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: c.CompanyID, BindingID: c.ID})
	if e != nil || !state.CurrentSnapshotID.Valid || !state.VerifiedSourceFreshUntil.Valid {
		return out, coded(ErrorUpstream, "references_unavailable", "Пересчёт правил ещё не завершён")
	}
	row, e := q.GetTaskControlV2Snapshot(ctx, db.GetTaskControlV2SnapshotParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: state.CurrentSnapshotID.UUID})
	if e != nil {
		return out, e
	}
	var snap TaskControlResolvedSnapshot
	if e = json.Unmarshal(row.Payload, &snap); e != nil {
		return out, e
	}
	if snap.Scope != conn.Scope || !c.PublicationID.Valid || snap.PublicationID != c.PublicationID.UUID {
		return out, coded(ErrorUpstream, "references_unavailable", "Требуется пересчёт правил")
	}
	if refs.Customers.Availability == "unknown" {
		publication, err := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: snap.PublicationID})
		if err != nil {
			return out, err
		}
		var pub TaskControlPublicationV2
		if err = json.Unmarshal(publication.Payload, &pub); err != nil {
			return out, err
		}
		for _, item := range pub.IntentRules {
			depends := item.Dynamic != nil && item.Dynamic.Enabled && !item.Dynamic.Archived && item.Dynamic.Customers.Kind != "disabled"
			depends = depends || (item.Legacy != nil && item.Legacy.Enabled && !item.Legacy.Archived && item.Legacy.Customers)
			if depends {
				return out, coded(ErrorUpstream, "references_unavailable", "Не подтверждены справочники покупателей")
			}
		}
	}
	now := s.now().UTC()
	fresh := state.VerifiedSourceFreshUntil.Time
	if refs.FreshUntil.Before(fresh) {
		fresh = refs.FreshUntil
	}
	if !fresh.After(now) {
		return out, coded(ErrorUpstream, "references_unavailable", "Справочники устарели")
	}
	until := now.Add(taskControlSessionTTL)
	if fresh.Before(until) {
		until = fresh
	}
	rules, checksum := tc.ProjectRuntimeRules(snap.Rules, actor)
	out = TaskControlRuntimeEnvelopeV2{2, conn.Scope, actor, snap.PublicationID, snap.PublicationVersion, snap.IntentChecksum, snap.SnapshotID, snap.RuntimeRevision, snap.SourceRevisions, 1, snap.ResolvedChecksum, checksum, rules, now, until, "1.2.0"}
	raw, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	if len(raw) > 1<<20 {
		return out, coded(ErrorPayloadTooLarge, "runtime_payload_too_large", "Runtime превышает 1 МиБ")
	}
	if e = q.SaveTaskControlV2Issued(ctx, db.SaveTaskControlV2IssuedParams{CompanyID: c.CompanyID, BindingID: c.ID, ModeEpoch: c.ModeEpoch, ActorID: actor, SnapshotID: snap.SnapshotID, PayloadChecksum: checksum, Payload: raw, IssuedAt: now}); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) AckTaskControlV2(ctx context.Context, token string, in TaskControlAckInputV2) error {
	conn, actor, e := s.AuthorizeTaskControlRuntime(ctx, token)
	if e != nil {
		return e
	}
	if in.Scope != conn.Scope || in.ActorID != actor || in.RuntimeInstanceID == uuid.Nil || in.SnapshotID == uuid.Nil || in.RuntimeVersion != "1.2.0" || in.RuntimeRevision < 1 || in.RuntimeRevision > 9007199254740991 || in.AppliedAt.IsZero() || in.AppliedAt.After(s.now().Add(5*time.Second)) {
		return validation("Некорректное подтверждение runtime")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.LockTaskControlAccount(ctx, conn.Scope.AccountID); e != nil {
		return e
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: conn.Scope.CompanyID, ID: conn.Scope.BindingID})
	if e != nil {
		return e
	}
	if taskControlScope(c) != in.Scope || c.State != "active" || c.RuntimeSchemaVersion != 2 {
		return forbidden("Сессия подключения отозвана")
	}
	issued, e := q.GetTaskControlV2Issued(ctx, db.GetTaskControlV2IssuedParams{CompanyID: c.CompanyID, BindingID: c.ID, ModeEpoch: c.ModeEpoch, ActorID: actor, SnapshotID: in.SnapshotID})
	if e != nil || issued.IssuedAt.Before(s.now().Add(-30*24*time.Hour)) {
		return validation("Этот снимок не выдавался текущему пользователю")
	}
	var envelope TaskControlRuntimeEnvelopeV2
	if json.Unmarshal(issued.Payload, &envelope) != nil || envelope.PublicationID != in.PublicationID || envelope.PublicationVersion != in.PublicationVersion || envelope.RuntimeRevision != in.RuntimeRevision || issued.PayloadChecksum != in.PayloadChecksum || in.AppliedAt.Before(envelope.IssuedAt.Add(-5*time.Second)) {
		return conflict("Подтверждение не соответствует выданному снимку")
	}
	prior, e := q.GetTaskControlV2Ack(ctx, db.GetTaskControlV2AckParams{CompanyID: c.CompanyID, BindingID: c.ID, ModeEpoch: c.ModeEpoch, ActorID: actor, RuntimeInstanceID: in.RuntimeInstanceID, RuntimeRevision: in.RuntimeRevision})
	if e == nil {
		if prior.PayloadChecksum != in.PayloadChecksum {
			return conflict("Контрольная сумма подтверждения изменилась")
		}
	} else if !isNoRows(e) {
		return e
	}
	raw, _ := json.Marshal(in)
	if e = q.SaveTaskControlV2Ack(ctx, db.SaveTaskControlV2AckParams{CompanyID: c.CompanyID, BindingID: c.ID, ModeEpoch: c.ModeEpoch, ActorID: actor, RuntimeInstanceID: in.RuntimeInstanceID, RuntimeRevision: in.RuntimeRevision, PayloadChecksum: in.PayloadChecksum, Payload: raw, ReceivedAt: s.now().UTC()}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) TaskControlApplicationV2(ctx context.Context, a Actor, id, pubID uuid.UUID) (any, error) {
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, false)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, e = q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: pubID}); e != nil {
		return nil, notFound("Публикация")
	}
	out := map[string]any{"publicationId": pubID, "currentSnapshotId": nil, "currentRuntimeRevision": nil, "runtimeState": "notApplied", "lastRuntimeAck": nil, "issues": []tc.IntentIssue{}}
	state, e := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil && !isNoRows(e) {
		return nil, e
	}
	if e == nil && state.CurrentSnapshotID.Valid && c.PublicationID.Valid && c.PublicationID.UUID == pubID {
		out["currentSnapshotId"] = state.CurrentSnapshotID.UUID
		out["currentRuntimeRevision"] = state.RuntimeRevision
		row, err := q.GetTaskControlV2Snapshot(ctx, db.GetTaskControlV2SnapshotParams{CompanyID: a.CompanyID, BindingID: id, ID: state.CurrentSnapshotID.UUID})
		var snap TaskControlResolvedSnapshot
		if err == nil && json.Unmarshal(row.Payload, &snap) == nil {
			out["issues"] = snap.Issues
		}
		if !state.VerifiedSourceFreshUntil.Valid || !state.VerifiedSourceFreshUntil.Time.After(s.now()) {
			out["runtimeState"] = "stale"
		}
	}
	ack, e := q.LatestTaskControlV2Ack(ctx, db.LatestTaskControlV2AckParams{CompanyID: a.CompanyID, BindingID: id, PublicationID: pubID.String()})
	if e == nil {
		out["lastRuntimeAck"] = map[string]any{"receivedAt": ack.ReceivedAt, "actorId": ack.ActorID, "runtimeInstanceId": ack.RuntimeInstanceID, "runtimeRevision": ack.RuntimeRevision, "payloadChecksum": ack.PayloadChecksum}
		if c.State == "active" && c.Mode == "managed" && ack.ModeEpoch == c.ModeEpoch && ack.RuntimeRevision == state.RuntimeRevision && s.now().Sub(ack.ReceivedAt) <= 5*time.Minute && state.VerifiedSourceFreshUntil.Valid && state.VerifiedSourceFreshUntil.Time.After(s.now()) {
			out["runtimeState"] = "appliedByRuntime"
		}
	} else if !isNoRows(e) {
		return nil, e
	}
	if c.State == "active" && c.Mode == "managed" && c.PublicationID.Valid && c.PublicationID.UUID == pubID && state.VerifiedSourceFreshUntil.Valid && state.VerifiedSourceFreshUntil.Time.After(s.now()) {
		applied, err := q.HasCurrentTaskControlV2Ack(ctx, db.HasCurrentTaskControlV2AckParams{CompanyID: a.CompanyID, BindingID: id, ModeEpoch: c.ModeEpoch, RuntimeRevision: state.RuntimeRevision, FreshAfter: s.now().Add(-5 * time.Minute)})
		if err != nil {
			return nil, err
		}
		if applied {
			out["runtimeState"] = "appliedByRuntime"
		}
	}
	if failed, err := q.GetTaskControlV2Failure(ctx, db.GetTaskControlV2FailureParams{CompanyID: a.CompanyID, BindingID: id}); err == nil {
		issues := out["issues"].([]tc.IntentIssue)
		out["issues"] = append(issues, tc.IntentIssue{Code: failed.Code, Path: "sources", Message: failed.Message, Blocking: true})
	} else if !isNoRows(err) {
		return nil, err
	}
	return out, nil
}
