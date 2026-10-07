package transport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

// Required fields must remain distinguishable from valid false values.
func runtimeDecode(w http.ResponseWriter, r *http.Request, dst any, required ...string) bool {
	var raw json.RawMessage
	if !decodeStrict(w, r, &raw) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		apierror.Write(w, apierror.New(http.StatusBadRequest, "Ожидается объект параметров"))
		return false
	}
	for _, name := range required {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			apierror.Write(w, apierror.New(http.StatusBadRequest, "Отсутствует обязательный параметр: "+name))
			return false
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		apierror.Write(w, apierror.New(http.StatusBadRequest, "Некорректные параметры распределения"))
		return false
	}
	return true
}

func runtimePage(w http.ResponseWriter, limit, offset *int32) (int32, int32, bool) {
	l, o := int32(50), int32(0)
	if limit != nil {
		l = *limit
	}
	if offset != nil {
		o = *offset
	}
	if l < 1 || l > 100 || o < 0 || o > 100000 {
		apierror.Write(w, apierror.New(http.StatusBadRequest, "Недопустимые параметры страницы"))
		return 0, 0, false
	}
	return l, o, true
}
func runtimeOutput(w http.ResponseWriter, out interface{ GetPayload() []byte }, dst any) {
	if out == nil || len(out.GetPayload()) > 4<<20 || !json.Valid(out.GetPayload()) || bytes.Equal(bytes.TrimSpace(out.GetPayload()), []byte("null")) || json.Unmarshal(out.GetPayload(), dst) != nil {
		apierror.Write(w, apierror.New(http.StatusBadGateway, "Некорректный ответ сервиса распределения"))
		return
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusOK, dst)
}
func (h *Handler) readRuntime(w http.ResponseWriter, r *http.Request, kind string, id uuid.UUID, l, o int32, dst any) {
	key := ""
	if id != uuid.Nil {
		key = id.String()
	}
	out, e := h.company.ReadDistributionRuntime(outgoingContext(r), &v.ReadDistributionRuntimeRequest{Kind: kind, Id: key, Limit: l, Offset: o})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	runtimeOutput(w, out, dst)
}
func (h *Handler) writeRuntime(w http.ResponseWriter, r *http.Request, kind string, id uuid.UUID, in, dst any) {
	raw, e := json.Marshal(in)
	if e != nil {
		apierror.Write(w, apierror.New(http.StatusBadRequest, "Некорректные параметры распределения"))
		return
	}
	key := ""
	if id != uuid.Nil {
		key = id.String()
	}
	out, e := h.company.WriteDistributionRuntime(outgoingContext(r), &v.WriteDistributionRuntimeRequest{Kind: kind, Id: key, Payload: raw})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	runtimeOutput(w, out, dst)
}
func (h *Handler) GetDistributionSettings(w http.ResponseWriter, r *http.Request) {
	h.readRuntime(w, r, "settings", uuid.Nil, 0, 0, &api.DistributionRuntimeSettings{})
}
func (h *Handler) SetDistributionSettings(w http.ResponseWriter, r *http.Request) {
	var in api.DistributionTimezoneInput
	if !runtimeDecode(w, r, &in, "timezone") {
		return
	}
	h.writeRuntime(w, r, "settings", uuid.Nil, in, &api.DistributionRuntimeSettings{})
}
func (h *Handler) GetDistributionRules(w http.ResponseWriter, r *http.Request, p api.GetDistributionRulesParams) {
	l, o, ok := runtimePage(w, p.Limit, p.Offset)
	if !ok {
		return
	}
	h.readRuntime(w, r, "rules", uuid.Nil, l, o, &api.DistributionRuntimeRules{})
}
func (h *Handler) CreateDistributionRule(w http.ResponseWriter, r *http.Request) {
	var in api.DistributionRuleCreateInput
	if !runtimeDecode(w, r, &in, "bindingId", "bindingRevision", "groupId") {
		return
	}
	h.writeRuntime(w, r, "rules", uuid.Nil, in, &api.DistributionRuntimeRule{})
}
func (h *Handler) UpdateDistributionRule(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.DistributionRuleUpdateInput
	if !runtimeDecode(w, r, &in, "expectedRevision", "active", "keepCurrentResponsible") {
		return
	}
	h.writeRuntime(w, r, "rule", id, in, &api.DistributionRuntimeRule{})
}
func (h *Handler) GetDistributionAvailability(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.readRuntime(w, r, "availability", id, 0, 0, &api.DistributionRuntimeAvailability{})
}
func (h *Handler) GetDistributionQueue(w http.ResponseWriter, r *http.Request, p api.GetDistributionQueueParams) {
	l, o, ok := runtimePage(w, p.Limit, p.Offset)
	if !ok {
		return
	}
	request := &v.ReadDistributionRuntimeRequest{Kind: "queue", Limit: l, Offset: o}
	if p.Tab != nil {
		request.Tab = string(*p.Tab)
	}
	if p.GroupId != nil {
		request.GroupId = p.GroupId.String()
	}
	if p.From != nil {
		request.From = p.From.Format(time.RFC3339Nano)
	}
	if p.To != nil {
		request.To = p.To.Format(time.RFC3339Nano)
	}
	out, e := h.company.ReadDistributionRuntime(outgoingContext(r), request)
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	runtimeOutput(w, out, &api.DistributionRuntimeQueue{})
}
func (h *Handler) GetDistributionQueueHistory(w http.ResponseWriter, r *http.Request, id api.ID, p api.GetDistributionQueueHistoryParams) {
	l, o, ok := runtimePage(w, p.Limit, p.Offset)
	if !ok {
		return
	}
	h.readRuntime(w, r, "history", id, l, o, &api.DistributionRuntimeHistory{})
}

func (h *Handler) GetDistributionSummary(w http.ResponseWriter, r *http.Request, p api.GetDistributionSummaryParams) {
	request := &v.ReadDistributionRuntimeRequest{Kind: "summary"}
	if p.GroupId != nil {
		request.GroupId = p.GroupId.String()
	}
	out, e := h.company.ReadDistributionRuntime(outgoingContext(r), request)
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	runtimeOutput(w, out, &api.DistributionRuntimeSummary{})
}
func (h *Handler) GetDistributionQueueItem(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.readRuntime(w, r, "detail", id, 0, 0, &api.DistributionRuntimeQueueItem{})
}
func (h *Handler) ActOnDistributionQueue(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.DistributionQueueActionInput
	if !runtimeDecode(w, r, &in, "action", "requestId", "expectedUpdatedAt") {
		return
	}
	h.writeRuntime(w, r, "action", id, in, &api.DistributionRuntimeQueueItem{})
}
func (h *Handler) ConfigureDistributionGroup(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.DistributionGroupConfigurationInput
	if !runtimeDecode(w, r, &in, "expectedRevision", "name", "memberIds", "disabledMemberIds", "active", "algorithm") {
		return
	}
	h.writeRuntime(w, r, "group", id, in, &api.DealDistributionGroup{})
}

func (h *Handler) GetDistributionObservations(w http.ResponseWriter, r *http.Request, id api.ID, p api.GetDistributionObservationsParams) {
	l, o, ok := runtimePage(w, p.Limit, p.Offset)
	if !ok {
		return
	}
	h.readRuntime(w, r, "observations", id, l, o, &api.DistributionRuntimeObservations{})
}
