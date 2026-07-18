package grpc

import (
	"testing"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUserRoleMapping(t *testing.T) {
	tests := []struct {
		proto  companyv1.UserRole
		domain string
	}{
		{proto: companyv1.UserRole_USER_ROLE_OWNER, domain: "owner"},
		{proto: companyv1.UserRole_USER_ROLE_ADMIN, domain: "admin"},
		{proto: companyv1.UserRole_USER_ROLE_EMPLOYEE, domain: "employee"},
		{proto: companyv1.UserRole_USER_ROLE_PARTNER, domain: "partner"},
	}
	for _, test := range tests {
		t.Run(test.domain, func(t *testing.T) {
			domain, err := userRoleFromProto(test.proto)
			if err != nil {
				t.Fatal(err)
			}
			if domain != test.domain || userRoleToProto(domain) != test.proto {
				t.Fatalf("round trip = %q / %v", domain, userRoleToProto(domain))
			}
		})
	}
	if _, err := userRoleFromProto(companyv1.UserRole_USER_ROLE_UNSPECIFIED); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unspecified role error = %v", err)
	}
	if got := userRoleToProto("future-role"); got != companyv1.UserRole_USER_ROLE_UNSPECIFIED {
		t.Fatalf("unknown domain role = %v", got)
	}
}

func TestStatusAndInviteEnumMapping(t *testing.T) {
	if got := userStatusToProto("deactivated"); got != companyv1.UserStatus_USER_STATUS_DEACTIVATED {
		t.Fatalf("status = %v", got)
	}
	if got := inviteStatusToProto("accepted"); got != companyv1.InviteStatus_INVITE_STATUS_ACCEPTED {
		t.Fatalf("invite status = %v", got)
	}
	if got := userSourceToProto("amo"); got != companyv1.UserSource_USER_SOURCE_AMO {
		t.Fatalf("source = %v", got)
	}
	if got := userAccessModeToProto("link"); got != companyv1.UserAccessMode_USER_ACCESS_MODE_LINK {
		t.Fatalf("access mode = %v", got)
	}
	if _, err := userStatusFromProto(companyv1.UserStatus_USER_STATUS_UNSPECIFIED); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unspecified status error = %v", err)
	}
}

func TestUpdateCurrentUserOptionalMapping(t *testing.T) {
	absent := updateCurrentUserInput(&companyv1.UpdateCurrentUserRequest{})
	if absent.SetPhone || absent.Phone != nil {
		t.Fatalf("absent optionals = %#v", absent)
	}

	empty := ""
	clear := updateCurrentUserInput(&companyv1.UpdateCurrentUserRequest{Phone: &empty})
	if !clear.SetPhone || clear.Phone != nil {
		t.Fatalf("clear optionals = %#v", clear)
	}

	phone := "+7 900 000-00-00"
	update := updateCurrentUserInput(&companyv1.UpdateCurrentUserRequest{Phone: &phone})
	if !update.SetPhone || update.Phone == nil || *update.Phone != phone {
		t.Fatalf("set phone = %#v", update)
	}
	phone = "changed after mapping"
	if *update.Phone != "+7 900 000-00-00" {
		t.Fatal("transport input aliases the protobuf request")
	}
}

func TestUpdateCompanyAmoAccountIDMapping(t *testing.T) {
	absent := updateCompanyInput(&companyv1.UpdateCompanyRequest{})
	if absent.SetAmoAccountID || absent.AmoAccountID != nil {
		t.Fatalf("absent amo account id = %#v", absent)
	}
	empty := ""
	clear := updateCompanyInput(&companyv1.UpdateCompanyRequest{AmoAccountId: &empty})
	if !clear.SetAmoAccountID || clear.AmoAccountID != nil {
		t.Fatalf("clear amo account id = %#v", clear)
	}
	value := "31355990"
	update := updateCompanyInput(&companyv1.UpdateCompanyRequest{AmoAccountId: &value})
	if !update.SetAmoAccountID || update.AmoAccountID == nil || *update.AmoAccountID != value {
		t.Fatalf("set amo account id = %#v", update)
	}
}

func TestProtoOptionalOutputMapping(t *testing.T) {
	withoutOptionals := userToProto(application.User{})
	if withoutOptionals.Phone != nil || withoutOptionals.AvatarUrl != nil || withoutOptionals.BirthDate != nil || withoutOptionals.VacationAllowance != nil {
		t.Fatalf("unexpected optional values: %#v", withoutOptionals)
	}

	level := int16(0)
	position := positionToProto(application.Position{Level: level})
	if position.Level == nil || *position.Level != 0 {
		t.Fatalf("position level = %#v", position.Level)
	}
}
