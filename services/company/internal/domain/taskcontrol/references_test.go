package taskcontrol

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestReferenceSnapshotSharedCanonicalFixture(t *testing.T) {
	raw, e := os.ReadFile("../../../../../contracts/fixtures/task-control-references-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	var original ReferenceSnapshot
	if e = json.Unmarshal(raw, &original); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 16, 10, 0, 2, 0, time.UTC)
	if e = original.Validate(original.Scope, now); e != nil {
		t.Fatal("cross-runtime canonical fixture", e)
	}
	if original.Revision != "sha256:8f5f9ebb85c99c812c6e9a6892ffd83ead7f74d4726ca5d56a7be5d8109f4ec0" {
		t.Fatal("fixture revision", original.Revision)
	}
	changed := original
	changed.Users = append([]ReferenceUser{}, original.Users...)
	changed.Users[0], changed.Users[1] = changed.Users[1], changed.Users[0]
	revision, e := ReferenceRevision(changed)
	if e != nil || revision != original.Revision {
		t.Fatal("order changed canonical revision", revision, e)
	}
	changed.Provenance.FetchedAt = now
	changed.Customers.CheckedAt = now
	code := "network_error"
	changed.Customers.ErrorCode = &code
	revision, e = ReferenceRevision(changed)
	if e != nil || revision != original.Revision {
		t.Fatal("transport/time changed semantic revision", revision, e)
	}
	for _, change := range []func(*ReferenceSnapshot){
		func(s *ReferenceSnapshot) { s.Complete = false },
		func(s *ReferenceSnapshot) { s.Scope.Generation++ },
		func(s *ReferenceSnapshot) { s.Users[0].Name = "Tampered" },
		func(s *ReferenceSnapshot) { s.Users[0].ID = "01" },
		func(s *ReferenceSnapshot) { s.Users[0].ID = "9007199254740992" },
		func(s *ReferenceSnapshot) { s.Users[0].ID = s.Users[1].ID },
		func(s *ReferenceSnapshot) { s.Groups = nil },
		func(s *ReferenceSnapshot) { s.FreshUntil = s.FreshUntil.Add(time.Minute) },
		func(s *ReferenceSnapshot) { s.Pipelines[0].Statuses[0].Kind = "unknown" },
		func(s *ReferenceSnapshot) {
			s.Customers.Availability = "unsupported"
			s.Customers.Statuses = []ReferenceCustomerStatus{{ID: "1"}}
		},
	} {
		var s ReferenceSnapshot
		_ = json.Unmarshal(raw, &s)
		change(&s)
		if s.Validate(original.Scope, now) == nil {
			t.Fatal("accepted incomplete, foreign or modified snapshot", s)
		}
	}
}
