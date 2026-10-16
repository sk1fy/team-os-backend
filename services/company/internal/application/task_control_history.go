package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type TaskControlHistoryOptions struct {
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}
type TaskControlHistoryEvent struct {
	ID        uuid.UUID        `json:"id"`
	Sequence  string           `json:"sequence"`
	EventType string           `json:"eventType"`
	Scope     TaskControlScope `json:"scope"`
	ActorID   *uuid.UUID       `json:"actorId"`
	CreatedAt time.Time        `json:"createdAt"`
	Details   json.RawMessage  `json:"details"`
}
type taskControlPageCursor struct {
	CompanyID uuid.UUID `json:"companyId"`
	BindingID uuid.UUID `json:"bindingId"`
	Kind      string    `json:"kind"`
	HighWater int64     `json:"highWater,string"`
	Before    int64     `json:"before,string"`
}

func tcPageOptions(company, binding uuid.UUID, kind, raw string, limit int) (taskControlPageCursor, int, error) {
	c := taskControlPageCursor{CompanyID: company, BindingID: binding, Kind: kind, Before: math.MaxInt64}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return c, 0, validation("Некорректный размер страницы")
	}
	if raw != "" {
		if len(raw) > 1024 {
			return c, 0, validation("Некорректный курсор")
		}
		b, e := base64.RawURLEncoding.DecodeString(raw)
		if e != nil {
			return c, 0, validation("Некорректный курсор")
		}
		var decoded taskControlPageCursor
		if json.Unmarshal(b, &decoded) != nil || decoded.CompanyID != company || decoded.BindingID != binding || decoded.Kind != kind || decoded.HighWater < 0 || decoded.Before < 1 || decoded.Before > decoded.HighWater {
			return c, 0, validation("Курсор относится к другой истории")
		}
		c = decoded
	}
	return c, limit, nil
}
func tcPageToken(c taskControlPageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Service) TaskControlHistory(ctx context.Context, a Actor, id uuid.UUID, opts TaskControlHistoryOptions) (any, error) {
	tx, q, _, e := s.taskControlRulesTx(ctx, a, id, false)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	c, limit, e := tcPageOptions(a.CompanyID, id, "history", opts.Cursor, opts.Limit)
	if e != nil {
		return nil, e
	}
	if opts.Cursor == "" {
		c.HighWater, e = q.GetTaskControlAuditHighWater(ctx, db.GetTaskControlAuditHighWaterParams{CompanyID: a.CompanyID, BindingID: id})
		if e != nil {
			return nil, e
		}
	}
	rows, e := q.ListTaskControlAuditEvents(ctx, db.ListTaskControlAuditEventsParams{CompanyID: a.CompanyID, BindingID: id, HighWater: c.HighWater, BeforeSequence: c.Before, PageLimit: int32(limit + 1)})
	if e != nil {
		return nil, e
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := make([]TaskControlHistoryEvent, 0, len(rows))
	size := 32
	var lastSequence int64
	for _, r := range rows {
		var scope TaskControlScope
		if e = json.Unmarshal(r.Scope, &scope); e != nil {
			return nil, e
		}
		var actor *uuid.UUID
		if r.ActorID.Valid {
			v := r.ActorID.UUID
			actor = &v
		}
		item := TaskControlHistoryEvent{r.ID, strconv.FormatInt(r.Sequence, 10), r.EventType, scope, actor, r.CreatedAt, r.Details}
		raw, _ := json.Marshal(item)
		if size+len(raw) > 1000*1024 {
			more = true
			break
		}
		size += len(raw) + 1
		items = append(items, item)
		lastSequence = r.Sequence
	}
	out := map[string]any{"items": items}
	if more {
		c.Before = lastSequence
		out["nextCursor"] = tcPageToken(c)
	}
	return out, nil
}
func (s *Service) taskControlPublicationsPage(ctx context.Context, a Actor, id uuid.UUID, opts TaskControlPublicationOptionsV2, v2 bool) (any, error) {
	tx, q, connection, e := s.taskControlRulesTx(ctx, a, id, false)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if !v2 && connection.RuntimeSchemaVersion == 2 {
		return nil, taskControlRequiresV2()
	}
	kind := "publications-v1"
	if v2 {
		kind = "publications-v2"
	}
	c, limit, e := tcPageOptions(a.CompanyID, id, kind, opts.Cursor, opts.Limit)
	// UUID cursors issued by the original v2 endpoint remain usable.
	if e != nil && v2 {
		if old, pe := uuid.Parse(opts.Cursor); pe == nil {
			row, re := q.GetTaskControlPublication(ctx, db.GetTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: old})
			if re != nil {
				return nil, notFound("Курсор публикации")
			}
			c, limit, e = tcPageOptions(a.CompanyID, id, kind, "", opts.Limit)
			c.Before = row.Version
		}
	}
	if e != nil {
		return nil, e
	}
	if c.HighWater == 0 {
		c.HighWater, e = q.GetTaskControlPublicationHighWater(ctx, db.GetTaskControlPublicationHighWaterParams{CompanyID: a.CompanyID, BindingID: id})
		if e != nil {
			return nil, e
		}
	}

	// Fetch bounded chunks, because mixed v1/v2 histories may contain skipped versions.
	items := []json.RawMessage{}
	size := 32
	more := false
	last := c.Before
	for {
		rows, re := q.ListTaskControlPublicationsPage(ctx, db.ListTaskControlPublicationsPageParams{CompanyID: a.CompanyID, BindingID: id, HighWater: c.HighWater, BeforeVersion: last, SchemaVersion: map[bool]string{false: "1", true: "2"}[v2], PageLimit: 10})
		if re != nil {
			return nil, re
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			var m struct {
				SchemaVersion int `json:"schemaVersion"`
			}
			if json.Unmarshal(r.Payload, &m) != nil {
				return nil, internal("Некорректная публикация", nil)
			}
			if (m.SchemaVersion == 2) != v2 {
				last = r.Version
				continue
			}
			if len(items) == limit || size+len(r.Payload) > 4*1024*1024 {
				more = true
				break
			}
			items = append(items, r.Payload)
			size += len(r.Payload) + 1
			last = r.Version
		}
		if more || len(rows) < 10 {
			break
		}
	}
	if more && len(items) == 0 {
		return nil, coded(ErrorPayloadTooLarge, "task_control_history_payload_too_large", "Публикация превышает лимит страницы истории")
	}
	out := map[string]any{"items": items}
	if more {
		c.Before = last
		out["nextCursor"] = tcPageToken(c)
	}
	return out, nil
}
func (s *Service) RollbackTaskControl(ctx context.Context, a Actor, id uuid.UUID, in TaskControlRollbackInputV2) (TaskControlPublication, error) {
	var out TaskControlPublication
	if in.RequestID == uuid.Nil || in.SourcePublicationID == uuid.Nil || strings.TrimSpace(in.Reason) == "" || utf8.RuneCountInString(in.Reason) > 1000 {
		return out, validation("Требуются публикация, ID операции и причина восстановления")
	}
	tx, q, c, e := s.taskControlRulesTx(ctx, a, id, true)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if c.RuntimeSchemaVersion == 2 {
		return out, taskControlRequiresV2()
	}
	hash := requestBodyHash([]any{"rollback-v1", id, in})
	prior, e := q.GetTaskControlPublishRequest(ctx, db.GetTaskControlPublishRequestParams{CompanyID: a.CompanyID, RequestID: in.RequestID})
	if e == nil {
		if prior.BindingID != id || prior.BodyHash != hash {
			return out, conflict("Ключ операции уже использован")
		}
		e = json.Unmarshal(prior.Payload, &out)
		if out.CreatedBy != a.UserID {
			return TaskControlPublication{}, forbidden("Операция принадлежит другому пользователю")
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
	if e = json.Unmarshal(source.Payload, &out); e != nil {
		return out, e
	}
	if out.SchemaVersion != 1 {
		return TaskControlPublication{}, coded(ErrorConflict, "unsupported_schema", "Требуется восстановление через API версии 2")
	}
	if issues := tc.ValidateRules(out.Rules); len(issues) > 0 {
		return TaskControlPublication{}, validation(issues[0].Message)
	}
	if taskControlRulesNeedReferences(out.Rules) {
		refs, re := s.taskControlFreshReferencesTx(ctx, q, c)
		if re != nil {
			return TaskControlPublication{}, re
		}
		if issues := tc.ValidateReferenceRules(out.Rules, refs); len(issues) > 0 {
			return TaskControlPublication{}, coded(ErrorValidation, issues[0].Code, issues[0].Message)
		}
	}
	version, e := q.GetTaskControlPublicationHighWater(ctx, db.GetTaskControlPublicationHighWaterParams{CompanyID: a.CompanyID, BindingID: id})
	if e != nil {
		return out, e
	}
	if version >= 9007199254740991 {
		return out, conflict("Исчерпан счётчик публикаций")
	}
	out.ID = uuid.New()
	out.Version = version + 1
	out.Scope = taskControlScope(c)
	out.CreatedAt = s.now().UTC()
	out.CreatedBy = a.UserID
	out.Reason = strings.TrimSpace(in.Reason)
	out.RestoredFromID = &in.SourcePublicationID
	raw, _ := json.Marshal(out)
	if _, e = q.CreateTaskControlPublication(ctx, db.CreateTaskControlPublicationParams{CompanyID: a.CompanyID, BindingID: id, ID: out.ID, RequestID: in.RequestID, BodyHash: hash, Payload: raw}); e != nil {
		return out, e
	}
	if _, e = q.SetTaskControlPublication(ctx, db.SetTaskControlPublicationParams{CompanyID: a.CompanyID, ID: id, PublicationID: uuid.NullUUID{UUID: out.ID, Valid: true}}); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
