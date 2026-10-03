package distributionhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

type WidgetRuntimeService interface {
	DistributionWidgetRuntime(context.Context, application.DistributionWidgetRuntimeInput) (json.RawMessage, error)
}

func (h *Handler) widgetRuntime(w http.ResponseWriter, r *http.Request, d *json.Decoder, company, installation uuid.UUID) {
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": message, "status": status}})
	}
	var in application.DistributionWidgetRuntimeInput
	var extra any
	if d.Decode(&in) != nil || !errors.Is(d.Decode(&extra), io.EOF) {
		fail(400, "Некорректные параметры")
		return
	}
	if in.CompanyID != company || in.InstallationID != installation {
		fail(403, "Подключение не совпадает")
		return
	}
	service, ok := h.Service.(WidgetRuntimeService)
	if !ok {
		fail(503, "Сервис недоступен")
		return
	}
	raw, e := service.DistributionWidgetRuntime(r.Context(), in)
	if e != nil {
		status := 503
		message := "Источник данных недоступен"
		var app *application.Error
		if errors.As(e, &app) {
			message = app.Message
			switch app.Kind {
			case application.ErrorValidation:
				status = 400
			case application.ErrorForbidden:
				status = 403
			case application.ErrorNotFound:
				status = 404
			case application.ErrorConflict:
				status = 409
			}
		}
		fail(status, message)
		return
	}
	var state struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal(raw, &state)
	if state.State == "outcome_unknown" {
		w.WriteHeader(202)
	}
	_, _ = w.Write(raw)
}
