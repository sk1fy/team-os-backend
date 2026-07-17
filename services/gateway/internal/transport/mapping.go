package transport

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func userFromProto(value *companyv1.User) (api.User, error) {
	if value == nil {
		return api.User{}, errors.New("company returned an empty user")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.User{}, fmt.Errorf("invalid user id: %w", err)
	}
	positionIDs, err := UUIDsFromStrings(value.GetPositionIds())
	if err != nil {
		return api.User{}, err
	}
	role, err := roleFromProto(value.GetRole())
	if err != nil {
		return api.User{}, err
	}
	status, err := statusFromProto(value.GetStatus())
	if err != nil {
		return api.User{}, err
	}
	createdAt := time.Time{}
	if value.GetCreatedAt() != nil {
		createdAt = value.GetCreatedAt().AsTime()
	}
	result := api.User{
		Id: id, Email: openapi_types.Email(value.GetEmail()), FirstName: value.GetFirstName(),
		LastName: value.GetLastName(), AvatarUrl: value.AvatarUrl, Phone: value.Phone,
		Role: role, Status: status, PositionIds: positionIDs, CreatedAt: createdAt,
	}
	if source := sourceFromProto(value.GetSource()); source != nil {
		result.Source = source
	}
	if value.BirthDate != nil {
		date, parseErr := time.Parse(time.DateOnly, value.GetBirthDate())
		if parseErr != nil {
			return api.User{}, fmt.Errorf("invalid birth date: %w", parseErr)
		}
		converted := openapi_types.Date{Time: date}
		result.BirthDate = &converted
	}
	if value.HiredAt != nil {
		date, parseErr := time.Parse(time.DateOnly, value.GetHiredAt())
		if parseErr != nil {
			return api.User{}, fmt.Errorf("invalid hired date: %w", parseErr)
		}
		converted := openapi_types.Date{Time: date}
		result.HiredAt = &converted
	}
	if value.VacationAllowance != nil {
		converted := int(value.GetVacationAllowance())
		result.VacationAllowance = &converted
	}
	return result, nil
}

func sourceFromProto(value companyv1.UserSource) *api.UserSource {
	var source api.UserSource
	switch value {
	case companyv1.UserSource_USER_SOURCE_LOCAL:
		source = api.Local
	case companyv1.UserSource_USER_SOURCE_AMO:
		source = api.Amo
	default:
		return nil
	}
	return &source
}

func usersFromProto(values []*companyv1.User) ([]api.User, error) {
	result := make([]api.User, len(values))
	for index, value := range values {
		converted, err := userFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func companyFromProto(value *companyv1.Company) (api.Company, error) {
	if value == nil {
		return api.Company{}, errors.New("company returned an empty company")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Company{}, err
	}
	ownerID, err := uuid.Parse(value.GetOwnerId())
	if err != nil {
		return api.Company{}, err
	}
	createdAt := time.Time{}
	if value.GetCreatedAt() != nil {
		createdAt = value.GetCreatedAt().AsTime()
	}
	return api.Company{Id: id, Name: value.GetName(), LogoUrl: value.LogoUrl, AmoAccountId: value.AmoAccountId, OwnerId: ownerID, CreatedAt: createdAt}, nil
}

func departmentFromProto(value *companyv1.Department) (api.Department, error) {
	if value == nil {
		return api.Department{}, errors.New("company returned an empty department")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Department{}, err
	}
	parent := nullable.NewNullNullable[string]()
	if value.ParentId != nil {
		if _, err = uuid.Parse(value.GetParentId()); err != nil {
			return api.Department{}, err
		}
		parent = nullable.NewNullableWithValue(value.GetParentId())
	}
	headID, err := optionalUUID(value.HeadUserId)
	if err != nil {
		return api.Department{}, err
	}
	return api.Department{
		Id: id, Name: value.GetName(), ParentId: parent, HeadUserId: headID,
		ValuableFinalProduct: value.ValuableFinalProduct, Order: int(value.GetOrder()),
	}, nil
}

func departmentsFromProto(values []*companyv1.Department) ([]api.Department, error) {
	result := make([]api.Department, len(values))
	for index, value := range values {
		converted, err := departmentFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func positionFromProto(value *companyv1.Position) (api.Position, error) {
	if value == nil {
		return api.Position{}, errors.New("company returned an empty position")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Position{}, err
	}
	departmentID, err := uuid.Parse(value.GetDepartmentId())
	if err != nil {
		return api.Position{}, err
	}
	articleIDs, err := UUIDsFromStrings(value.GetArticleIds())
	if err != nil {
		return api.Position{}, err
	}
	courseIDs, err := UUIDsFromStrings(value.GetRequiredCourseIds())
	if err != nil {
		return api.Position{}, err
	}
	result := api.Position{
		Id: id, Name: value.GetName(), DepartmentId: departmentID,
		Description: value.Description, ArticleIds: articleIDs, RequiredCourseIds: courseIDs,
	}
	if value.Level != nil {
		level := int(value.GetLevel())
		result.Level = &level
	}
	return result, nil
}

func positionsFromProto(values []*companyv1.Position) ([]api.Position, error) {
	result := make([]api.Position, len(values))
	for index, value := range values {
		converted, err := positionFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func inviteFromProto(value *companyv1.Invite) (api.Invite, error) {
	if value == nil {
		return api.Invite{}, errors.New("company returned an empty invite")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Invite{}, err
	}
	invitedByID, err := uuid.Parse(value.GetInvitedById())
	if err != nil {
		return api.Invite{}, err
	}
	positionID, err := optionalUUID(value.PositionId)
	if err != nil {
		return api.Invite{}, err
	}
	departmentID, err := optionalUUID(value.DepartmentId)
	if err != nil {
		return api.Invite{}, err
	}
	role, err := roleFromProto(value.GetRole())
	if err != nil {
		return api.Invite{}, err
	}
	status, err := inviteStatusFromProto(value.GetStatus())
	if err != nil {
		return api.Invite{}, err
	}
	var email *openapi_types.Email
	if value.Email != nil {
		converted := openapi_types.Email(value.GetEmail())
		email = &converted
	}
	createdAt := time.Time{}
	if value.GetCreatedAt() != nil {
		createdAt = value.GetCreatedAt().AsTime()
	}
	return api.Invite{
		Id: id, Email: email, Token: value.GetToken(), Role: role,
		PositionId: positionID, DepartmentId: departmentID, InvitedById: invitedByID,
		Status: status, CreatedAt: createdAt,
	}, nil
}

func invitesFromProto(values []*companyv1.Invite) ([]api.Invite, error) {
	result := make([]api.Invite, len(values))
	for index, value := range values {
		converted, err := inviteFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func UUIDsFromStrings(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, len(values))
	for index, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID %q: %w", value, err)
		}
		result[index] = parsed
	}
	return result, nil
}

func stringsFromUUIDs(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func optionalUUID(value *string) (*uuid.UUID, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := uuid.Parse(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func roleToProto(value api.UserRole) companyv1.UserRole {
	switch value {
	case api.Owner:
		return companyv1.UserRole_USER_ROLE_OWNER
	case api.Admin:
		return companyv1.UserRole_USER_ROLE_ADMIN
	case api.Employee:
		return companyv1.UserRole_USER_ROLE_EMPLOYEE
	case api.Partner:
		return companyv1.UserRole_USER_ROLE_PARTNER
	default:
		return companyv1.UserRole_USER_ROLE_UNSPECIFIED
	}
}

func roleFromProto(value companyv1.UserRole) (api.UserRole, error) {
	switch value {
	case companyv1.UserRole_USER_ROLE_OWNER:
		return api.Owner, nil
	case companyv1.UserRole_USER_ROLE_ADMIN:
		return api.Admin, nil
	case companyv1.UserRole_USER_ROLE_EMPLOYEE:
		return api.Employee, nil
	case companyv1.UserRole_USER_ROLE_PARTNER:
		return api.Partner, nil
	default:
		return "", errors.New("company returned an invalid user role")
	}
}

func statusToProto(value api.UserStatus) companyv1.UserStatus {
	switch value {
	case api.Active:
		return companyv1.UserStatus_USER_STATUS_ACTIVE
	case api.Invited:
		return companyv1.UserStatus_USER_STATUS_INVITED
	case api.Deactivated:
		return companyv1.UserStatus_USER_STATUS_DEACTIVATED
	default:
		return companyv1.UserStatus_USER_STATUS_UNSPECIFIED
	}
}

func statusFromProto(value companyv1.UserStatus) (api.UserStatus, error) {
	switch value {
	case companyv1.UserStatus_USER_STATUS_ACTIVE:
		return api.Active, nil
	case companyv1.UserStatus_USER_STATUS_INVITED:
		return api.Invited, nil
	case companyv1.UserStatus_USER_STATUS_DEACTIVATED:
		return api.Deactivated, nil
	default:
		return "", errors.New("company returned an invalid user status")
	}
}

func inviteStatusFromProto(value companyv1.InviteStatus) (api.InviteStatus, error) {
	switch value {
	case companyv1.InviteStatus_INVITE_STATUS_PENDING:
		return api.Pending, nil
	case companyv1.InviteStatus_INVITE_STATUS_ACCEPTED:
		return api.Accepted, nil
	case companyv1.InviteStatus_INVITE_STATUS_EXPIRED:
		return api.Expired, nil
	default:
		return "", errors.New("company returned an invalid invite status")
	}
}

func clearableDateString(value *api.ClearableDate) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if date, err := value.AsLocalDate(); err == nil {
		result := date.String()
		return &result, nil
	}
	text, err := value.AsClearableDate1()
	if err != nil {
		return nil, err
	}
	result := string(text)
	return &result, nil
}

func clearablePhoneString(value *api.ClearablePhone) (*string, error) {
	if value == nil {
		return nil, nil
	}
	phone, err := value.AsPhone()
	if err != nil {
		return nil, err
	}
	result := string(phone)
	return &result, nil
}
