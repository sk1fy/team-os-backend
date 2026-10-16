package application

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestTaskControlLegacyHistoryPreservesEnvelopeAndSystemIdentity(t *testing.T) {
	publication, request, actor := uuid.New(), uuid.New(), uuid.New()
	for _, test := range []struct {
		event, action, actorType string
		actor                    *uuid.UUID
	}{
		{"publication_restored", "rollbackPublished", "user", &actor},
		{"runtime_recomputed", "runtimeRecomputed", "system", nil},
		{"connection_updated", "revoked", "system", nil},
	} {
		t.Run(test.event, func(t *testing.T) {
			details, err := json.Marshal(map[string]any{"reason": "Причина", "publicationId": publication, "requestId": request, "after": map[string]string{"state": "revoked"}})
			if err != nil {
				t.Fatal(err)
			}
			event := TaskControlHistoryEvent{ID: uuid.New(), ActorID: test.actor, EventType: test.event, Sequence: "123", Details: details}
			encoded, err := json.Marshal(taskControlLegacyHistory(event))
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err = json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"id", "scope", "actorId", "action", "createdAt", "reason", "eventType", "sequence", "details"} {
				if len(wire[name]) == 0 || string(wire[name]) == "null" {
					t.Fatalf("missing v1 field %s: %s", name, encoded)
				}
			}
			var result struct {
				ActorID       uuid.UUID `json:"actorId"`
				ActorType     string    `json:"actorType"`
				Action        string    `json:"action"`
				Reason        string    `json:"reason"`
				PublicationID uuid.UUID `json:"publicationId"`
				RequestID     uuid.UUID `json:"requestId"`
			}
			if err = json.Unmarshal(encoded, &result); err != nil {
				t.Fatal(err)
			}
			wantActor := uuid.Nil
			if test.actor != nil {
				wantActor = *test.actor
			}
			if result.ActorID != wantActor || result.ActorType != test.actorType || result.Action != test.action || result.Reason != "Причина" || result.PublicationID != publication || result.RequestID != request {
				t.Fatalf("lost legacy fields or misattributed author: %s", encoded)
			}
			if test.actor == nil {
				v2, err := json.Marshal(event)
				if err != nil {
					t.Fatal(err)
				}
				if err = json.Unmarshal(v2, &wire); err != nil || string(wire["actorId"]) != "null" {
					t.Fatalf("v2 must retain a null author: %s (%v)", v2, err)
				}
			}
		})
	}
}
