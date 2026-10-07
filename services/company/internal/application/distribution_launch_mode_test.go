package application

import (
	"testing"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestRuleStageMatchesBySource(t *testing.T) {
	rule := db.DistributionRule{PipelineID: "10", StatusID: "20", Source: "creation"}
	snap := &corebridge.LeadSnapshot{PipelineID: "10", StatusID: "99"}
	if !ruleStageMatches(rule, snap) {
		t.Fatal("creation must accept any stage inside the pipeline")
	}
	snap.PipelineID = "11"
	if ruleStageMatches(rule, snap) {
		t.Fatal("creation must reject another pipeline")
	}
	rule.Source = "legacy_stage"
	snap = &corebridge.LeadSnapshot{PipelineID: "10", StatusID: "99"}
	if ruleStageMatches(rule, snap) {
		t.Fatal("legacy must require the exact stage")
	}
	snap.StatusID = "20"
	if !ruleStageMatches(rule, snap) {
		t.Fatal("legacy must accept the exact stage")
	}
	if ruleStageMatches(rule, nil) {
		t.Fatal("nil snapshot must not match")
	}
}

func TestRuleSourceAcceptsEntry(t *testing.T) {
	group := uuid.New()
	creation := db.DistributionRule{Source: "creation"}
	if !ruleSourceAcceptsEntry(creation, db.DistributionObservedEntry{EventKind: "lead.created"}) {
		t.Fatal("creation accepts lead.created")
	}
	if ruleSourceAcceptsEntry(creation, db.DistributionObservedEntry{EventKind: "lead.status_changed"}) {
		t.Fatal("creation rejects a stage transition")
	}
	dp := db.DistributionRule{Source: "digital_pipeline", GroupID: group}
	good := db.DistributionObservedEntry{Evidence: "digital_pipeline_trigger", TriggerEvidence: []byte(`{"event":{}}`), TriggerGroupID: uuid.NullUUID{UUID: group, Valid: true}}
	if !ruleSourceAcceptsEntry(dp, good) {
		t.Fatal("dp accepts group-scoped trusted evidence")
	}
	wrongGroup := good
	wrongGroup.TriggerGroupID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	if ruleSourceAcceptsEntry(dp, wrongGroup) {
		t.Fatal("dp must reject another group's trigger")
	}
	noEvidence := good
	noEvidence.TriggerEvidence = nil
	if ruleSourceAcceptsEntry(dp, noEvidence) {
		t.Fatal("dp must reject missing trusted evidence")
	}
	noGroup := good
	noGroup.TriggerGroupID = uuid.NullUUID{}
	if ruleSourceAcceptsEntry(dp, noGroup) {
		t.Fatal("dp must reject a trigger without a bound group")
	}
	// A normal webhook entry can never satisfy a digital_pipeline rule.
	normal := db.DistributionObservedEntry{Evidence: "created_in_stage", EventKind: "lead.created", TriggerGroupID: uuid.NullUUID{UUID: group, Valid: true}}
	if ruleSourceAcceptsEntry(dp, normal) {
		t.Fatal("dp must reject a normal webhook entry")
	}
}

func TestDistributionTriggerPointIsTakenFromEntry(t *testing.T) {
	group := uuid.New()
	r := db.DistributionRule{Source: "digital_pipeline", GroupID: group, PipelineID: "", StatusID: ""}
	entry := db.DistributionObservedEntry{PipelineID: "10", StatusID: "20", Evidence: "digital_pipeline_trigger", TriggerEvidence: []byte(`{}`), TriggerGroupID: uuid.NullUUID{UUID: group, Valid: true}}
	for _, tt := range []struct {
		p, st string
		want  bool
	}{{"10", "20", true}, {"10", "21", false}, {"11", "20", false}} {
		if got := ruleEntrySnapshotMatches(r, entry, &corebridge.LeadSnapshot{PipelineID: tt.p, StatusID: tt.st}); got != tt.want {
			t.Fatalf("scope %s/%s = %v", tt.p, tt.st, got)
		}
	}
	entry.PipelineID = "12"
	entry.StatusID = "99"
	if !ruleEntrySnapshotMatches(r, entry, &corebridge.LeadSnapshot{PipelineID: "12", StatusID: "99"}) {
		t.Fatal("another trusted trigger point in same group rejected")
	}
}
