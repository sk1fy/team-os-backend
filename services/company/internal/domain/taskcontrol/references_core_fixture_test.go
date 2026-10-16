package taskcontrol

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReferenceSnapshotCoreWireAndCanonicalRevision(t *testing.T) {
	raw, err := os.ReadFile("../../../../../contracts/fixtures/task-control-references-core-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot ReferenceSnapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if err = snapshot.Validate(snapshot.Scope, snapshot.Provenance.FetchedAt); err != nil {
		t.Fatal("Core wire/canonical mismatch", err)
	}
	revision, err := ReferenceRevision(snapshot)
	if err != nil || revision != snapshot.Revision {
		t.Fatal(revision, err)
	}
}
