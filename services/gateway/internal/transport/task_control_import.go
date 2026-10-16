package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) taskControlImportMutation(w http.ResponseWriter, r *http.Request, binding, id api.ID, action v.TaskControlConnectionRequest_Action, operation string) {
	setPrivateNoStore(w)
	var raw json.RawMessage
	if !decodeTaskControlImportJSON(w, r, &raw) {
		return
	}
	in := &v.TaskControlConnectionRequest{Action: action, Id: binding.String(), JsonBody: raw, Operation: operation}
	if id != (api.ID{}) {
		in.AccountId = id.String()
	}
	h.taskControlCall(w, r, in, false)
}
func (h *Handler) PreviewTaskControlImportV2(w http.ResponseWriter, r *http.Request, binding api.ID) {
	h.taskControlImportMutation(w, r, binding, api.ID{}, v.TaskControlConnectionRequest_ACTION_IMPORT_PREVIEW, "v2")
}
func (h *Handler) GetTaskControlImportV2(w http.ResponseWriter, r *http.Request, binding, id api.ID) {
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_IMPORT_GET, Id: binding.String(), AccountId: id.String(), Operation: "v2"}, false)
}
func (h *Handler) ApplyTaskControlImportV2(w http.ResponseWriter, r *http.Request, binding, id api.ID) {
	h.taskControlImportMutation(w, r, binding, id, v.TaskControlConnectionRequest_ACTION_IMPORT_APPLY, "v2")
}
func taskControlBridgeAssertion(w http.ResponseWriter, r *http.Request) (string, bool) {
	setPrivateNoStore(w)
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		apierror.Write(w, apierror.Unauthorized())
		return "", false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 8192 || len(strings.Split(parts[1], ".")) != 3 {
		apierror.Write(w, apierror.Unauthorized())
		return "", false
	}
	return parts[1], true
}
func (h *Handler) GetTaskControlSourceRequests(w http.ResponseWriter, r *http.Request) {
	proof, ok := taskControlBridgeAssertion(w, r)
	if !ok {
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_SOURCE_PENDING, Credential: proof}, true)
}
func (h *Handler) SubmitTaskControlSourceResult(w http.ResponseWriter, r *http.Request, id api.ID) {
	proof, ok := taskControlBridgeAssertion(w, r)
	if !ok {
		return
	}
	var raw json.RawMessage
	if !decodeTaskControlImportJSON(w, r, &raw) {
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_SOURCE_SUBMIT, Id: id.String(), Credential: proof, JsonBody: raw}, true)
}

func (h *Handler) GetTaskControlRuntimeConnection(w http.ResponseWriter, r *http.Request) {
	setPrivateNoStore(w)
	token, ok := taskControlRuntimeBearer(w, r)
	if !ok {
		return
	}
	h.taskControlCall(w, r, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_RUNTIME_CONNECTION, Credential: token}, true)
}

// Import uploads keep the original bytes until company validates them. Reject
// duplicate keys here, before either JSONB or a struct decoder can repair input.
func decodeTaskControlImportJSON(w http.ResponseWriter, r *http.Request, out *json.RawMessage) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			apierror.Write(w, apierror.New(http.StatusRequestEntityTooLarge, "Источник превышает 1 МиБ").WithCode("task_control_payload_too_large"))
		} else {
			apierror.Write(w, apierror.BadRequest("Не удалось прочитать источник"))
		}
		return false
	}
	if !json.Valid(raw) || !uniqueTaskControlImportJSON(json.NewDecoder(bytes.NewReader(raw)), 0) {
		apierror.Write(w, apierror.BadRequest("Некорректный JSON источника или повторяющиеся поля"))
		return false
	}
	*out = append((*out)[:0], raw...)
	return true
}
func uniqueTaskControlImportJSON(d *json.Decoder, depth int) bool {
	if depth > 64 {
		return false
	}
	token, err := d.Token()
	if err != nil {
		return false
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return true
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			name, ok := key.(string)
			if e != nil || !ok || seen[name] {
				return false
			}
			seen[name] = true
			if !uniqueTaskControlImportJSON(d, depth+1) {
				return false
			}
		}
	case '[':
		for d.More() {
			if !uniqueTaskControlImportJSON(d, depth+1) {
				return false
			}
		}
	default:
		return false
	}
	_, err = d.Token()
	return err == nil
}
