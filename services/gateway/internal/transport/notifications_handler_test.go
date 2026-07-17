package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
)

func TestNotificationsUnavailableUsesAPIErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Handler{}).GetNotifications(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/notifications", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var envelope apierror.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Status != http.StatusServiceUnavailable || envelope.Error.Message != "Сервис уведомлений временно недоступен" {
		t.Fatalf("unexpected response: %+v", envelope.Error)
	}
}
