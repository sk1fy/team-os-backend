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
	if e = saveTaskControlRuntimeIssue(ctx, q, conn.Scope, actor, snap.SnapshotID, snap.PublicationID, snap.PublicationVersion, snap.RuntimeRevision, checksum, now, until); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) AckTaskControlV2(ctx context.Context, token string, in TaskControlAckInputV2) error {
	if in.SnapshotID == uuid.Nil || !taskControlRuntimeV2Version(in.RuntimeVersion) {
		return validation("Для подтверждения runtime v2 требуется снимок и совместимая версия")
	}
	return s.AckTaskControl(ctx, token, in)
}
func (s *Service) TaskControlApplicationV2(ctx context.Context, a Actor, id, pubID uuid.UUID) (any, error) {
	return s.taskControlApplicationDiagnostics(ctx, a, id, pubID, true)
}
