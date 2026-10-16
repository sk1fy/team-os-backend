package application

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestTaskControlTelemetryAcceptsActualWidgetWireFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/fixtures/task-control-telemetry.json")
	if err != nil {
		t.Fatal(err)
	}
	var events []TaskControlTelemetryInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&events); err != nil {
		t.Fatal("actual widget events do not match company DTO", err)
	}
	if len(events) != 2 || events[0].Event != "received" || events[1].Event != "applied" {
		t.Fatal("wire fixture does not separate reception from application")
	}
	for _, event := range events {
		if err = taskControlTelemetryValid(event, event.OccurredAt); err != nil {
			t.Fatal("actual widget telemetry rejected", err)
		}
	}
	if events[0].EventID == events[1].EventID || events[0].RuntimeInstanceID != events[1].RuntimeInstanceID || events[0].CorrelationID != events[1].CorrelationID {
		t.Fatal("event identity or correlation was lost")
	}
	var envelope TaskControlRuntimeEnvelopeV2
	raw, err = os.ReadFile("../../../../contracts/fixtures/task-control-runtime-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Scope != envelope.Scope || event.ActorID != envelope.ActorID || event.SnapshotID != envelope.SnapshotID || event.PublicationID != envelope.PublicationID || event.RuntimeRevision != envelope.RuntimeRevision || event.PayloadChecksum != envelope.PayloadChecksum {
			t.Fatal("widget evidence does not match the server-issued actor projection")
		}
	}
}
