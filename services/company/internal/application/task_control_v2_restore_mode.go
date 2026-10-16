package application

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type TaskControlRollbackInputV2 struct {
	RequestID                uuid.UUID `json:"requestId"`
	SourcePublicationID      uuid.UUID `json:"sourcePublicationId"`
	ExpectedPublishedVersion int64     `json:"expectedPublishedVersion"`
	ExpectedBindingRevision  int64     `json:"expectedBindingRevision"`
	ExpectedModeEpoch        int64     `json:"expectedModeEpoch"`
	Reason                   string    `json:"reason"`
}

func (s *Service) RollbackTaskControlV2(ctx context.Context, a Actor, id uuid.UUID, in TaskControlRollbackInputV2) (TaskControlPublicationV2, error) {
	var out TaskControlPublicationV2
	if in.RequestID == uuid.Nil || in.SourcePublicationID == uuid.Nil || utf8.RuneCountInString(in.Reason) > 1000 {
		return out, validation("Требуется публикация и ID операции")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = taskControlV2Required(c); e != nil {
		return out, e
	}
	hash := requestBodyHash([]any{"rollback", id, in})
	prior, e := q.GetTaskControlPublishRequest(ctx, db.GetTaskControlPublishRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID})
	if e == nil {
		if prior.BindingID != id || prior.BodyHash != hash {
			return out, conflict("Ключ операции уже использован")
		}
		e = json.Unmarshal(prior.Payload, &out)
		if out.CreatedBy != a.UserID {
			return TaskControlPublicationV2{}, forbidden("Операция принадлежит другому пользователю")
		}
		return out, e
	}
	if !isNoRows(e) {
		return out, e
	}
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return out, e
	}
	if !c.PublicationID.Valid {
		return out, conflict("Нет текущей публикации")
	}
	current, e := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: c.PublicationID.UUID})
	if e != nil {
		return out, e
	}
	if current.Version != in.ExpectedPublishedVersion {
		return out, conflict("Текущая публикация изменилась")
	}
	source, e := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: in.SourcePublicationID})
	if e != nil {
		return out, notFound("Публикация")
	}
	var meta struct {
		SchemaVersion int   `json:"schemaVersion"`
		DraftRevision int64 `json:"draftRevision"`
	}
	_ = json.Unmarshal(source.Payload, &meta)
	rules := []tc.IntentRule{}
	if meta.SchemaVersion == 2 {
		var pub TaskControlPublicationV2
		if e = json.Unmarshal(source.Payload, &pub); e != nil {
			return out, e
		}
		rules = pub.IntentRules
	} else {
		var pub TaskControlPublication
		if e = json.Unmarshal(source.Payload, &pub); e != nil {
			return out, e
		}
		for i := range pub.Rules {
			r := pub.Rules[i]
			rules = append(rules, tc.IntentRule{Kind: "legacyExplicit", Legacy: &r})
		}
	}
	input := TaskControlPublishInputV2{RequestID: in.RequestID, Reason: in.Reason}
	out, e = s.publishTaskControlIntentV2(ctx, q, c, a, input, rules, max(meta.DraftRevision, 1), hash, &in.SourcePublicationID)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

type TaskControlModePreviewInputV2 struct {
	TargetMode    string    `json:"targetMode"`
	PublicationID uuid.UUID `json:"publicationId"`
}
type TaskControlModeInputV2 struct {
	RequestID               uuid.UUID                  `json:"requestId"`
	ExpectedBindingRevision int64                      `json:"expectedBindingRevision"`
	ExpectedModeEpoch       int64                      `json:"expectedModeEpoch"`
	TargetMode              string                     `json:"targetMode"`
	PublicationID           uuid.UUID                  `json:"publicationId"`
	CoreAssertion           string                     `json:"coreAssertion"`
	ExportChecksum          string                     `json:"exportChecksum"`
	ExpectedSourceRevisions TaskControlSourceRevisions `json:"expectedSourceRevisions"`
	ConfirmDynamicLoss      bool                       `json:"confirmDynamicLoss"`
	ConfirmEmptyExport      bool                       `json:"confirmEmptyExport"`
}

func (s *Service) taskControlModePreviewTx(ctx context.Context, q *db.Queries, c db.TaskControlConnection, in TaskControlModePreviewInputV2) (map[string]any, error) {
	if in.TargetMode != "standalone" && in.TargetMode != "managed" {
		return nil, validation("Некорректный режим")
	}
	if !c.PublicationID.Valid || c.PublicationID.UUID != in.PublicationID {
		return nil, conflict("Текущая публикация изменилась")
	}
	row, e := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: in.PublicationID})
	if e != nil {
		return nil, e
	}
	var pub TaskControlPublicationV2
	if e = json.Unmarshal(row.Payload, &pub); e != nil || pub.SchemaVersion != 2 {
		return nil, coded(ErrorConflict, "unsupported_schema", "Требуется публикация версии 2")
	}
	resolved, src, fresh, e := s.taskControlResolveTx(ctx, q, c, pub.IntentRules, true)
	if e != nil {
		return nil, e
	}
	if resolved.HasBlockingIssues() {
		for _, issue := range resolved.Issues {
			if issue.Code == "runtime_payload_too_large" {
				return nil, coded(ErrorPayloadTooLarge, issue.Code, issue.Message)
			}
		}
		return nil, coded(ErrorValidation, "invalid_selection", "Невозможно материализовать недоступные ссылки")
	}
	loss := false
	active := 0
	for _, item := range pub.IntentRules {
		if item.Dynamic != nil && item.Dynamic.Enabled && !item.Dynamic.Archived {
			loss = true
			active++
		} else if item.Legacy != nil && item.Legacy.Enabled && !item.Legacy.Archived {
			active++
		}
	}
	if len(resolved.RuntimeRules) < active && len(resolved.RuntimeRules) > 0 {
		return nil, coded(ErrorValidation, "export_unrepresentable", "Некоторые правила имеют пустой состав и не представимы в автономных настройках")
	}
	return map[string]any{"scope": taskControlScope(c), "publicationId": pub.ID, "targetMode": in.TargetMode, "exportChecksum": resolved.ResolvedChecksum, "sourceRevisions": src, "runtimeRules": resolved.RuntimeRules, "dynamicLoss": loss, "emptyExport": len(resolved.RuntimeRules) == 0, "issues": resolved.Issues, "_sourceFreshUntil": fresh}, nil
}
func (s *Service) PreviewTaskControlModeV2(ctx context.Context, a Actor, id uuid.UUID, in TaskControlModePreviewInputV2) (any, error) {
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = taskControlV2Required(c); e != nil {
		return nil, e
	}
	preview, e := s.taskControlModePreviewTx(ctx, q, c, in)
	if e != nil {
		return nil, e
	}
	delete(preview, "_sourceFreshUntil")
	return preview, nil
}
func (s *Service) SetTaskControlModeV2(ctx context.Context, a Actor, id uuid.UUID, in TaskControlModeInputV2) (any, error) {
	if in.RequestID == uuid.Nil {
		return nil, validation("Требуется ID операции")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = taskControlV2Required(c); e != nil {
		return nil, e
	}
	hash := requestBodyHash([]any{id, in.ExpectedBindingRevision, in.ExpectedModeEpoch, in.TargetMode, in.PublicationID, in.ExportChecksum, in.ExpectedSourceRevisions, in.ConfirmDynamicLoss, in.ConfirmEmptyExport})
	prior, e := q.GetTaskControlConnectionRequest(ctx, db.GetTaskControlConnectionRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID})
	if e == nil {
		if prior.ActorID != a.UserID || prior.BindingID != id || prior.Action != "modeV2" || prior.BodyHash != hash {
			return nil, conflict("Ключ операции уже использован")
		}
		return json.RawMessage(prior.Response), nil
	}
	if !isNoRows(e) {
		return nil, e
	}
	if e = tcFence(c, in.ExpectedBindingRevision, in.ExpectedModeEpoch); e != nil {
		return nil, e
	}
	if c.Mode == in.TargetMode {
		return nil, conflict("Уже выбран этот режим")
	}
	preview, e := s.taskControlModePreviewTx(ctx, q, c, TaskControlModePreviewInputV2{in.TargetMode, in.PublicationID})
	if e != nil {
		return nil, e
	}
	if preview["exportChecksum"] != in.ExportChecksum || preview["sourceRevisions"] != in.ExpectedSourceRevisions {
		return nil, coded(ErrorConflict, "source_changed", "Источники экспорта изменились")
	}
	if in.TargetMode == "standalone" && ((preview["dynamicLoss"] == true && !in.ConfirmDynamicLoss) || (preview["emptyExport"] == true && !in.ConfirmEmptyExport)) {
		return nil, validation("Требуется явное подтверждение потери динамики и пустого экспорта")
	}
	proof, key, e := s.taskControlAssertion(in.CoreAssertion, "mode")
	if e != nil {
		return nil, e
	}
	if proof.CompanyID != c.CompanyID || proof.InstallationID != c.InstallationID || proof.IntegrationID != c.IntegrationID || proof.Generation != c.Generation || proof.AccountID != c.AccountID || proof.RequestID != in.RequestID {
		return nil, forbidden("Подтверждение относится к другому подключению")
	}
	fresh, _ := preview["_sourceFreshUntil"].(time.Time)
	if !fresh.After(s.now()) {
		return nil, coded(ErrorUpstream, "references_unavailable", "Источники экспорта устарели")
	}
	delete(preview, "_sourceFreshUntil")
	if e = s.consumeTC(ctx, q, proof, key); e != nil {
		return nil, e
	}
	old := c
	c, e = q.SetTaskControlMode(ctx, db.SetTaskControlModeParams{CompanyID: a.CompanyID, ID: id, Mode: in.TargetMode, BindingRevision: in.ExpectedBindingRevision, ModeEpoch: in.ExpectedModeEpoch})
	if e != nil {
		return nil, e
	}
	if e = q.DeleteTaskControlRuntimeSessions(ctx, db.DeleteTaskControlRuntimeSessionsParams{CompanyID: a.CompanyID, BindingID: id}); e != nil {
		return nil, e
	}
	if cached, err := q.GetTaskControlReferenceCache(ctx, db.GetTaskControlReferenceCacheParams{CompanyID: a.CompanyID, BindingID: id, ScopeKey: referenceScopeKey(old)}); err == nil {
		if e = q.SaveTaskControlReferenceCache(ctx, db.SaveTaskControlReferenceCacheParams{CompanyID: a.CompanyID, BindingID: id, ScopeKey: referenceScopeKey(c), Payload: cached.Payload, LastError: cached.LastError}); e != nil {
			return nil, e
		}
	}
	if in.TargetMode == "managed" {
		if err := s.taskControlV2DescriptorTx(ctx, q, c); err != nil {
			return nil, err
		}
		row, err := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: in.PublicationID})
		if err != nil {
			return nil, err
		}
		var pub TaskControlPublicationV2
		if err = json.Unmarshal(row.Payload, &pub); err != nil {
			return nil, err
		}
		resolved, src, fresh, err := s.taskControlResolveTx(ctx, q, c, pub.IntentRules, true)
		if err != nil {
			return nil, err
		}
		if resolved.HasBlockingIssues() {
			return nil, validation("Не удалось разрешить правила")
		}
		if _, err = s.saveTaskControlSnapshotV2(ctx, q, c, pub, resolved, src, fresh); err != nil {
			return nil, err
		}
	}
	out := map[string]any{"scope": taskControlScope(c), "state": "completed", "targetMode": in.TargetMode, "export": preview, "updatedAt": s.now().UTC(), "expiresAt": s.now().UTC().Add(time.Minute)}
	raw, _ := json.Marshal(out)
	if e = q.SaveTaskControlConnectionRequest(ctx, db.SaveTaskControlConnectionRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID, BindingID: id, ActorID: a.UserID, Action: "modeV2", BodyHash: hash, Response: raw}); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
