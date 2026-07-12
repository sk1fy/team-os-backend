package seed

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMapIDPreservesUUIDAndMapsLegacyIDDeterministically(t *testing.T) {
	valid := "3A9D591E-908E-4D20-8312-6BA7E558880E"
	mappedValid, err := MapID(valid)
	if err != nil {
		t.Fatal(err)
	}
	if mappedValid != uuid.MustParse(valid) {
		t.Fatalf("MapID(valid) = %s", mappedValid)
	}

	first, err := MapID("user-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := MapID("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.String() != "f5a28cb4-0933-5a27-b2ce-f330a446c3af" {
		t.Fatalf("legacy mapping = %s / %s", first, second)
	}
	if first.Version() != 5 {
		t.Fatalf("legacy mapping version = %d, want 5", first.Version())
	}
	if _, err := MapID("  "); err == nil {
		t.Fatal("MapID(empty) expected an error")
	}
}

func TestNormalizeRewritesReferencesAndUsesCurrentUserAsOwner(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	parentID := "department-root"
	headID := "user-1"
	positionID := "position-1"
	departmentID := "department-child"
	fixtures := Fixtures{
		Company: CompanyFixture{
			ID: "company-1", Name: "Ромашка", OwnerID: "obsolete-owner",
			CreatedAt: "2026-01-01T10:00:00Z",
		},
		CurrentUserID: "user-1",
		Users: []UserFixture{{
			ID: "user-1", Email: "OWNER@EXAMPLE.COM", FirstName: " Анна ", LastName: "Смирнова",
			Role: "owner", Status: "active", PositionIDs: []string{positionID},
			BirthDate: "1988-07-08", HiredAt: "2019-11-04", CreatedAt: "2026-01-02T10:00:00Z",
		}},
		Departments: []DepartmentFixture{
			{ID: parentID, Name: "Ромашка", HeadUserID: &headID, Order: 0},
			{ID: departmentID, Name: "Продажи", ParentID: &parentID, Order: 0},
		},
		Positions: []PositionFixture{{
			ID: positionID, Name: "Директор", DepartmentID: departmentID,
			ArticleIDs: []string{"article-1"}, RequiredCourseIDs: []string{"course-1"},
		}},
		Invites: []InviteFixture{{
			ID: "invite-1", Token: "demo-token", Role: "employee", PositionID: &positionID,
			DepartmentID: &departmentID, InvitedByID: "user-1", Status: "pending",
			CreatedAt: "2026-07-10T12:00:00Z",
		}},
	}

	dataset, err := Normalize(fixtures, now)
	if err != nil {
		t.Fatal(err)
	}
	wantOwner, _ := MapID("user-1")
	if dataset.Company.OwnerID != wantOwner || dataset.Users[0].ID != wantOwner {
		t.Fatalf("owner/user mapping mismatch: %#v / %#v", dataset.Company, dataset.Users[0])
	}
	if dataset.Users[0].Email != "owner@example.com" || dataset.Users[0].FirstName != "Анна" {
		t.Fatalf("user normalization failed: %#v", dataset.Users[0])
	}
	if dataset.Users[0].PositionID == nil || *dataset.Users[0].PositionID != dataset.Positions[0].ID {
		t.Fatalf("user position reference was not rewritten: %#v", dataset.Users[0])
	}
	if dataset.Departments[1].ParentID == nil || *dataset.Departments[1].ParentID != dataset.Departments[0].ID {
		t.Fatalf("department parent reference was not rewritten: %#v", dataset.Departments[1])
	}
	if dataset.Departments[0].HeadUserID == nil || *dataset.Departments[0].HeadUserID != wantOwner {
		t.Fatalf("department head reference was not rewritten: %#v", dataset.Departments[0])
	}
	if dataset.Invites[0].PositionID == nil || *dataset.Invites[0].PositionID != dataset.Positions[0].ID {
		t.Fatalf("invite position reference was not rewritten: %#v", dataset.Invites[0])
	}
	if dataset.Invites[0].InvitedByID != wantOwner {
		t.Fatalf("invite inviter reference was not rewritten: %#v", dataset.Invites[0])
	}
	articleID, _ := MapID("article-1")
	if len(dataset.Positions[0].ArticleIDs) != 1 || dataset.Positions[0].ArticleIDs[0] != articleID {
		t.Fatalf("external article reference was not rewritten: %#v", dataset.Positions[0])
	}
	wantExpiry := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if !dataset.Invites[0].ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("expiresAt = %s, want %s", dataset.Invites[0].ExpiresAt, wantExpiry)
	}
}

func TestNormalizeRejectsBrokenReferencesAndMultiplePositions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Fixtures)
		want   string
	}{
		{
			name: "unknown department",
			mutate: func(fixtures *Fixtures) {
				fixtures.Positions[0].DepartmentID = "missing"
			},
			want: "не найден",
		},
		{
			name: "multiple positions",
			mutate: func(fixtures *Fixtures) {
				fixtures.Users[0].PositionIDs = []string{"position-1", "position-2"}
			},
			want: "не более одной",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixtures := minimalFixtures()
			test.mutate(&fixtures)
			_, err := Normalize(fixtures, time.Now())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Normalize() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func minimalFixtures() Fixtures {
	return Fixtures{
		Company:       CompanyFixture{ID: "company-1", Name: "Ромашка", OwnerID: "user-1"},
		CurrentUserID: "user-1",
		Users: []UserFixture{{
			ID: "user-1", Email: "owner@example.com", FirstName: "Анна", LastName: "Смирнова",
			Role: "owner", Status: "active", PositionIDs: []string{"position-1"},
		}},
		Departments: []DepartmentFixture{{ID: "department-1", Name: "Ромашка", Order: 0}},
		Positions: []PositionFixture{{
			ID: "position-1", Name: "Директор", DepartmentID: "department-1",
		}},
		Invites: []InviteFixture{},
	}
}
