package transport

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

func (h *Handler) activityCall(w http.ResponseWriter, r *http.Request, in *v.ActivityControlRequest) {
	setPrivateNoStore(w)
	out, err := h.company.ActivityControl(outgoingContext(r), in)
	if err != nil {
		for _, detail := range status.Convert(err).Details() {
			if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Reason == "activity_rate_limited" {
				if seconds, e := strconv.Atoi(info.Metadata["retryAfter"]); e == nil && seconds > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(seconds))
				}
			}
		}
		h.writeRPCError(w, r, err)
		return
	}
	if out == nil || !json.Valid(out.JsonBody) {
		apierror.Write(w, apierror.New(502, "Некорректный ответ Activity"))
		return
	}
	writeJSON(w, 200, json.RawMessage(out.JsonBody))
}
func (h *Handler) GetActivityConnection(w http.ResponseWriter, r *http.Request) {
	h.activityCall(w, r, &v.ActivityControlRequest{Action: v.ActivityControlRequest_ACTION_CONNECTION})
}
func (h *Handler) GetActivityPanels(w http.ResponseWriter, r *http.Request, p api.GetActivityPanelsParams) {
	h.activityCall(w, r, &v.ActivityControlRequest{BindingId: p.XActivityBindingId.String(), Action: v.ActivityControlRequest_ACTION_PANELS})
}
func (h *Handler) GetActivityEmployees(w http.ResponseWriter, r *http.Request, p api.GetActivityEmployeesParams) {
	h.activityCall(w, r, &v.ActivityControlRequest{BindingId: p.XActivityBindingId.String(), Action: v.ActivityControlRequest_ACTION_EMPLOYEES})
}
func (h *Handler) GetActivityTaskPanel(w http.ResponseWriter, r *http.Request, p api.GetActivityTaskPanelParams) {
	h.activityCall(w, r, &v.ActivityControlRequest{BindingId: p.XActivityBindingId.String(), Action: v.ActivityControlRequest_ACTION_TASK_PANEL})
}
func (h *Handler) CreateActivityPanel(w http.ResponseWriter, r *http.Request, p api.CreateActivityPanelParams) {
	var in api.ActivityPanelInput
	if !decodeStrict(w, r, &in) {
		return
	}
	raw, _ := json.Marshal(in)
	h.activityCall(w, r, &v.ActivityControlRequest{BindingId: p.XActivityBindingId.String(), Action: v.ActivityControlRequest_ACTION_CREATE, CommandId: p.IdempotencyKey.String(), JsonBody: raw})
}
func (h *Handler) UpdateActivityPanel(w http.ResponseWriter, r *http.Request, id uuid.UUID, p api.UpdateActivityPanelParams) {
	var in api.ActivityPanelPatch
	if !decodeStrict(w, r, &in) {
		return
	}
	raw, _ := json.Marshal(in)
	h.activityCall(w, r, &v.ActivityControlRequest{BindingId: p.XActivityBindingId.String(), Action: v.ActivityControlRequest_ACTION_UPDATE, PanelId: id.String(), JsonBody: raw})
}
func (h *Handler) RotateActivityShareLink(w http.ResponseWriter, r *http.Request, id uuid.UUID, p api.RotateActivityShareLinkParams) {
	h.activityCall(w, r, &v.ActivityControlRequest{BindingId: p.XActivityBindingId.String(), Action: v.ActivityControlRequest_ACTION_SHARE, PanelId: id.String(), CommandId: p.IdempotencyKey.String()})
}
func (h *Handler) UpdateActivityTaskPanel(w http.ResponseWriter, r *http.Request, p api.UpdateActivityTaskPanelParams) {
	var in api.ActivityTaskPanel
	if !decodeStrict(w, r, &in) {
		return
	}
	raw, _ := json.Marshal(in)
	h.activityCall(w, r, &v.ActivityControlRequest{BindingId: p.XActivityBindingId.String(), Action: v.ActivityControlRequest_ACTION_SAVE_TASK_PANEL, JsonBody: raw})
}
