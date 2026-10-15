package transport

import (
	"encoding/json"
	"net/http"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

// KT-01.02 publishes the contract only. Never return fake persistence/delivery success.
func taskControlNotImplemented(w http.ResponseWriter) {
	apierror.Write(w, apierror.New(http.StatusNotImplemented, "Контроль задач: обработчик ещё не реализован").WithCode("task_control_not_implemented"))
}

func (h *Handler) GetTaskControlDraft(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_DRAFT, Id: bindingID.String()}, false)
}

func (h *Handler) UpdateTaskControlDraft(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	var raw json.RawMessage
	if !decodeStrict(w, r, &raw) {
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_UPDATE_DRAFT, Id: bindingID.String(), JsonBody: raw}, false)
}

func (h *Handler) GetTaskControlHistory(w http.ResponseWriter, r *http.Request, bindingID api.ID, params api.GetTaskControlHistoryParams) {
	taskControlNotImplemented(w)
}

func (h *Handler) PreviewTaskControlImport(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	taskControlNotImplemented(w)
}

func (h *Handler) GetTaskControlImport(w http.ResponseWriter, r *http.Request, bindingID api.ID, importID api.ID) {
	taskControlNotImplemented(w)
}

func (h *Handler) ApplyTaskControlImport(w http.ResponseWriter, r *http.Request, bindingID api.ID, importID api.ID) {
	taskControlNotImplemented(w)
}

func (h *Handler) CreateTaskControlModeIntent(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	var input api.TaskControlModeInput
	if !decodeStrict(w, r, &input) {
		return
	}
	raw, _ := json.Marshal(input)
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_MODE, Id: bindingID.String(), JsonBody: raw}, false)
}

func (h *Handler) GetTaskControlPublications(w http.ResponseWriter, r *http.Request, bindingID api.ID, params api.GetTaskControlPublicationsParams) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_PUBLICATIONS, Id: bindingID.String()}, false)
}

func (h *Handler) PublishTaskControlDraft(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	var input api.TaskControlPublishInput
	if !decodeStrict(w, r, &input) {
		return
	}
	raw, _ := json.Marshal(input)
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_PUBLISH, Id: bindingID.String(), JsonBody: raw}, false)
}

func (h *Handler) GetTaskControlPublication(w http.ResponseWriter, r *http.Request, bindingID api.ID, publicationID api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_PUBLICATION, Id: bindingID.String(), AccountId: publicationID.String()}, false)
}

func (h *Handler) GetTaskControlApplicationStatus(w http.ResponseWriter, r *http.Request, bindingID api.ID, publicationID api.ID) {
	taskControlNotImplemented(w)
}

func (h *Handler) GetTaskControlReferences(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	taskControlNotImplemented(w)
}

func (h *Handler) RollbackTaskControlPublication(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	taskControlNotImplemented(w)
}

func (h *Handler) CreateTaskControlSourceRequest(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	taskControlNotImplemented(w)
}

func (h *Handler) GetTaskControlSourceRequest(w http.ResponseWriter, r *http.Request, bindingID api.ID, sourceRequestID api.ID) {
	taskControlNotImplemented(w)
}

func (h *Handler) ValidateTaskControlDraft(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_VALIDATE, Id: bindingID.String()}, false)
}
