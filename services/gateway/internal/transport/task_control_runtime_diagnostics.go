package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
)

func (h *Handler) taskControlRuntimeEvent(w http.ResponseWriter, r *http.Request, action v.TaskControlConnectionRequest_Action) {
	setPrivateNoStore(w)
	token, ok := taskControlRuntimeBearer(w, r)
	if !ok {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierror.Write(w, apierror.New(http.StatusRequestEntityTooLarge, "Телеметрия превышает 8 КиБ").WithCode("task_control_telemetry_too_large"))
		} else {
			apierror.Write(w, apierror.BadRequest("Не удалось прочитать подтверждение runtime"))
		}
		return
	}
	if !json.Valid(raw) || !uniqueTaskControlImportJSON(json.NewDecoder(bytes.NewReader(raw)), 0) {
		apierror.Write(w, apierror.BadRequest("Некорректный JSON подтверждения runtime или повторяющиеся поля"))
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: action, Credential: token, JsonBody: raw}, true)
}

func (h *Handler) AckTaskControlRuntime(w http.ResponseWriter, r *http.Request) {
	h.taskControlRuntimeEvent(w, r, v.TaskControlConnectionRequest_ACTION_ACK)
}

func (h *Handler) RecordTaskControlRuntimeTelemetry(w http.ResponseWriter, r *http.Request) {
	h.taskControlRuntimeEvent(w, r, v.TaskControlConnectionRequest_ACTION_TELEMETRY)
}
