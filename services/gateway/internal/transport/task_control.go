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
func (h *Handler) taskControlPending(w http.ResponseWriter, r *http.Request, id api.ID, operation string) {
	setPrivateNoStore(w)
	_, err := h.company.TaskControlConnection(outgoingContext(r), &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_AUTHORIZE, Id: id.String(), Operation: operation})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	taskControlNotImplemented(w)
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
	h.taskControlPending(w, r, bindingID, "history")
}

func (h *Handler) PreviewTaskControlImport(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	h.taskControlPending(w, r, bindingID, "imports")
}

func (h *Handler) GetTaskControlImport(w http.ResponseWriter, r *http.Request, bindingID api.ID, importID api.ID) {
	h.taskControlPending(w, r, bindingID, "imports")
}

func (h *Handler) ApplyTaskControlImport(w http.ResponseWriter, r *http.Request, bindingID api.ID, importID api.ID) {
	h.taskControlPending(w, r, bindingID, "imports")
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
	h.taskControlPending(w, r, bindingID, "application")
}

func (h *Handler) GetTaskControlReferences(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_REFERENCES, Id: bindingID.String(), Operation: "legacy"}, false)
}

func (h *Handler) RollbackTaskControlPublication(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	h.taskControlPending(w, r, bindingID, "rollback")
}

func (h *Handler) CreateTaskControlSourceRequest(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	h.taskControlPending(w, r, bindingID, "sourceRequests")
}

func (h *Handler) GetTaskControlSourceRequest(w http.ResponseWriter, r *http.Request, bindingID api.ID, sourceRequestID api.ID) {
	h.taskControlPending(w, r, bindingID, "sourceRequests")
}

func (h *Handler) ValidateTaskControlDraft(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_VALIDATE, Id: bindingID.String()}, false)
}

func (h *Handler) GetTaskControlReferenceSelection(w http.ResponseWriter, r *http.Request, bindingID api.ID, params api.GetTaskControlReferenceSelectionParams) {
	// Map generated query DTO explicitly to avoid exposing security scope as input.
	options := map[string]any{}
	if params.Search != nil {
		options["Search"] = *params.Search
	}
	if params.DepartmentId != nil {
		options["DepartmentID"] = *params.DepartmentId
	}
	if params.WithoutDepartment != nil {
		options["WithoutDepartment"] = *params.WithoutDepartment
	}
	if params.Offset != nil {
		options["Offset"] = *params.Offset
	}
	if params.Limit != nil {
		options["Limit"] = *params.Limit
	}
	if params.SelectedEmployeeIds != nil {
		options["SelectedEmployeeIDs"] = *params.SelectedEmployeeIds
	}
	raw, _ := json.Marshal(options)
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_REFERENCES, Id: bindingID.String(), JsonBody: raw}, false)
}
func (h *Handler) RefreshTaskControlReferences(w http.ResponseWriter, r *http.Request, bindingID api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_REFRESH_REFERENCES, Id: bindingID.String()}, false)
}
func (h *Handler) GetTaskControlReferencesRefresh(w http.ResponseWriter, r *http.Request, bindingID api.ID, refreshID api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_REFERENCES_REFRESH, Id: bindingID.String(), AccountId: refreshID.String()}, false)
}
