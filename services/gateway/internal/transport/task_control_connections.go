package transport

import (
	"encoding/json"
	"net/http"
	"strings"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc/metadata"
)

func (h *Handler) taskControlCall(w http.ResponseWriter, r *http.Request, in *v.TaskControlConnectionRequest, runtime bool) {
	setPrivateNoStore(w)
	ctx := outgoingContext(r)
	if runtime {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-teamos-service", "task-control", "x-teamos-service-token", h.companyServiceToken)
	}
	result, e := h.company.TaskControlConnection(ctx, in)
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	if result == nil || !json.Valid(result.JsonBody) {
		apierror.Write(w, apierror.New(502, "Некорректный ответ Контроля задач"))
		return
	}
	writeJSON(w, 200, json.RawMessage(result.JsonBody))
}
func (h *Handler) GetTaskControlConnections(w http.ResponseWriter, r *http.Request) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_LIST}, false)
}
func (h *Handler) GetTaskControlInstallation(w http.ResponseWriter, r *http.Request, accountID string) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_INSTALLATION, AccountId: accountID}, false)
}
func (h *Handler) GetTaskControlIntent(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_INTENT, Id: id.String()}, false)
}
func (h *Handler) CreateTaskControlLinkIntent(w http.ResponseWriter, r *http.Request) {
	var body api.TaskControlLinkInput
	if !decodeStrict(w, r, &body) {
		return
	}
	raw, _ := json.Marshal(body)
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_LINK, JsonBody: raw}, false)
}
func (h *Handler) RevokeTaskControlConnection(w http.ResponseWriter, r *http.Request, id api.ID) {
	var body api.TaskControlRevokeInput
	if !decodeStrict(w, r, &body) {
		return
	}
	raw, _ := json.Marshal(body)
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_REVOKE, Id: id.String(), JsonBody: raw}, false)
}
func (h *Handler) ExchangeTaskControlRuntimeSession(w http.ResponseWriter, r *http.Request) {
	var body api.TaskControlSessionInput
	if !decodeStrict(w, r, &body) {
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_EXCHANGE, Credential: valueOrEmpty(body.CoreAssertion)}, true)
}
func (h *Handler) GetTaskControlBootstrap(w http.ResponseWriter, r *http.Request) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		apierror.Write(w, apierror.Unauthorized())
		return
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) != 43 {
		apierror.Write(w, apierror.Unauthorized())
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_BOOTSTRAP, Credential: parts[1]}, true)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
