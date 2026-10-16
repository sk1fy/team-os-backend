package application

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
)

// This fixture crosses the real Go JSON encoder, OpenAPI validator, and AMD client.
func TestTaskControlRuntimeV2ContractFixture(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "contracts", "fixtures")
	raw, e := os.ReadFile(filepath.Join(dir, "task-control-intent-v2.json"))
	if e != nil {
		t.Fatal(e)
	}
	var intents struct {
		IntentRules []tc.IntentRule `json:"intentRules"`
	}
	if e = json.Unmarshal(raw, &intents); e != nil {
		t.Fatal(e)
	}
	intentHash, e := tc.IntentChecksum(intents.IntentRules)
	if e != nil {
		t.Fatal(e)
	}
	raw, e = os.ReadFile(filepath.Join(dir, "task-control-v1.json"))
	if e != nil {
		t.Fatal(e)
	}
	var legacy struct {
		Rules []tc.Rule `json:"rules"`
	}
	if e = json.Unmarshal(raw, &legacy); e != nil {
		t.Fatal(e)
	}
	rules, payloadHash := tc.ProjectRuntimeRules(legacy.Rules, "7")
	_, resolvedHash := tc.Legacy(legacy.Rules)
	id := func(s string) uuid.UUID { return uuid.MustParse(s) }
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	scope := TaskControlScope{CompanyID: id("a0000000-0000-4000-8000-000000000001"), BindingID: id("b0000000-0000-4000-8000-000000000001"), BindingRevision: 1, InstallationID: id("c0000000-0000-4000-8000-000000000001"), IntegrationID: id("d0000000-0000-4000-8000-000000000001"), AccountID: "123", Product: "task-control", ModeEpoch: 2, Generation: 1}
	value := TaskControlRuntimeEnvelopeV2{RuntimeSchemaVersion: 2, Scope: scope, ActorID: "7", PublicationID: id("e0000000-0000-4000-8000-000000000001"), PublicationVersion: 7, IntentChecksum: intentHash, SnapshotID: id("f0000000-0000-4000-8000-000000000001"), RuntimeRevision: 21, SourceRevisions: TaskControlSourceRevisions{CRM: "sha256:" + strings.Repeat("a", 64), Organization: 1, Mapping: 1}, ResolverVersion: 1, ResolvedChecksum: resolvedHash, PayloadChecksum: payloadHash, RuntimeRules: rules, IssuedAt: now, ValidUntil: now.Add(3 * time.Minute), MinRuntimeVersion: "1.2.0"}
	encoded, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	encoded = append(encoded, '\n')
	name := filepath.Join(dir, "task-control-runtime-v2.json")
	if os.Getenv("UPDATE_TASK_CONTROL_RUNTIME_FIXTURE") == "1" {
		if e = os.WriteFile(name, encoded, 0644); e != nil {
			t.Fatal(e)
		}
	}
	expected, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	if string(expected) != string(encoded) {
		t.Fatal("runtime v2 contract fixture differs from production encoder/checksum")
	}
}
