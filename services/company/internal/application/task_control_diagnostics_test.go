package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTaskControlTelemetryValidationRejectsUnsafeAndStaleEvidence(t *testing.T) {
	now := time.Now().UTC()
	base := TaskControlTelemetryInput{RuntimeInstanceID: uuid.New(), EventID: uuid.New(), CorrelationID: uuid.New(), RuntimeVersion: "1.2.2", OccurredAt: now, Event: "error", ErrorCode: "config_fetch_failed"}
	if e := taskControlTelemetryValid(base, now); e != nil {
		t.Fatal(e)
	}
	cases := map[string]func(*TaskControlTelemetryInput){
		"future":              func(in *TaskControlTelemetryInput) { in.OccurredAt = now.Add(time.Minute) },
		"stale":               func(in *TaskControlTelemetryInput) { in.OccurredAt = now.Add(-6 * time.Minute) },
		"unknown reason":      func(in *TaskControlTelemetryInput) { in.ErrorCode = "JWT " + strings.Repeat("x", 1024) },
		"runtime version":     func(in *TaskControlTelemetryInput) { in.RuntimeVersion = "unknown" },
		"missing correlation": func(in *TaskControlTelemetryInput) { in.CorrelationID = uuid.Nil },
		"fabricated receive":  func(in *TaskControlTelemetryInput) { in.Event = "received"; in.ErrorCode = "" },
		"unsafe integer": func(in *TaskControlTelemetryInput) {
			in.PublicationID = uuid.New()
			in.PublicationVersion = 1
			in.RuntimeRevision = 9007199254740992
			in.PayloadChecksum = strings.Repeat("a", 64)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			in := base
			change(&in)
			if e := taskControlTelemetryValid(in, now); e == nil {
				t.Fatal("accepted invalid evidence")
			}
		})
	}
}

func TestTaskControlV2AckRequiresSnapshotAndCompatibleRuntime(t *testing.T) {
	svc := &Service{}
	for _, input := range []TaskControlAckInputV2{
		{RuntimeVersion: "1.2.2"},
		{SnapshotID: uuid.New(), RuntimeVersion: "1.1.4"},
	} {
		if err := svc.AckTaskControlV2(context.Background(), "unused", input); !isCompanyError(err, ErrorValidation) {
			t.Fatal("v2 ACK accepted incompatible envelope", err)
		}
	}
}
