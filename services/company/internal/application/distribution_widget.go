package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// DistributionWidgetRuntimeInput carries only Core-verified Scope/UserID, never browser authority.
type DistributionWidgetRuntimeInput struct {
	corebridge.Scope
	PrincipalExpiresAt time.Time       `json:"principalExpiresAt"`
	UserID             string          `json:"userId"`
	Kind               string          `json:"kind"`
	ID                 uuid.UUID       `json:"id"`
	GroupID            uuid.UUID       `json:"groupId"`
	LeadID             string          `json:"leadId"`
	Limit              int32           `json:"limit"`
	Offset             int32           `json:"offset"`
	Write              bool            `json:"write"`
	RequestID          uuid.UUID       `json:"requestId"`
	Payload            json.RawMessage `json:"payload"`
}

// DistributionDPCore is the optional Core bridge that issues the per-group
// Digital Pipeline credential. Implemented by corebridge.Client.
type DistributionDPCore interface {
	DPCredential(context.Context, corebridge.Scope, uuid.UUID) (corebridge.DPCredential, error)
}

func (s *Service) widgetRuntimeActor(ctx context.Context, in DistributionWidgetRuntimeInput) (Actor, error) {
	a, e := s.DistributionWidgetAccess(ctx, DistributionWidgetAccessInput{Scope: in.Scope, UserID: in.UserID, LeadID: in.LeadID})
	if e != nil {
		return Actor{}, e
	}
	if !a.Allowed {
		return Actor{}, forbidden("Нет доступа к распределению")
	}
	return s.distributionActor(ctx, Actor{CompanyID: in.CompanyID, UserID: a.EmployeeID}, in.Write)
}
func (s *Service) widgetRuntimeResource(ctx context.Context, in DistributionWidgetRuntimeInput) error {
	q := db.New(s.pool)
	switch in.Kind {
	case "rule", "availability", "observations":
		r, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: in.CompanyID, ID: in.ID})
		if e != nil {
			return notFound("Правило")
		}
		if r.BindingID != in.BindingID || r.BindingRevision != in.BindingRevision {
			return forbidden("Правило другого подключения")
		}
	case "group":
		_, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: in.CompanyID, ID: in.ID})
		if e != nil {
			return notFound("Группа")
		}
		other, e := q.DistributionWidgetGroupOtherBinding(ctx, db.DistributionWidgetGroupOtherBindingParams{CompanyID: in.CompanyID, GroupID: in.ID, BindingID: in.BindingID})
		if e != nil {
			return e
		}
		if other {
			return forbidden("Группа используется другим подключением")
		}
	case "dp_settings":
		// A paused group must stay configurable, so only ownership and binding
		// scope are checked here; group activity remains admission policy.
		if in.GroupID == uuid.Nil {
			return validation("Укажите группу")
		}
		if _, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: in.CompanyID, ID: in.GroupID}); e != nil {
			return notFound("Группа")
		}
		other, e := q.DistributionWidgetGroupOtherBinding(ctx, db.DistributionWidgetGroupOtherBindingParams{CompanyID: in.CompanyID, GroupID: in.GroupID, BindingID: in.BindingID})
		if e != nil {
			return e
		}
		if other {
			return forbidden("Группа используется другим подключением")
		}
	case "history", "action":
		r, e := q.LockDistributionQueue(ctx, in.ID)
		if e != nil || r.CompanyID != in.CompanyID || r.LeadID != in.LeadID {
			return notFound("Сделка очереди")
		}
		rule, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: in.CompanyID, ID: r.RuleID})
		if e != nil || rule.BindingID != in.BindingID || rule.BindingRevision != in.BindingRevision {
			return forbidden("Сделка другого подключения")
		}
	}
	return nil
}
func (s *Service) DistributionWidgetRuntime(ctx context.Context, in DistributionWidgetRuntimeInput) (json.RawMessage, error) {
	if in.PrincipalExpiresAt.IsZero() || !s.now().Before(in.PrincipalExpiresAt) || in.PrincipalExpiresAt.After(s.now().Add(15*time.Minute)) {
		return nil, forbidden("Срок авторизации истёк")
	}
	ctx, cancel := context.WithDeadline(ctx, in.PrincipalExpiresAt)
	defer cancel()
	actor, e := s.widgetRuntimeActor(ctx, in)
	if e != nil {
		return nil, e
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 || in.Offset > 100000 {
		return nil, validation("Некорректная страница")
	}
	if (in.Kind == "lead" || in.Kind == "history" || in.Kind == "action") && !validCRMID(in.LeadID) {
		return nil, validation("Укажите сделку")
	}
	if e = s.widgetRuntimeResource(ctx, in); e != nil {
		return nil, e
	}
	q := db.New(s.pool)
	if !in.Write {
		switch in.Kind {
		case "observations":
			return s.DistributionObservations(ctx, actor, in.ID, in.Limit, in.Offset)
		case "references":
			refs, e := s.readDistributionReferences(ctx, in.Scope)
			if e != nil {
				return nil, e
			}
			employees, e := q.ListDistributionWidgetEmployees(ctx, db.ListDistributionWidgetEmployeesParams{CompanyID: in.CompanyID, BindingID: in.BindingID})
			if e != nil {
				return nil, e
			}
			items := []any{}
			for _, u := range employees {
				items = append(items, map[string]any{"id": u.ID, "name": u.Name})
			}
			return json.Marshal(map[string]any{"users": refs.Users, "pipelines": refs.Pipelines, "employees": items, "checkedAt": s.now()})
		case "settings":
			return s.DistributionRuntimeRead(ctx, actor, "settings", uuid.Nil, in.Limit, in.Offset)
		case "rules":
			rows, e := q.ListDistributionWidgetRules(ctx, db.ListDistributionWidgetRulesParams{CompanyID: in.CompanyID, BindingID: in.BindingID, BindingRevision: in.BindingRevision, Limit: in.Limit, Offset: in.Offset})
			if e != nil {
				return nil, e
			}
			items := make([]distributionRuleDTO, 0, len(rows))
			for _, r := range rows {
				items = append(items, ruleDTO(r))
			}
			return json.Marshal(map[string]any{"items": items})
		case "groups":
			rows, e := q.ListDistributionGroups(ctx, in.CompanyID)
			if e != nil {
				return nil, e
			}
			items := []any{}
			for _, r := range rows {
				other, e := q.DistributionWidgetGroupOtherBinding(ctx, db.DistributionWidgetGroupOtherBindingParams{CompanyID: in.CompanyID, GroupID: r.ID, BindingID: in.BindingID})
				if e != nil {
					return nil, e
				}
				if !other {
					items = append(items, map[string]any{"id": r.ID, "name": r.Name, "description": textPointer(r.Description), "active": r.Active, "algorithm": r.Algorithm, "memberIds": r.MemberIds, "disabledMemberIds": r.DisabledMemberIds, "revision": r.Revision})
				}
			}
			return json.Marshal(map[string]any{"items": items})
		case "availability":
			return s.DistributionRuntimeRead(ctx, actor, "availability", in.ID, in.Limit, in.Offset)
		case "history":
			return s.DistributionRuntimeRead(ctx, actor, "history", in.ID, in.Limit, in.Offset)
		case "lead":
			rows, e := q.ListDistributionWidgetLeadQueue(ctx, db.ListDistributionWidgetLeadQueueParams{CompanyID: in.CompanyID, BindingID: in.BindingID, BindingRevision: in.BindingRevision, LeadID: in.LeadID, Limit: in.Limit, Offset: in.Offset})
			if e != nil {
				return nil, e
			}
			items := []any{}
			for _, r := range rows {
				detail, e := s.DistributionQueueDetail(ctx, actor, r.ID)
				if e != nil {
					return nil, e
				}
				items = append(items, detail)
			}
			actual, e := s.widgetCurrentLead(ctx, actor, in.Scope, in.LeadID)
			if e != nil {
				return nil, e
			}
			latest, e := s.latestLeadObservation(ctx, actor, in.Scope, in.LeadID)
			if e != nil {
				return nil, e
			}
			return json.Marshal(map[string]any{"items": items, "checkedAt": s.now(), "latestObservation": latest, "currentLead": actual})
		default:
			return nil, validation("Неизвестная операция чтения")
		}
	}
	if in.RequestID == uuid.Nil {
		return nil, validation("Укажите идентификатор запроса")
	}
	switch in.Kind {
	case "rules", "rule", "group", "action", "dp_settings":
	default:
		return nil, validation("Неизвестная операция записи")
	}
	payload := in.Payload
	if in.Kind == "rules" {
		var obj map[string]json.RawMessage
		if e = decodeRuntime(payload, &obj); e != nil {
			return nil, e
		}
		if obj == nil {
			return nil, validation("Укажите параметры правила")
		}
		if _, ok := obj["bindingId"]; ok {
			return nil, validation("Связь задаёт сервер")
		}
		if _, ok := obj["bindingRevision"]; ok {
			return nil, validation("Версию связи задаёт сервер")
		}
		obj["bindingId"], _ = json.Marshal(in.BindingID)
		obj["bindingRevision"], _ = json.Marshal(in.BindingRevision)
		payload, _ = json.Marshal(obj)
		var group struct {
			GroupID uuid.UUID `json:"groupId"`
		}
		_ = json.Unmarshal(payload, &group)
		check := in
		check.Kind = "group"
		check.ID = group.GroupID
		if e = s.widgetRuntimeResource(ctx, check); e != nil {
			return nil, e
		}
	}
	if in.Kind == "action" {
		var action distributionUIAction
		if e = decodeRuntime(payload, &action); e != nil {
			return nil, e
		}
		if action.RequestID != in.RequestID {
			return nil, validation("Идентификатор действия не совпадает")
		}
	}
	identity := in
	identity.PrincipalExpiresAt = time.Time{}
	canonical, e := json.Marshal(identity)
	if e != nil {
		return nil, e
	}
	hash := sha256.Sum256(canonical)
	claimed, e := q.ClaimDistributionWidgetRequest(ctx, db.ClaimDistributionWidgetRequestParams{CompanyID: in.CompanyID, RequestID: in.RequestID, BindingID: in.BindingID, EmployeeID: actor.UserID, RequestHash: hash[:]})
	if e != nil {
		return nil, e
	}
	if claimed == 0 {
		saved, e := q.GetDistributionWidgetRequest(ctx, db.GetDistributionWidgetRequestParams{CompanyID: in.CompanyID, RequestID: in.RequestID})
		if e != nil {
			return nil, e
		}
		if saved.EmployeeID != actor.UserID || saved.BindingID != in.BindingID || !bytes.Equal(saved.RequestHash, hash[:]) {
			return nil, conflict("Идентификатор запроса уже использован")
		}
		if saved.State == "completed" {
			if in.Kind == "dp_settings" {
				// The secret is never persisted; Core re-issues the same
				// idempotent credential for this scope.
				return s.dpSettingsResponse(ctx, actor, in)
			}
			return saved.Response, nil
		}
		if saved.State == "rejected" {
			kind, _ := strconv.Atoi(saved.ErrorKind.String)
			return nil, &Error{Kind: ErrorKind(kind), Message: saved.ErrorMessage.String}
		}
		return json.Marshal(map[string]any{"requestId": in.RequestID, "state": "outcome_unknown", "retryAllowed": false})
	}
	// Durable pending before mutation; uncertain results are never automatically executed twice.
	if _, e = s.widgetRuntimeActor(ctx, in); e != nil {
		return nil, e
	}
	ctx = context.WithValue(ctx, distributionWidgetMutationKey{}, in)
	var result json.RawMessage
	var err error
	if in.Kind == "dp_settings" {
		result, err = s.dpSettingsResponse(ctx, actor, in)
	} else {
		result, err = s.DistributionRuntimeWrite(ctx, actor, in.Kind, in.ID, payload)
	}
	state := "completed"
	var errorKind, errorMessage pgtype.Text
	if err != nil {
		var app *Error
		if !errors.As(err, &app) || app.Kind == ErrorInternal || app.Kind == ErrorUpstream {
			return nil, err
		}
		state = "rejected"
		errorKind = pgtype.Text{String: strconv.Itoa(int(app.Kind)), Valid: true}
		errorMessage = pgtype.Text{String: app.Message, Valid: true}
	}
	persisted := result
	if in.Kind == "dp_settings" {
		// Never store the DP credential in the idempotency result.
		persisted = json.RawMessage(`{"state":"connected","redacted":true}`)
	}
	if _, e = q.CompleteDistributionWidgetRequest(ctx, db.CompleteDistributionWidgetRequestParams{CompanyID: in.CompanyID, RequestID: in.RequestID, State: state, Response: persisted, ErrorKind: errorKind, ErrorMessage: errorMessage}); e != nil {
		return nil, e
	}
	return result, err
}

// dpSettingsResponse issues the per-group Digital Pipeline credential via Core.
// It is deterministic for a scope, so replay stays safe without persisting the
// plaintext key anywhere on the TeamOS side.
func (s *Service) dpSettingsResponse(ctx context.Context, actor Actor, in DistributionWidgetRuntimeInput) (json.RawMessage, error) {
	g, e := db.New(s.pool).GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: actor.CompanyID, ID: in.GroupID})
	if isNoRows(e) {
		return nil, notFound("Группа")
	}
	if e != nil {
		return nil, e
	}
	core, ok := s.deliveryCore.(DistributionDPCore)
	if !ok || core == nil {
		return nil, upstream("Core недоступен", nil)
	}
	cred, e := core.DPCredential(ctx, in.Scope, in.GroupID)
	if e != nil {
		return nil, coreError(e)
	}
	if len(cred.Key) < 6 || cred.Key[:3] != "dp_" {
		return nil, upstream("Core не выдал ключ", nil)
	}
	return json.Marshal(map[string]any{"state": "connected", "groupId": in.GroupID.String(), "groupName": g.Name, "key": cred.Key})
}

type distributionWidgetMutationKey struct{}

func (s *Service) checkWidgetMutation(ctx context.Context) error {
	in, ok := ctx.Value(distributionWidgetMutationKey{}).(DistributionWidgetRuntimeInput)
	if !ok {
		return nil
	}
	if !s.now().Before(in.PrincipalExpiresAt) {
		return forbidden("Срок авторизации истёк")
	}
	if _, e := s.widgetRuntimeActor(ctx, in); e != nil {
		return e
	}
	return s.widgetRuntimeResource(ctx, in)
}
