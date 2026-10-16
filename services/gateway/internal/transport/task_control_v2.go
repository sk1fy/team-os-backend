package transport

import (
	"encoding/json"
	"net/http"
	"strings"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

// The company transport strictly decodes discriminated intent unions. Forward raw
// JSON so generated union wrappers never discard unknown or incompatible fields.
func (h *Handler) taskControlV2Mutation(w http.ResponseWriter, r *http.Request, id api.ID, action v.TaskControlConnectionRequest_Action) {
	var raw json.RawMessage
	if !decodeStrict(w, r, &raw) {
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: action, Id: id.String(), JsonBody: raw}, false)
}
func (h *Handler) GetTaskControlDraftV2(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_DRAFT_V2, Id: id.String()}, false)
}
func (h *Handler) UpdateTaskControlDraftV2(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlV2Mutation(w, r, id, v.TaskControlConnectionRequest_ACTION_UPDATE_DRAFT_V2)
}
func (h *Handler) ValidateTaskControlDraftV2(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlV2Mutation(w, r, id, v.TaskControlConnectionRequest_ACTION_VALIDATE_V2)
}
func (h *Handler) PublishTaskControlDraftV2(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlV2Mutation(w, r, id, v.TaskControlConnectionRequest_ACTION_PUBLISH_V2)
}
func (h *Handler) ActivateTaskControlV2(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlV2Mutation(w, r, id, v.TaskControlConnectionRequest_ACTION_ACTIVATE_V2)
}
func (h *Handler) RollbackTaskControlPublicationV2(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlV2Mutation(w, r, id, v.TaskControlConnectionRequest_ACTION_ROLLBACK_V2)
}
func (h *Handler) GetTaskControlPublicationsV2(w http.ResponseWriter, r *http.Request, id api.ID, p api.GetTaskControlPublicationsV2Params) {
	raw, _ := json.Marshal(p)
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_PUBLICATIONS_V2, Id: id.String(), JsonBody: raw}, false)
}
func (h *Handler) GetTaskControlPublicationV2(w http.ResponseWriter, r *http.Request, id api.ID, pid api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_PUBLICATION_V2, Id: id.String(), AccountId: pid.String()}, false)
}
func (h *Handler) GetTaskControlApplicationV2(w http.ResponseWriter, r *http.Request, id api.ID, pid api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_APPLICATION_V2, Id: id.String(), AccountId: pid.String()}, false)
}
func taskControlRuntimeBearer(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		apierror.Write(w, apierror.Unauthorized())
		return "", false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) != 43 {
		apierror.Write(w, apierror.Unauthorized())
		return "", false
	}
	return parts[1], true
}
func (h *Handler) GetTaskControlRuntimeV2(w http.ResponseWriter, r *http.Request) {
	token, ok := taskControlRuntimeBearer(w, r)
	if !ok {
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_BOOTSTRAP_V2, Credential: token}, true)
}
func (h *Handler) AckTaskControlRuntimeV2(w http.ResponseWriter, r *http.Request) {
	h.taskControlRuntimeEvent(w, r, v.TaskControlConnectionRequest_ACTION_ACK_V2)
}

func (h *Handler) PreviewTaskControlModeV2(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlV2Mutation(w, r, id, v.TaskControlConnectionRequest_ACTION_MODE_PREVIEW_V2)
}
func (h *Handler) CreateTaskControlModeV2(w http.ResponseWriter, r *http.Request, id api.ID) {
	h.taskControlV2Mutation(w, r, id, v.TaskControlConnectionRequest_ACTION_MODE_V2)
}
