package eventbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/eventbus"
)

func TestNewEventRoundTrip(t *testing.T) {
	t.Parallel()

	event, err := eventbus.NewEvent("company-1", "user-1", map[string]any{
		"userId": "created-user",
		"role":   "employee",
	})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if len(event.EventID) != 36 {
		t.Fatalf("event ID = %q", event.EventID)
	}
	if event.OccurredAt.Location() != time.UTC {
		t.Fatalf("event time is not UTC: %v", event.OccurredAt)
	}

	data, err := eventbus.EncodeEvent(event)
	if err != nil {
		t.Fatalf("EncodeEvent: %v", err)
	}
	decoded, err := eventbus.DecodeEvent(data)
	if err != nil {
		t.Fatalf("DecodeEvent: %v", err)
	}
	if decoded.EventID != event.EventID || decoded.CompanyID != "company-1" || decoded.ActorID != "user-1" {
		t.Fatalf("unexpected decoded event: %#v", decoded)
	}

	var payload map[string]any
	if err := json.Unmarshal(decoded.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["userId"] != "created-user" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestDecodeEventAllowsAdditiveEnvelopeFields(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"eventId":"event-1",
		"occurredAt":"2026-07-12T09:00:00Z",
		"companyId":"company-1",
		"actorId":"user-1",
		"payload":{},
		"futureOptionalField":"compatible"
	}`)
	if _, err := eventbus.DecodeEvent(data); err != nil {
		t.Fatalf("additive field should be accepted: %v", err)
	}
}

func TestInvalidEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event eventbus.Event
	}{
		{name: "missing ID", event: eventbus.Event{OccurredAt: time.Now(), CompanyID: "company", Payload: json.RawMessage(`{}`)}},
		{name: "missing time", event: eventbus.Event{EventID: "event", CompanyID: "company", Payload: json.RawMessage(`{}`)}},
		{name: "missing company", event: eventbus.Event{EventID: "event", OccurredAt: time.Now(), Payload: json.RawMessage(`{}`)}},
		{name: "invalid payload", event: eventbus.Event{EventID: "event", OccurredAt: time.Now(), CompanyID: "company", Payload: json.RawMessage(`{`)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.event.Validate(); !errors.Is(err, eventbus.ErrInvalidEvent) {
				t.Fatalf("error = %v, want ErrInvalidEvent", err)
			}
		})
	}
}

func TestSubjectValidation(t *testing.T) {
	t.Parallel()

	valid := []string{
		"teamos.org.user.created.v1",
		"teamos.kb.article.updated.v2",
		"teamos.tasks.task.due_soon.v12",
	}
	for _, subject := range valid {
		if err := eventbus.ValidateSubject(subject); err != nil {
			t.Errorf("valid subject %q rejected: %v", subject, err)
		}
	}

	invalid := []string{
		"org.user.created.v1",
		"teamos.org.user.created",
		"teamos.org.user.created.v0",
		"teamos.org.user.*.v1",
		"teamos.org.user.created.extra.v1",
	}
	for _, subject := range invalid {
		if err := eventbus.ValidateSubject(subject); !errors.Is(err, eventbus.ErrInvalidSubject) {
			t.Errorf("error for %q = %v, want ErrInvalidSubject", subject, err)
		}
	}
}

func TestFilterSubjectValidation(t *testing.T) {
	t.Parallel()

	valid := []string{
		"teamos.org.user.*.v1",
		"teamos.*.mention.created.v1",
		"teamos.tasks.task.assigned.v1",
	}
	for _, subject := range valid {
		if err := eventbus.ValidateFilterSubject(subject); err != nil {
			t.Errorf("valid filter %q rejected: %v", subject, err)
		}
	}

	invalid := []string{
		"teamos.org.user.>.v1",
		"teamos.org.user.*",
		"teamos.>.v1",
	}
	for _, subject := range invalid {
		if err := eventbus.ValidateFilterSubject(subject); !errors.Is(err, eventbus.ErrInvalidSubject) {
			t.Errorf("error for %q = %v, want ErrInvalidSubject", subject, err)
		}
	}
}

func TestHandlerFunc(t *testing.T) {
	t.Parallel()

	want := errors.New("database offline")
	handler := eventbus.HandlerFunc(func(context.Context, eventbus.Event) (bool, error) {
		return false, want
	})
	_, err := handler.HandleOnce(context.Background(), eventbus.Event{})
	if !errors.Is(err, want) {
		t.Fatalf("handler error = %v", err)
	}
}

func TestUninitializedBusIsNotReady(t *testing.T) {
	t.Parallel()

	var bus *eventbus.Bus
	if err := bus.Ready(context.Background()); err == nil {
		t.Fatal("uninitialized bus unexpectedly reported ready")
	}
}
