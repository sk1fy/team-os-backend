package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// TaskControlTelemetryInput carries bounded technical evidence, never rules or deal data.
type TaskControlTelemetryInput struct {
	Scope              TaskControlScope `json:"scope"`
	RuntimeInstanceID  uuid.UUID        `json:"runtimeInstanceId"`
	ActorID            string           `json:"actorId"`
	PublicationID      uuid.UUID        `json:"publicationId,omitempty"`
	PublicationVersion int64            `json:"publicationVersion,omitempty"`
	RuntimeRevision    int64            `json:"runtimeRevision,omitempty"`
	PayloadChecksum    string           `json:"payloadChecksum,omitempty"`
	RuntimeVersion     string           `json:"runtimeVersion"`
	Event              string           `json:"event"`
	EventID            uuid.UUID        `json:"eventId"`
	OccurredAt         time.Time        `json:"occurredAt"`
	CorrelationID      uuid.UUID        `json:"correlationId"`
	SnapshotID         uuid.UUID        `json:"snapshotId,omitempty"`
	ErrorCode          string           `json:"errorCode,omitempty"`
}
type taskControlRuntimeEvidence struct {
	TaskControlTelemetryInput
	LeaseValidUntil time.Time `json:"leaseValidUntil"`
}

var taskControlTelemetryCodes = map[string]bool{"config_invalid": true, "config_fetch_failed": true, "lease_expired": true, "scope_mismatch": true, "runtime_incompatible": true, "apply_failed": true, "references_unavailable": true, "network_unavailable": true}

func taskControlTelemetryValid(in TaskControlTelemetryInput, now time.Time) error {
	if in.RuntimeInstanceID == uuid.Nil || in.EventID == uuid.Nil || in.CorrelationID == uuid.Nil || in.OccurredAt.IsZero() || in.OccurredAt.After(now.Add(5*time.Second)) || in.OccurredAt.Before(now.Add(-5*time.Minute)) || !taskControlRuntimeV2Version(in.RuntimeVersion) && in.RuntimeVersion != "1.1.4" {
		return validation("Некорректное техническое наблюдение runtime")
	}
	switch in.Event {
	case "received", "applied", "heartbeat":
		if in.ErrorCode != "" {
			return validation("Причина ошибки допустима только для события ошибки")
		}
	case "error":
		if !taskControlTelemetryCodes[in.ErrorCode] {
			return validation("Неизвестная причина ошибки runtime")
		}
	default:
		return validation("Неизвестное событие runtime")
	}
	if in.PublicationID != uuid.Nil {
		if in.PublicationVersion < 1 || in.RuntimeRevision < 1 || in.RuntimeRevision > 9007199254740991 || in.PublicationVersion > 9007199254740991 || len(in.PayloadChecksum) != 64 {
			return validation("Некорректная версия runtime")
		}
		if _, e := hex.DecodeString(in.PayloadChecksum); e != nil {
			return validation("Некорректная контрольная сумма runtime")
		}
	} else if in.Event != "error" || in.PublicationVersion != 0 || in.RuntimeRevision != 0 || in.PayloadChecksum != "" || in.SnapshotID != uuid.Nil {
		return validation("Для события требуется выданная публикация")
	}
	return nil
}

func saveTaskControlRuntimeIssue(ctx context.Context, q *db.Queries, scope TaskControlScope, actor string, key, pub uuid.UUID, pubVersion, revision int64, checksum string, issued, until time.Time) error {
	return q.SaveTaskControlRuntimeIssued(ctx, db.SaveTaskControlRuntimeIssuedParams{CompanyID: scope.CompanyID, BindingID: scope.BindingID, ModeEpoch: scope.ModeEpoch, ActorID: actor, IssueKey: key, PublicationID: pub, PublicationVersion: pubVersion, RuntimeRevision: revision, PayloadChecksum: checksum, IssuedAt: issued, ValidUntil: until})
}

func (s *Service) taskControlTelemetryTx(ctx context.Context, token string) (pgx.Tx, *db.Queries, db.TaskControlConnection, string, error) {
	conn, actor, e := s.AuthorizeTaskControlRuntime(ctx, token)
	if e != nil {
		return nil, nil, db.TaskControlConnection{}, "", e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, nil, db.TaskControlConnection{}, "", e
	}
	q := db.New(tx)
	fail := func(err error) (pgx.Tx, *db.Queries, db.TaskControlConnection, string, error) {
		_ = tx.Rollback(ctx)
		return nil, nil, db.TaskControlConnection{}, "", err
	}
	if e = q.LockTaskControlAccount(ctx, conn.Scope.AccountID); e != nil {
		return fail(e)
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: conn.Scope.CompanyID, ID: conn.Scope.BindingID})
	if e != nil {
		return fail(e)
	}
	hash := sha256.Sum256([]byte(token))
	sess, e := q.GetTaskControlRuntimeSession(ctx, db.GetTaskControlRuntimeSessionParams{TokenHash: hash[:], ExpiresAt: s.now()})
	if e != nil || c.State != "active" || taskControlScope(c) != conn.Scope || sess.BindingRevision != c.BindingRevision || sess.ModeEpoch != c.ModeEpoch || sess.Generation != c.Generation {
		return fail(forbidden("Сессия подключения отозвана"))
	}
	if e = s.currentTaskControlInstallation(ctx, q, c.CompanyID, c.AccountID, c.InstallationID, c.IntegrationID, c.Generation); e != nil {
		return fail(e)
	}
	return tx, q, c, actor, nil
}

// RecordTaskControlTelemetry does not extend a runtime lease or alter installation state.
func (s *Service) RecordTaskControlTelemetry(ctx context.Context, token string, in TaskControlTelemetryInput) error {
	now := s.now().UTC()
	if e := taskControlTelemetryValid(in, now); e != nil {
		return e
	}
	tx, q, c, actor, e := s.taskControlTelemetryTx(ctx, token)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if in.Scope != taskControlScope(c) || in.ActorID != actor {
		return forbidden("Наблюдение относится к другому пользователю или подключению")
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return e
	}
	if len(raw) > 8192 {
		return coded(ErrorPayloadTooLarge, "telemetry_payload_too_large", "Наблюдение превышает 8 КиБ")
	}
	hash := sha256.Sum256(raw)
	hs := hex.EncodeToString(hash[:])
	owner, err := q.GetTaskControlRuntimeInstanceActor(ctx, db.GetTaskControlRuntimeInstanceActorParams{CompanyID: c.CompanyID, BindingID: c.ID, RuntimeInstanceID: in.RuntimeInstanceID})
	if err != nil && !isNoRows(err) {
		return err
	}
	if err == nil && owner != actor {
		return forbidden("Экземпляр runtime принадлежит другому пользователю")
	}
	if isNoRows(err) {
		count, err := q.CountTaskControlRuntimeInstances(ctx, db.CountTaskControlRuntimeInstancesParams{CompanyID: c.CompanyID, BindingID: c.ID, Since: now.Add(-5 * time.Minute)})
		if err != nil {
			return err
		}
		if count >= 256 {
			return coded(ErrorRateLimited, "telemetry_instance_limit", "Достигнут лимит наблюдаемых экземпляров подключения")
		}
	}
	until := now.Add(taskControlSessionTTL)
	if in.PublicationID != uuid.Nil {
		if c.RuntimeSchemaVersion == 2 {
			if !taskControlRuntimeV2Version(in.RuntimeVersion) {
				return validation("Требуется совместимая версия runtime v2")
			}
			if err := s.taskControlV2DescriptorTx(ctx, q, c); err != nil {
				return err
			}
		}
		if c.Mode != "managed" || !c.PublicationID.Valid || c.PublicationID.UUID != in.PublicationID {
			return coded(ErrorConflict, "runtime_revision_stale", "Публикация runtime больше не активна")
		}
		key := in.PublicationID
		if c.RuntimeSchemaVersion == 2 {
			key = in.SnapshotID
			if key == uuid.Nil {
				return validation("Не указан снимок runtime")
			}
			state, err := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: c.CompanyID, BindingID: c.ID})
			if err != nil {
				return err
			}
			if !state.CurrentSnapshotID.Valid || state.CurrentSnapshotID.UUID != key || state.RuntimeRevision != in.RuntimeRevision || !state.VerifiedSourceFreshUntil.Valid || !state.VerifiedSourceFreshUntil.Time.After(now) {
				return coded(ErrorConflict, "runtime_revision_stale", "Ревизия runtime больше не актуальна")
			}
		} else if in.SnapshotID != uuid.Nil {
			return validation("Снимок доступен только для runtime v2")
		}
		issued, err := q.GetTaskControlRuntimeIssued(ctx, db.GetTaskControlRuntimeIssuedParams{CompanyID: c.CompanyID, BindingID: c.ID, ModeEpoch: c.ModeEpoch, ActorID: actor, IssueKey: key})
		if err != nil {
			if isNoRows(err) {
				return conflict("Этот runtime не выдавался текущему пользователю")
			}
			return err
		}
		if issued.PublicationID != in.PublicationID || issued.PublicationVersion != in.PublicationVersion || issued.RuntimeRevision != in.RuntimeRevision || issued.PayloadChecksum != in.PayloadChecksum || !issued.ValidUntil.After(now) || in.OccurredAt.Before(issued.IssuedAt.Add(-5*time.Second)) {
			return coded(ErrorConflict, "runtime_revision_stale", "Подтверждение не соответствует действующей выдаче runtime")
		}
		until = issued.ValidUntil
		if in.Event == "heartbeat" {
			applied, err := q.HasTaskControlRuntimeApplied(ctx, db.HasTaskControlRuntimeAppliedParams{CompanyID: c.CompanyID, BindingID: c.ID, ModeEpoch: c.ModeEpoch, ActorID: actor, RuntimeInstanceID: in.RuntimeInstanceID, PublicationID: in.PublicationID, RuntimeRevision: in.RuntimeRevision, Since: now.Add(-30 * 24 * time.Hour)})
			if err != nil {
				return err
			}
			if !applied {
				return conflict("Heartbeat не подтверждает первоначальное применение runtime")
			}
		}
	}
	prior, e := q.GetTaskControlRuntimeEvent(ctx, db.GetTaskControlRuntimeEventParams{CompanyID: c.CompanyID, BindingID: c.ID, EventID: in.EventID})
	if e == nil {
		if prior.PayloadHash != hs {
			return conflict("Идентификатор события уже использован")
		}
		return tx.Commit(ctx)
	}
	if !isNoRows(e) {
		return e
	}
	count, e := q.CountTaskControlRuntimeEvents(ctx, db.CountTaskControlRuntimeEventsParams{CompanyID: c.CompanyID, BindingID: c.ID, Since: now.Add(-time.Minute), RuntimeInstanceID: in.RuntimeInstanceID})
	if e != nil {
		return e
	}
	if count >= 20 {
		return coded(ErrorRateLimited, "telemetry_rate_limited", "Превышена частота наблюдений этого экземпляра")
	}
	count, e = q.CountTaskControlRuntimeEvents(ctx, db.CountTaskControlRuntimeEventsParams{CompanyID: c.CompanyID, BindingID: c.ID, Since: now.Add(-time.Minute), RuntimeInstanceID: uuid.Nil})
	if e != nil {
		return e
	}
	if count >= 1000 {
		return coded(ErrorRateLimited, "telemetry_rate_limited", "Превышена частота наблюдений подключения")
	}
	payload, e := json.Marshal(taskControlRuntimeEvidence{in, until})
	if e != nil {
		return e
	}
	if e = q.SaveTaskControlRuntimeEvent(ctx, db.SaveTaskControlRuntimeEventParams{CompanyID: c.CompanyID, BindingID: c.ID, EventID: in.EventID, ModeEpoch: c.ModeEpoch, ActorID: actor, RuntimeInstanceID: in.RuntimeInstanceID, PublicationID: in.PublicationID, RuntimeRevision: in.RuntimeRevision, Event: in.Event, PayloadHash: hs, Payload: payload, ReceivedAt: now}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func taskControlAckTelemetry(in TaskControlAckInputV2) TaskControlTelemetryInput {
	return TaskControlTelemetryInput{Scope: in.Scope, RuntimeInstanceID: in.RuntimeInstanceID, ActorID: in.ActorID, PublicationID: in.PublicationID, PublicationVersion: in.PublicationVersion, RuntimeRevision: in.RuntimeRevision, PayloadChecksum: in.PayloadChecksum, RuntimeVersion: in.RuntimeVersion, Event: "applied", EventID: uuid.New(), OccurredAt: in.AppliedAt, CorrelationID: uuid.New(), SnapshotID: in.SnapshotID}
}
func (s *Service) AckTaskControl(ctx context.Context, token string, in TaskControlAckInputV2) error {
	return s.RecordTaskControlTelemetry(ctx, token, taskControlAckTelemetry(in))
}

type TaskControlRuntimeInstance struct {
	RuntimeInstanceID uuid.UUID  `json:"runtimeInstanceId"`
	PublicationID     uuid.UUID  `json:"publicationId"`
	RuntimeRevision   int64      `json:"runtimeRevision"`
	RuntimeVersion    string     `json:"runtimeVersion"`
	State             string     `json:"state"`
	LastEvent         string     `json:"lastEvent"`
	LastSeenAt        time.Time  `json:"lastSeenAt"`
	LastReceivedAt    *time.Time `json:"lastReceivedAt,omitempty"`
	LastAppliedAt     *time.Time `json:"lastAppliedAt,omitempty"`
	LastHeartbeatAt   *time.Time `json:"lastHeartbeatAt,omitempty"`
	LeaseValidUntil   time.Time  `json:"leaseValidUntil"`
	Reason            string     `json:"reason,omitempty"`
	CorrelationID     uuid.UUID  `json:"correlationId"`
}

func (s *Service) TaskControlApplication(ctx context.Context, a Actor, id, pubID uuid.UUID) (any, error) {
	return s.taskControlApplicationDiagnostics(ctx, a, id, pubID, false)
}
func (s *Service) taskControlApplicationDiagnostics(ctx context.Context, a Actor, id, pubID uuid.UUID, v2 bool) (any, error) {
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, false)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	pub, e := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: pubID})
	if e != nil {
		if isNoRows(e) {
			return nil, notFound("Публикация")
		}
		return nil, e
	}
	now := s.now().UTC()
	revision := pub.Version
	var snapshot any
	issues := []tc.IntentIssue{}
	sourcesFresh := true
	installationCurrent := s.currentTaskControlInstallation(ctx, q, c.CompanyID, c.AccountID, c.InstallationID, c.IntegrationID, c.Generation) == nil
	if v2 {
		state, err := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: a.CompanyID, BindingID: id})
		if err != nil && !isNoRows(err) {
			return nil, err
		}
		if err == nil {
			revision = state.RuntimeRevision
			sourcesFresh = state.VerifiedSourceFreshUntil.Valid && state.VerifiedSourceFreshUntil.Time.After(now)
			if state.CurrentSnapshotID.Valid {
				snapshot = state.CurrentSnapshotID.UUID
				row, err := q.GetTaskControlV2Snapshot(ctx, db.GetTaskControlV2SnapshotParams{CompanyID: a.CompanyID, BindingID: id, ID: state.CurrentSnapshotID.UUID})
				if err != nil {
					return nil, err
				}
				var snap TaskControlResolvedSnapshot
				if err = json.Unmarshal(row.Payload, &snap); err != nil {
					return nil, err
				}
				issues = append(issues, snap.Issues...)
			}
		} else {
			sourcesFresh = false
		}
		if failure, err := q.GetTaskControlV2Failure(ctx, db.GetTaskControlV2FailureParams{CompanyID: a.CompanyID, BindingID: id}); err == nil {
			issues = append(issues, tc.IntentIssue{Code: failure.Code, Path: "sources", Message: failure.Message, Blocking: true})
		} else if !isNoRows(err) {
			return nil, err
		}
	}
	rows, e := q.ListTaskControlRuntimeEvents(ctx, db.ListTaskControlRuntimeEventsParams{CompanyID: a.CompanyID, BindingID: id, Since: now.Add(-30 * 24 * time.Hour)})
	if e != nil {
		return nil, e
	}
	instances := []TaskControlRuntimeInstance{}
	indices := map[uuid.UUID]int{}
	active, stale, applied := 0, 0, 0
	var lastAck any
	for _, row := range rows {
		var ev taskControlRuntimeEvidence
		if e = json.Unmarshal(row.Payload, &ev); e != nil {
			return nil, e
		}
		if _, ok := indices[row.RuntimeInstanceID]; !ok {
			if len(instances) >= 256 {
				continue
			}
			state, reason := "active", ""
			if row.ModeEpoch != c.ModeEpoch || ev.Scope != taskControlScope(c) {
				state, reason = "stale", "scope_changed"
			} else if now.Sub(row.ReceivedAt) > 5*time.Minute {
				state, reason = "stale", "heartbeat_expired"
			} else if !ev.LeaseValidUntil.After(now) {
				state, reason = "stale", "lease_expired"
			} else if !installationCurrent {
				state, reason = "stale", "installation_unverified"
			} else if !sourcesFresh {
				state, reason = "stale", "sources_stale"
			} else if c.State != "active" || c.Mode != "managed" {
				state, reason = "stale", "connection_inactive"
			} else if !c.PublicationID.Valid || c.PublicationID.UUID != pubID || row.PublicationID != pubID || row.RuntimeRevision != revision {
				state, reason = "stale", "runtime_revision_stale"
			}
			if ev.Event == "error" {
				reason = ev.ErrorCode
			}
			instance := TaskControlRuntimeInstance{RuntimeInstanceID: row.RuntimeInstanceID, PublicationID: row.PublicationID, RuntimeRevision: row.RuntimeRevision, RuntimeVersion: ev.RuntimeVersion, State: state, LastEvent: row.Event, LastSeenAt: row.ReceivedAt, LeaseValidUntil: ev.LeaseValidUntil, Reason: reason, CorrelationID: ev.CorrelationID}
			indices[row.RuntimeInstanceID] = len(instances)
			instances = append(instances, instance)
			if state == "active" {
				active++
				if ev.Event == "applied" || ev.Event == "heartbeat" {
					applied++
					if lastAck == nil {
						lastAck = map[string]any{"receivedAt": row.ReceivedAt, "actorId": row.ActorID, "runtimeInstanceId": row.RuntimeInstanceID, "runtimeRevision": row.RuntimeRevision, "version": ev.PublicationVersion, "payloadChecksum": ev.PayloadChecksum, "appliedAt": ev.OccurredAt}
					}
				}
			} else {
				stale++
			}
		}
		idx, ok := indices[row.RuntimeInstanceID]
		if !ok {
			continue
		}
		instance := &instances[idx]
		when := row.ReceivedAt
		// Event times refer to the same publication/revision as this instance's latest evidence.
		if row.PublicationID != instance.PublicationID || row.RuntimeRevision != instance.RuntimeRevision {
			continue
		}
		switch ev.Event {
		case "received":
			if instance.LastReceivedAt == nil {
				instance.LastReceivedAt = &when
			}
		case "applied":
			if instance.LastAppliedAt == nil {
				instance.LastAppliedAt = &when
			}
		case "heartbeat":
			if instance.LastHeartbeatAt == nil {
				instance.LastHeartbeatAt = &when
			}
		}
	}
	// Preserve the time of actual application when later heartbeats arrive.
	if lastAck != nil {
		ack := lastAck.(map[string]any)
		idx, ok := indices[ack["runtimeInstanceId"].(uuid.UUID)]
		if !ok || instances[idx].LastAppliedAt == nil {
			lastAck = nil
		} else {
			ack["appliedAt"] = instances[idx].LastAppliedAt
		}
	}
	runtimeState := "runtimeUnknown"
	if v2 {
		runtimeState = "notApplied"
		if !sourcesFresh {
			runtimeState = "stale"
		}
	}
	if applied > 0 {
		runtimeState = "appliedByRuntime"
	}
	out := map[string]any{"scope": taskControlScope(c), "publicationId": pubID, "publicationVersion": pub.Version, "publishedVersion": pub.Version, "runtimeState": runtimeState, "lastRuntimeAck": lastAck, "instances": instances, "activeInstances": active, "staleInstances": stale, "appliedInstances": applied, "observedAt": now, "evidenceTtlSeconds": 300, "retentionDays": 30, "coverage": "observedInstancesOnly", "allInstancesApplied": false, "truncated": len(rows) == 2000 || len(instances) >= 256, "issues": issues, "currentSnapshotId": snapshot, "currentRuntimeRevision": revision}
	if v2 && (!c.PublicationID.Valid || c.PublicationID.UUID != pubID) {
		out["currentSnapshotId"] = nil
		out["currentRuntimeRevision"] = nil
	}
	if !v2 && lastAck != nil {
		delete(lastAck.(map[string]any), "runtimeRevision")
	}
	if !v2 && lastAck == nil {
		delete(out, "lastRuntimeAck")
	}
	return out, nil
}
