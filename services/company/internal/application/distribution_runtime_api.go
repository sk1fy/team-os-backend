package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"io"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type distributionRuleDTO struct {
	ExecutionMode          string     `json:"executionMode"`
	ExecutionEpoch         int64      `json:"executionEpoch"`
	LiveStartedAt          *time.Time `json:"liveStartedAt"`
	ID                     uuid.UUID  `json:"id"`
	BindingID              uuid.UUID  `json:"bindingId"`
	BindingRevision        int64      `json:"bindingRevision"`
	AccountID              string     `json:"accountId"`
	PipelineID             string     `json:"pipelineId"`
	StatusID               string     `json:"statusId"`
	GroupID                uuid.UUID  `json:"groupId"`
	Active                 bool       `json:"active"`
	KeepCurrentResponsible bool       `json:"keepCurrentResponsible"`
	Revision               int64      `json:"revision"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
	Source                 string     `json:"source"`
}

func ruleDTO(r db.DistributionRule) distributionRuleDTO {
	var live *time.Time
	if r.LiveStartedAt.Valid {
		live = &r.LiveStartedAt.Time
	}
	return distributionRuleDTO{r.ExecutionMode, r.ExecutionEpoch, live, r.ID, r.BindingID, r.BindingRevision, r.AccountID, r.PipelineID, r.StatusID, r.GroupID, r.Active, r.KeepCurrent, r.Revision, r.CreatedAt, r.UpdatedAt, r.Source}
}
func (s *Service) DistributionRuntimeRead(ctx context.Context, actor Actor, kind string, id uuid.UUID, limit, offset int32) (json.RawMessage, error) {
	if _, e := s.distributionActor(ctx, actor, false); e != nil {
		return nil, e
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, validation("Некорректная страница")
	}
	q := db.New(s.pool)
	var out any
	switch kind {
	case "settings":
		v, e := q.GetDistributionSettings(ctx, actor.CompanyID)
		if isNoRows(e) {
			return nil, notFound("Часовой пояс")
		}
		if e != nil {
			return nil, e
		}
		out = map[string]any{"timezone": v.Timezone, "revision": v.Revision}
	case "rules":
		rows, e := q.ListDistributionRules(ctx, db.ListDistributionRulesParams{CompanyID: actor.CompanyID, Limit: limit, Offset: offset})
		if e != nil {
			return nil, e
		}
		items := make([]distributionRuleDTO, 0, len(rows))
		for _, r := range rows {
			items = append(items, ruleDTO(r))
		}
		out = map[string]any{"items": items}
	case "observations":
		return s.DistributionObservations(ctx, actor, id, limit, offset)
	case "availability":
		r, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: id})
		if isNoRows(e) {
			return nil, notFound("Правило")
		}
		if e != nil {
			return nil, e
		}
		items, e := s.GetDistributionAvailability(ctx, actor, r.GroupID, id)
		if e != nil {
			return nil, e
		}
		out = map[string]any{"ruleId": id, "groupId": r.GroupID, "checkedAt": s.now(), "employees": items}
	case "queue":
		permissionCtx, cancelPermission := context.WithTimeout(ctx, 3*time.Second)
		defer cancelPermission()
		rows, e := q.ListDistributionQueue(ctx, db.ListDistributionQueueParams{CompanyID: actor.CompanyID, Limit: limit, Offset: offset})
		if e != nil {
			return nil, e
		}
		items := make([]any, 0, len(rows))
		for _, r := range rows {
			var op, employee *uuid.UUID
			var planned *time.Time
			if r.OperationID.Valid {
				v := r.OperationID.UUID
				op = &v
			}
			if r.PlannedEmployeeID.Valid {
				v := r.PlannedEmployeeID.UUID
				employee = &v
			}
			if r.PlannedAt.Valid {
				v := r.PlannedAt.Time
				planned = &v
			}
			items = append(items, map[string]any{"id": r.ID, "entryId": r.EntryID, "ruleId": r.RuleID, "groupId": r.GroupID, "accountId": r.AccountID, "leadId": s.visibleDistributionLead(permissionCtx, actor, r), "state": r.State, "reason": r.Reason, "nextAttemptAt": r.NextAttemptAt, "operationId": op, "plannedEmployeeId": employee, "plannedAt": planned, "createdAt": r.CreatedAt})
		}
		out = map[string]any{"items": items, "limit": limit, "offset": offset}
	case "history":
		row, e := q.LockDistributionQueue(ctx, id)
		if isNoRows(e) || e == nil && row.CompanyID != actor.CompanyID {
			return nil, notFound("Сделка очереди")
		}
		if e != nil {
			return nil, e
		}
		rows, e := q.ListDistributionQueueHistory(ctx, db.ListDistributionQueueHistoryParams{CompanyID: actor.CompanyID, ID: id, Limit: limit, Offset: offset})
		if e != nil {
			return nil, e
		}
		items := make([]any, 0, len(rows))
		for _, r := range rows {
			items = append(items, map[string]any{"id": r.ID, "state": r.State, "reason": r.Reason, "createdAt": r.CreatedAt, "payload": map[string]any{}})
		}
		out = map[string]any{"items": items, "limit": limit, "offset": offset}
	default:
		return nil, validation("Неизвестная операция")
	}
	return json.Marshal(out)
}
func decodeRuntime(raw json.RawMessage, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return validation("Некорректные параметры")
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return validation("Лишние параметры")
	}
	return nil
}
func (s *Service) DistributionRuntimeWrite(ctx context.Context, actor Actor, kind string, id uuid.UUID, raw json.RawMessage) (json.RawMessage, error) {
	if _, e := s.distributionActor(ctx, actor, true); e != nil {
		return nil, e
	}
	switch kind {
	case "action":
		return s.DistributionQueueAction(ctx, actor, id, raw)
	case "group":
		return s.ConfigureDistributionGroup(ctx, actor, id, raw)
	case "settings":
		var in struct {
			Timezone string `json:"timezone"`
		}
		if e := decodeRuntime(raw, &in); e != nil {
			return nil, e
		}
		v, e := s.SaveDistributionTimezone(ctx, actor, in.Timezone)
		if e != nil {
			return nil, e
		}
		return json.Marshal(map[string]any{"timezone": v.Timezone, "revision": v.Revision})
	case "rules":
		var in struct {
			BindingID       uuid.UUID `json:"bindingId"`
			ExecutionMode   *string   `json:"executionMode"`
			BindingRevision int64     `json:"bindingRevision"`
			GroupID         uuid.UUID `json:"groupId"`
			PipelineID      string    `json:"pipelineId"`
			StatusID        string    `json:"statusId"`
			Active          *bool     `json:"active"`
			Keep            *bool     `json:"keepCurrentResponsible"`
			Source          *string   `json:"source"`
		}
		if e := decodeRuntime(raw, &in); e != nil {
			return nil, e
		}
		b, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: in.BindingID})
		if isNoRows(e) {
			return nil, notFound("Связь")
		}
		if e != nil {
			return nil, e
		}
		active := false
		keep := true
		if in.Active != nil {
			active = *in.Active
		}
		if in.Keep != nil {
			keep = *in.Keep
		}
		mode := "live"
		if in.ExecutionMode != nil {
			mode = *in.ExecutionMode
		}
		if mode != "live" && mode != "observe" {
			return nil, validation("Неизвестный режим распределения")
		}
		source := "legacy_stage"
		if in.Source != nil {
			source = *in.Source
		}
		if source != "legacy_stage" && source != "creation" && source != "digital_pipeline" {
			return nil, validation("Неизвестный источник запуска")
		}
		r, e := s.CreateDistributionRuntimeRule(ctx, actor, db.CreateDistributionRuleParams{CompanyID: actor.CompanyID, BindingID: in.BindingID, BindingRevision: in.BindingRevision, AccountID: b.AccountID, GroupID: in.GroupID, PipelineID: in.PipelineID, StatusID: in.StatusID, Active: active, KeepCurrent: keep, ExecutionMode: mode, Source: source})
		if e != nil {
			return nil, e
		}
		return json.Marshal(ruleDTO(r))
	case "rule":
		var in struct {
			ExpectedRevision int64   `json:"expectedRevision"`
			ExecutionMode    *string `json:"executionMode"`
			PipelineID       *string `json:"pipelineId"`
			StatusID         *string `json:"statusId"`
			Active           *bool   `json:"active"`
			Keep             *bool   `json:"keepCurrentResponsible"`
			Source           *string `json:"source"`
		}
		if e := decodeRuntime(raw, &in); e != nil {
			return nil, e
		}
		initial, e := db.New(s.pool).GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: id})
		if isNoRows(e) {
			return nil, notFound("Правило")
		}
		if e != nil {
			return nil, e
		}
		if in.Source != nil && *in.Source != initial.Source {
			if *in.Source != "legacy_stage" && *in.Source != "creation" && *in.Source != "digital_pipeline" {
				return nil, validation("Неизвестный источник запуска")
			}
		}
		pipeline, status := initial.PipelineID, initial.StatusID
		if in.PipelineID != nil {
			pipeline = *in.PipelineID
		}
		if in.StatusID != nil {
			status = *in.StatusID
		}
		sourceWant := initial.Source
		if in.Source != nil {
			if *in.Source != "legacy_stage" && *in.Source != "creation" && *in.Source != "digital_pipeline" {
				return nil, validation("Неизвестный источник запуска")
			}
			sourceWant = *in.Source
		}
		changed := pipeline != initial.PipelineID || status != initial.StatusID
		var refs corebridge.References
		var binding db.DistributionBinding
		if changed {
			if !validCRMID(pipeline) || (sourceWant != "creation" && !validCRMID(status)) {
				return nil, validation("Укажите корректный этап amoCRM")
			}
			binding, e = db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: initial.BindingID})
			if e != nil {
				return nil, e
			}
			refs, e = s.readDistributionReferences(ctx, bindingScope(binding))
			if e != nil {
				return nil, e
			}
			found := false
			for _, p := range refs.Pipelines {
				if p.ID != pipeline {
					continue
				}
				if sourceWant == "creation" {
					found = true
					break
				}
				for _, st := range p.Statuses {
					if st.ID == status {
						found = true
					}
				}
			}
			if !found {
				return nil, validation("Этап amoCRM недоступен")
			}
		}
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			return nil, e
		}
		defer func() { _ = tx.Rollback(ctx) }()
		q := db.New(tx)
		if e = q.EnsureDistributionAvailabilityVersion(ctx, actor.CompanyID); e != nil {
			return nil, e
		}
		if _, e = q.LockDistributionAvailabilityVersion(ctx, actor.CompanyID); e != nil {
			return nil, e
		}
		if e = s.checkWidgetMutation(ctx); e != nil {
			return nil, e
		}
		if _, e = s.distributionActor(ctx, actor, true); e != nil {
			return nil, e
		}
		old, e := q.LockDistributionRule(ctx, db.LockDistributionRuleParams{CompanyID: actor.CompanyID, ID: id})
		if isNoRows(e) {
			return nil, notFound("Правило")
		}
		if e != nil {
			return nil, e
		}
		if old.Revision != initial.Revision {
			return nil, conflict("Правило изменилось: обновите данные")
		}
		source := sourceWant
		sourceChanged := false
		if in.Source != nil {
			source = *in.Source
			sourceChanged = source != old.Source
		}
		if sourceChanged {
			busy, err := q.DistributionRuleUnsettled(ctx, db.DistributionRuleUnsettledParams{CompanyID: actor.CompanyID, RuleID: id})
			if err != nil {
				return nil, err
			}
			if busy {
				return nil, conflict("Сначала завершите или отмените все ожидающие сделки правила")
			}
		}
		if in.ExecutionMode != nil {
			if *in.ExecutionMode != "live" && *in.ExecutionMode != "observe" {
				return nil, validation("Неизвестный режим распределения")
			}
			if old.ExecutionMode != *in.ExecutionMode {
				if old.ExecutionEpoch >= 9007199254740991 {
					return nil, conflict("Достигнут предел версий режима")
				}
				busy, err := q.DistributionRuleUnsettled(ctx, db.DistributionRuleUnsettledParams{CompanyID: actor.CompanyID, RuleID: id})
				if err != nil {
					return nil, err
				}
				if busy {
					return nil, conflict("Сначала завершите или отмените все ожидающие сделки правила")
				}
				if err = q.UpdateDistributionExecutionMode(ctx, db.UpdateDistributionExecutionModeParams{CompanyID: actor.CompanyID, ID: id, ExecutionMode: *in.ExecutionMode}); err != nil {
					return nil, err
				}
			}
		}
		if changed {
			used, e := q.DistributionRuleHasQueue(ctx, db.DistributionRuleHasQueueParams{CompanyID: actor.CompanyID, RuleID: id})
			if e != nil {
				return nil, e
			}
			observedUsed, err := q.DistributionRuleHasObservation(ctx, db.DistributionRuleHasObservationParams{CompanyID: actor.CompanyID, RuleID: id})
			if err != nil {
				return nil, err
			}
			if used || observedUsed {
				return nil, conflict("Правило уже использовалось: приостановите его и создайте новую группу для другого этапа")
			}
			current, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: old.BindingID})
			if e != nil {
				return nil, e
			}
			if current.State != "active" || bindingScope(current) != bindingScope(binding) || current.MappingRevision != binding.MappingRevision || current.MappingRevision != current.MappingAckRevision || !refs.FreshUntil.After(s.now()) {
				return nil, conflict("Связь или справочник изменились")
			}
		}
		if in.Active == nil || in.Keep == nil || !safeRevision(in.ExpectedRevision) {
			return nil, validation("Укажите текущую версию и настройки правила")
		}
		if *in.Active && (!old.Active || changed) {
			busy, e := q.DistributionPointHasUnfinishedOperation(ctx, db.DistributionPointHasUnfinishedOperationParams{AccountID: old.AccountID, PipelineID: pipeline, StatusID: status})
			if e != nil {
				return nil, e
			}
			if busy {
				return nil, conflict("Сначала завершите сверку операций этапа")
			}
		}
		if *in.Active {
			b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: old.BindingID})
			if e != nil {
				return nil, e
			}
			if b.State != "active" || b.Revision != old.BindingRevision {
				return nil, validation("Связь недоступна")
			}
		}
		r, e := q.UpdateDistributionRulePoint(ctx, db.UpdateDistributionRulePointParams{CompanyID: actor.CompanyID, ID: id, PipelineID: pipeline, StatusID: status, Active: *in.Active, KeepCurrent: *in.Keep, Revision: in.ExpectedRevision, Source: source})
		if isNoRows(e) {
			return nil, conflict("Правило изменилось: обновите данные")
		}
		if isUniqueViolation(e) {
			return nil, conflict("Для этого этапа уже есть активное правило")
		}
		if e != nil {
			return nil, e
		}
		if e = tx.Commit(ctx); e != nil {
			return nil, e
		}
		return json.Marshal(ruleDTO(r))
	default:
		return nil, validation("Неизвестная операция")
	}
}

func (s *Service) visibleDistributionLead(ctx context.Context, actor Actor, row db.DistributionQueue) *string {
	if s.distributionCore == nil {
		return nil
	}
	r, e := db.New(s.pool).GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: actor.CompanyID, ID: row.RuleID})
	if e != nil {
		return nil
	}
	b, e := db.New(s.pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: r.BindingID})
	if e != nil || b.Revision != r.BindingRevision || b.AccountID != row.AccountID {
		return nil
	}
	permission, e := s.DistributionLeadPermission(ctx, actor, r.BindingID, row.LeadID)
	if e != nil || !permission.CanViewLead {
		return nil
	}
	lead := row.LeadID
	return &lead
}
