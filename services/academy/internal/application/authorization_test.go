package application

import (
	"testing"

	"github.com/google/uuid"
)

func TestCanReadAcademy(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee", "partner"} {
		if !canReadAcademy(Actor{Role: role}) {
			t.Fatalf("роль %q должна иметь доступ к академии", role)
		}
	}
	for _, role := range []string{"", "unknown"} {
		if canReadAcademy(Actor{Role: role}) {
			t.Fatalf("роль %q не должна иметь доступ к академии", role)
		}
	}
}

func TestVisibleCourseMatrix(t *testing.T) {
	t.Parallel()
	assignedID := uuid.New()
	partnerID := uuid.New()
	audienceCourseID := uuid.New()
	assigned := map[uuid.UUID]struct{}{assignedID: {}}
	partnerAudience := map[uuid.UUID]struct{}{audienceCourseID: {}}
	tests := []struct {
		name   string
		actor  Actor
		course Course
		want   bool
	}{
		{name: "owner sees draft", actor: Actor{Role: "owner"}, course: Course{Status: "draft", Visibility: "restricted"}, want: true},
		{name: "employee cannot see draft", actor: Actor{Role: "employee"}, course: Course{Status: "draft", Visibility: "public"}},
		{name: "employee sees public", actor: Actor{Role: "employee"}, course: Course{Status: "published", Visibility: "public"}, want: true},
		{name: "employee sees company", actor: Actor{Role: "employee"}, course: Course{Status: "published", Visibility: "company"}, want: true},
		{name: "employee sees assigned restricted", actor: Actor{Role: "employee"}, course: Course{ID: assignedID, Status: "published", Visibility: "restricted"}, want: true},
		{name: "employee cannot see unassigned restricted", actor: Actor{Role: "employee"}, course: Course{ID: uuid.New(), Status: "published", Visibility: "restricted"}},
		{name: "partner sees own course", actor: Actor{Role: "partner", UserID: partnerID}, course: Course{OwnerType: "partner", OwnerUserID: &partnerID, LifecycleStatus: "active"}, want: true},
		{name: "partner cannot see another partner course", actor: Actor{Role: "partner", UserID: uuid.New()}, course: Course{OwnerType: "partner", OwnerUserID: &partnerID, LifecycleStatus: "active"}},
		{name: "employee cannot see partner course", actor: Actor{Role: "employee"}, course: Course{OwnerType: "partner", OwnerUserID: &partnerID, Status: "published", LifecycleStatus: "active"}},
		{name: "partner sees company course in audience", actor: Actor{Role: "partner", UserID: partnerID}, course: Course{ID: audienceCourseID, OwnerType: "company", Status: "published", Visibility: "company", LifecycleStatus: "active"}, want: true},
		{name: "partner cannot see company course outside audience", actor: Actor{Role: "partner", UserID: partnerID}, course: Course{ID: uuid.New(), OwnerType: "company", Status: "published", Visibility: "company", LifecycleStatus: "active"}},
		{name: "archived hidden from employee", actor: Actor{Role: "employee"}, course: Course{OwnerType: "company", Status: "published", Visibility: "public", LifecycleStatus: "archived"}},
		{name: "blocked hidden from partner owner", actor: Actor{Role: "partner", UserID: partnerID}, course: Course{OwnerType: "partner", OwnerUserID: &partnerID, LifecycleStatus: "active", DistributionStatus: "blocked"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := visibleCourse(test.actor, test.course, assigned, partnerAudience); got != test.want {
				t.Fatalf("visibleCourse() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAssigneeTypeToEvent(t *testing.T) {
	for _, value := range []string{"user", "position", "department", "external"} {
		if assigneeTypeToEvent(value) == 0 {
			t.Fatalf("тип назначения %q не преобразован", value)
		}
	}
	if assigneeTypeToEvent("unknown") != 0 {
		t.Fatal("неизвестный тип назначения должен оставаться unspecified")
	}
}

func TestEnrollmentAuthorization(t *testing.T) {
	companyID, userID, partnerID := uuid.New(), uuid.New(), uuid.New()
	otherCompany := uuid.New()
	enrollment := Enrollment{CompanyID: companyID, LearnerType: "user", UserID: &userID}
	companyCourse := Course{CompanyID: companyID, OwnerType: "company"}
	partnerCourse := Course{CompanyID: companyID, OwnerType: "partner", OwnerUserID: &partnerID}

	if !canViewEnrollment(Actor{CompanyID: companyID, Role: "employee", UserID: userID}, enrollment, companyCourse) {
		t.Fatal("employee cannot view own enrollment")
	}
	if canViewEnrollment(Actor{CompanyID: companyID, Role: "employee", UserID: uuid.New()}, enrollment, companyCourse) {
		t.Fatal("employee can view another enrollment")
	}
	if !canViewEnrollment(Actor{CompanyID: companyID, Role: "partner", UserID: partnerID}, enrollment, partnerCourse) {
		t.Fatal("partner cannot view enrollment for own course")
	}
	if canViewEnrollment(Actor{CompanyID: otherCompany, Role: "owner"}, enrollment, companyCourse) {
		t.Fatal("cross-tenant owner can view enrollment")
	}
	if !canMutateEnrollment(Actor{CompanyID: companyID, Role: "employee", UserID: userID}, enrollment) {
		t.Fatal("employee cannot mutate own enrollment")
	}
	if canMutateEnrollment(Actor{CompanyID: companyID, Role: "partner", UserID: partnerID}, enrollment) {
		t.Fatal("partner can mutate learner enrollment")
	}
	if canMutateEnrollment(Actor{CompanyID: companyID, Role: "owner", UserID: uuid.New()}, enrollment) {
		t.Fatal("owner can mutate another learner enrollment")
	}
	if canMutateEnrollment(Actor{CompanyID: companyID, Role: "admin", UserID: uuid.New()}, enrollment) {
		t.Fatal("admin can mutate another learner enrollment")
	}
}
