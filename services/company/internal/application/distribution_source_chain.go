package application

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// provenDistributionReentry reconstructs only a complete source transition
// chain after the current entry. GET snapshots may already reflect the return
// when the exit webhook is processed. Arrival order and GET updated_at are not
// transition identity. Different transitions in one source second remain
// ambiguous; identical timestamp/old/new claims are duplicate notifications.
func provenDistributionReentry(entry db.DistributionObservedEntry, payloads [][]byte) *DistributionEventEnvelope {
	return provenDistributionSourceChain(entry, nil, payloads)
}

func provenDistributionSourceChain(entry db.DistributionObservedEntry, original *DistributionEventEnvelope, payloads [][]byte) *DistributionEventEnvelope {
	if !entry.SourceOccurredAt.Valid || len(payloads) >= 101 {
		return nil // bounded reads must not accidentally prove a truncated chain
	}
	type transition struct {
		event                                    DistributionEventEnvelope
		oldPipeline, oldStatus, pipeline, status string
	}
	var transitions []transition
	for _, raw := range payloads {
		var event DistributionEventEnvelope
		if json.Unmarshal(raw, &event) != nil || event.Event.Kind != "lead.status_changed" || event.SourceOccurredAt == nil || !event.SourceOccurredAt.After(entry.SourceOccurredAt.Time) {
			return nil
		}
		s := event.Event.SourceEvidence
		if s == nil || s.PipelineID == nil || s.StatusID == nil || s.OldStatusID == nil {
			return nil
		}
		oldPipeline := *s.PipelineID
		if s.OldPipelineID != nil {
			oldPipeline = *s.OldPipelineID
		} else if *s.PipelineID != entry.PipelineID {
			return nil // only same-pipeline status changes may omit old_pipeline_id
		}
		transitions = append(transitions, transition{event, oldPipeline, *s.OldStatusID, *s.PipelineID, *s.StatusID})
	}
	sort.Slice(transitions, func(i, j int) bool {
		return transitions[i].event.SourceOccurredAt.Before(*transitions[j].event.SourceOccurredAt)
	})
	pipeline, status := entry.PipelineID, entry.StatusID
	minimumTransitions := 2
	// The first creation GET can already see the destination of a fast move.
	// A documented creation outside that destination is a real source anchor,
	// not an observed target-stage entry. One matching status edge proves entry.
	if entry.EventKind == "lead.created" && (entry.Evidence == "reconciled" || entry.Evidence == "ambiguous_reentry") && original != nil && original.Event.Kind == "lead.created" && original.EventID == entry.EntryEventID && original.SourceOccurredAt != nil && original.SourceOccurredAt.Equal(entry.SourceOccurredAt.Time) {
		s := original.Event.SourceEvidence
		if s != nil && s.PipelineID != nil && s.StatusID != nil && (*s.PipelineID != pipeline || *s.StatusID != status) {
			pipeline, status, minimumTransitions = *s.PipelineID, *s.StatusID, 1
		}
	}
	var last *transition
	var occurred time.Time
	count := 0
	var candidate *DistributionEventEnvelope
	for i := range transitions {
		t := &transitions[i]
		if last != nil && t.event.SourceOccurredAt.Equal(occurred) {
			if t.oldPipeline == last.oldPipeline && t.oldStatus == last.oldStatus && t.pipeline == last.pipeline && t.status == last.status {
				continue
			}
			return nil
		}
		if t.oldPipeline != pipeline || t.oldStatus != status {
			return nil // a missing/reordered source edge cannot establish an entry
		}
		last, occurred = t, *t.event.SourceOccurredAt
		if pipeline != t.pipeline || status != t.status {
			count++
			if t.pipeline == entry.PipelineID && t.status == entry.StatusID {
				candidate = &t.event
			}
		}
		pipeline, status = t.pipeline, t.status
	}
	if count < minimumTransitions || last == nil || pipeline != entry.PipelineID || status != entry.StatusID {
		return nil
	}
	return candidate
}

// A second notification of the same source transition is not a reentry.
func mirrorOfStatusChangedEntry(entry db.DistributionObservedEntry, incoming, original DistributionEventEnvelope) bool {
	if entry.EventKind != "lead.status_changed" || incoming.Event.Kind != entry.EventKind || incoming.SourceOccurredAt == nil || original.SourceOccurredAt == nil || !entry.SourceOccurredAt.Valid || !incoming.SourceOccurredAt.Equal(entry.SourceOccurredAt.Time) || !incoming.SourceOccurredAt.Equal(*original.SourceOccurredAt) {
		return false
	}
	a, b := incoming.Event.SourceEvidence, original.Event.SourceEvidence
	if a == nil || b == nil || a.StatusID == nil || b.StatusID == nil || a.OldStatusID == nil || b.OldStatusID == nil {
		return false
	}
	return equalSourceID(a.PipelineID, b.PipelineID) && equalSourceID(a.StatusID, b.StatusID) && equalSourceID(a.OldPipelineID, b.OldPipelineID) && equalSourceID(a.OldStatusID, b.OldStatusID)
}
func equalSourceID(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// A snapshot's updated_at can advance for a responsible-user edit. Source
// ordering is anchored to the durable entry when known; otherwise keep the
// existing snapshot fallback and fail closed for a missing source timestamp.
func distributionSourceClaimFresh(occurred, entryTime, snapshotUpdated *time.Time) bool {
	if occurred == nil {
		return true
	}
	anchor := entryTime
	if anchor == nil {
		anchor = snapshotUpdated
	}
	return anchor == nil || !occurred.Before(*anchor)
}
