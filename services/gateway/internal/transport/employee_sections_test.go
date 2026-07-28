package transport

import (
	"testing"
	"time"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestUpdateUserRequestMapsEmployeeSections(t *testing.T) {
	sections := []api.EmployeeSection{api.Schedule, api.Distribution}
	request, err := updateUserRequest(uuid.New(), api.UpdateUserInput{SectionAccess: &sections})
	if err != nil {
		t.Fatal(err)
	}
	if !request.UpdateSectionAccess ||
		len(request.SectionAccess) != 2 ||
		request.SectionAccess[0] != companyv1.EmployeeSection_EMPLOYEE_SECTION_SCHEDULE ||
		request.SectionAccess[1] != companyv1.EmployeeSection_EMPLOYEE_SECTION_DISTRIBUTION {
		t.Fatalf("request = %#v", request)
	}
}

func TestEmployeeSectionsMapFromCompanyUser(t *testing.T) {
	value := &companyv1.User{
		Id: uuid.NewString(), Email: "employee@example.com", FirstName: "Иван",
		Role: companyv1.UserRole_USER_ROLE_EMPLOYEE, Status: companyv1.UserStatus_USER_STATUS_ACTIVE,
		CreatedAt: timestamppb.New(time.Now()),
		SectionAccess: []companyv1.EmployeeSection{
			companyv1.EmployeeSection_EMPLOYEE_SECTION_KNOWLEDGE,
			companyv1.EmployeeSection_EMPLOYEE_SECTION_ACADEMY,
		},
	}
	user, err := userFromProto(value)
	if err != nil {
		t.Fatal(err)
	}
	if user.SectionAccess == nil || len(*user.SectionAccess) != 2 ||
		(*user.SectionAccess)[0] != api.Knowledge || (*user.SectionAccess)[1] != api.Academy {
		t.Fatalf("user = %#v", user)
	}
}
