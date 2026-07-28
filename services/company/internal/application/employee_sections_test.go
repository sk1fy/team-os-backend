package application

import (
	"encoding/json"
	"errors"
	"testing"

	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestValidateEmployeeSections(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []string
		ok     bool
	}{
		{name: "one section", values: []string{"schedule"}, ok: true},
		{name: "all sections", values: []string{"schedule", "knowledge", "academy", "distribution"}, ok: true},
		{name: "empty", values: nil},
		{name: "unknown", values: []string{"activity"}},
		{name: "duplicate", values: []string{"academy", "academy"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateEmployeeSections(test.values)
			if test.ok && err != nil {
				t.Fatalf("validateEmployeeSections() error = %v", err)
			}
			if !test.ok {
				var applicationErr *Error
				if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorValidation {
					t.Fatalf("error = %#v, want validation", err)
				}
			}
		})
	}
}

func TestEmployeeSectionCapabilities(t *testing.T) {
	employee := Actor{Role: "employee", SectionAccess: []string{"schedule", "distribution"}}
	if !actorHasSection(employee, "schedule") || !actorHasSection(employee, "distribution") {
		t.Fatal("granted employee section rejected")
	}
	if actorHasSection(employee, "academy") {
		t.Fatal("non-granted employee section allowed")
	}
	if !actorHasSection(Actor{Role: "owner"}, "academy") ||
		!actorHasSection(Actor{Role: "admin"}, "distribution") {
		t.Fatal("administrator full access rejected")
	}
	if actorHasSection(Actor{Role: "partner", SectionAccess: []string{"academy"}}, "academy") {
		t.Fatal("partner section claim must be ignored")
	}
}

func TestEmployeeSectionsComparedAsASet(t *testing.T) {
	if !employeeSectionsEqual(
		[]string{"schedule", "knowledge", "academy"},
		[]string{"academy", "schedule", "knowledge"},
	) {
		t.Fatal("same section set reported as changed")
	}
}

func TestUserEventSnapshotUsesProtoEmployeeSectionNames(t *testing.T) {
	payload, err := json.Marshal(userEventSnapshot(User{
		Role: "employee", Status: "active",
		SectionAccess: []string{"schedule", "distribution"},
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot eventsv1.OrgUserSnapshot
	if err = protojson.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.SectionAccess) != 2 ||
		snapshot.SectionAccess[0] != eventsv1.OrgEmployeeSection_ORG_EMPLOYEE_SECTION_SCHEDULE ||
		snapshot.SectionAccess[1] != eventsv1.OrgEmployeeSection_ORG_EMPLOYEE_SECTION_DISTRIBUTION {
		t.Fatalf("snapshot sections = %#v", snapshot.SectionAccess)
	}
}
