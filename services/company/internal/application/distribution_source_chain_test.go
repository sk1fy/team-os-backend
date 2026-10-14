package application

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func stageSourceEvent(at time.Time, old, current string) DistributionEventEnvelope {
	p := "20"
	return DistributionEventEnvelope{EventID: uuid.New(), SourceOccurredAt: &at, Event: DistributionCRMEvent{Kind: "lead.status_changed", SourceEvidence: &DistributionSourceEvidence{PipelineID: &p, OldPipelineID: &p, StatusID: &current, OldStatusID: &old}}}
}

func sourcePayloads(events ...DistributionEventEnvelope) [][]byte {
	var raw [][]byte
	for _, event := range events {
		data, _ := json.Marshal(event)
		raw = append(raw, data)
	}
	return raw
}

func TestProvenDistributionReentry(t *testing.T) {
	base := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	entry := db.DistributionObservedEntry{PipelineID: "20", StatusID: "30", SourceOccurredAt: pgtype.Timestamptz{Time: base, Valid: true}}
	for _, delay := range []time.Duration{time.Second, 5 * time.Second, 12 * time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			exit := stageSourceEvent(base.Add(time.Second), "30", "40")
			back := stageSourceEvent(base.Add(time.Second+delay), "40", "30")
			duplicate := back
			duplicate.EventID = uuid.New()
			for _, events := range [][]DistributionEventEnvelope{{exit, back}, {back, exit}, {duplicate, exit, back}} {
				got := provenDistributionReentry(entry, sourcePayloads(events...))
				if got == nil || !got.SourceOccurredAt.Equal(*back.SourceOccurredAt) || *got.Event.SourceEvidence.OldStatusID != "40" {
					t.Fatal("complete source chain did not preserve genuine return", got)
				}
			}
		})
	}
	exit := stageSourceEvent(base.Add(time.Second), "30", "40")
	for name, events := range map[string][]DistributionEventEnvelope{
		"exit only":             {exit},
		"return without exit":   {stageSourceEvent(base.Add(2*time.Second), "40", "30")},
		"same second unordered": {exit, stageSourceEvent(base.Add(time.Second), "40", "30")},
		"missing edge":          {exit, stageSourceEvent(base.Add(2*time.Second), "50", "30")},
		"old history":           {stageSourceEvent(base.Add(-2*time.Second), "30", "40"), stageSourceEvent(base.Add(-time.Second), "40", "30")},
		"final outside":         {exit, stageSourceEvent(base.Add(2*time.Second), "40", "50")},
	} {
		t.Run(name, func(t *testing.T) {
			if got := provenDistributionReentry(entry, sourcePayloads(events...)); got != nil {
				t.Fatal("unproven entry admitted", got)
			}
		})
	}
}

func TestStatusChangedDuplicateEntryIdentity(t *testing.T) {
	at := time.Now().Truncate(time.Second)
	original := stageSourceEvent(at, "40", "30")
	entry := db.DistributionObservedEntry{EventKind: "lead.status_changed", SourceOccurredAt: pgtype.Timestamptz{Time: at, Valid: true}}
	duplicate := original
	duplicate.EventID = uuid.New()
	if !mirrorOfStatusChangedEntry(entry, duplicate, original) {
		t.Fatal("duplicate notification paused genuine entry")
	}
	for _, changed := range []DistributionEventEnvelope{stageSourceEvent(at.Add(time.Second), "40", "30"), stageSourceEvent(at, "50", "30")} {
		if mirrorOfStatusChangedEntry(entry, changed, original) {
			t.Fatal("different transition reused entry")
		}
	}
}

func TestDistributionLiveBoundaryRetryGuard(t *testing.T) {
	boundary := time.Date(2026, 10, 14, 12, 0, 0, 123456000, time.UTC)
	r := db.DistributionRule{LiveStartedAt: pgtype.Timestamptz{Time: boundary, Valid: true}, FirstActivationAt: pgtype.Timestamptz{Time: boundary.Add(-time.Minute), Valid: true}}
	for _, before := range []time.Time{boundary.Truncate(time.Second), boundary.Add(-time.Minute)} {
		e := db.DistributionObservedEntry{SourceOccurredAt: pgtype.Timestamptz{Time: before, Valid: true}}
		if !distributionEntryBeforeLiveBoundary(r, e) {
			t.Fatal("uncertain/history candidate bypassed retry guard")
		}
	}
	e := db.DistributionObservedEntry{SourceOccurredAt: pgtype.Timestamptz{Time: boundary.Truncate(time.Second).Add(time.Second), Valid: true}}
	if distributionEntryBeforeLiveBoundary(r, e) {
		t.Fatal("fresh next-second entry blocked")
	}
}

func TestDistributionSourceClaimFreshUsesEntryNotOwnerEdit(t *testing.T) {
	entry := time.Now().Truncate(time.Second)
	ownerEdit := entry.Add(10 * time.Second)
	exit := entry.Add(time.Second)
	old := entry.Add(-time.Second)
	if !distributionSourceClaimFresh(&exit, &entry, &ownerEdit) {
		t.Fatal("genuine exit hidden by later responsible edit")
	}
	if distributionSourceClaimFresh(&old, &entry, nil) {
		t.Fatal("old claim paused a verified newer entry")
	}
	if !distributionSourceClaimFresh(nil, &entry, &ownerEdit) {
		t.Fatal("unknown source time bypassed ambiguity guard")
	}
}
