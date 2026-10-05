package application

import (
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
)

func TestDistributionReferenceSnapshotValidation(t *testing.T) {
	now := time.Now()
	base := corebridge.References{State: "fresh", FetchedAt: now, FreshUntil: now.Add(time.Minute), Users: []corebridge.User{{ID: "9007199254740993", IsActive: true}}, Pipelines: []corebridge.Pipeline{{ID: "2", Statuses: []corebridge.Status{{ID: "3"}}}}}
	if e := validateDistributionReferences(base, now); e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*corebridge.References){func(r *corebridge.References) { r.State = "partial" }, func(r *corebridge.References) { r.FreshUntil = now }, func(r *corebridge.References) { r.Users = append(r.Users, r.Users[0]) }, func(r *corebridge.References) {
		r.Pipelines[0].Statuses = append(r.Pipelines[0].Statuses, r.Pipelines[0].Statuses[0])
	}, func(r *corebridge.References) { r.Users[0].ID = "9223372036854775808" }} {
		r := base
		r.Users = append([]corebridge.User(nil), base.Users...)
		r.Pipelines = append([]corebridge.Pipeline(nil), base.Pipelines...)
		edit(&r)
		if e := validateDistributionReferences(r, now); e == nil {
			t.Fatal("accepted invalid snapshot")
		}
	}
}
func TestCRMIDsStayCanonical(t *testing.T) {
	for _, s := range []string{"0", "01", "-1", "1.0", " 1", "9223372036854775808"} {
		if validCRMID(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	if !validCRMID("9223372036854775807") {
		t.Fatal("rejected int64 max")
	}
}
