package apierror_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
)

func TestConstructors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     *apierror.Error
		status  int
		message string
	}{
		{name: "bad request", err: apierror.BadRequest("Некорректные данные"), status: http.StatusBadRequest, message: "Некорректные данные"},
		{name: "unauthorized default", err: apierror.Unauthorized(), status: http.StatusUnauthorized, message: "Требуется авторизация"},
		{name: "forbidden custom", err: apierror.Forbidden("Только для владельца"), status: http.StatusForbidden, message: "Только для владельца"},
		{name: "feminine not found", err: apierror.NotFound("Статья"), status: http.StatusNotFound, message: "Статья не найдена"},
		{name: "masculine not found", err: apierror.NotFound("Пользователь"), status: http.StatusNotFound, message: "Пользователь не найден"},
		{name: "neuter not found", err: apierror.NotFound("Приглашение"), status: http.StatusNotFound, message: "Приглашение не найдено"},
		{name: "conflict", err: apierror.Conflict("Версия устарела"), status: http.StatusConflict, message: "Версия устарела"},
		{name: "empty message remains Russian", err: apierror.Conflict(""), status: http.StatusConflict, message: "Конфликт данных"},
		{name: "invalid status is internal", err: apierror.New(http.StatusOK, ""), status: http.StatusInternalServerError, message: apierror.DefaultInternalMessage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.err.Status != tt.status {
				t.Fatalf("status = %d, want %d", tt.err.Status, tt.status)
			}
			if tt.err.Message != tt.message {
				t.Fatalf("message = %q, want %q", tt.err.Message, tt.message)
			}
		})
	}
}

func TestInternalRetainsCauseWithoutSerializingIt(t *testing.T) {
	t.Parallel()

	cause := errors.New("database password leaked here")
	err := apierror.Internal(cause)
	if !errors.Is(err, cause) {
		t.Fatal("internal error does not retain its cause")
	}

	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal error: %v", marshalErr)
	}
	if string(data) != `{"message":"Что-то пошло не так. Попробуйте ещё раз.","status":500}` {
		t.Fatalf("unexpected public JSON: %s", data)
	}
}

func TestFromPreservesAPIError(t *testing.T) {
	t.Parallel()

	original := apierror.NotFound("Отдел")
	wrapped := errors.Join(errors.New("repository"), original)
	if got := apierror.From(wrapped); got != original {
		t.Fatal("From did not preserve the API error")
	}
}

func TestWriteUsesFrontendEnvelope(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	apierror.Write(recorder, apierror.NotFound("Должность"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}

	var envelope apierror.Envelope
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error.Message != "Должность не найдена" || envelope.Error.Status != http.StatusNotFound {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}
