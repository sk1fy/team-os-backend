package grpc

import (
	"strings"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func companyToProto(value application.Company) *companyv1.Company {
	return &companyv1.Company{
		Id:        value.ID.String(),
		Name:      value.Name,
		LogoUrl:   cloneString(value.LogoURL),
		OwnerId:   value.OwnerID.String(),
		CreatedAt: timestamppb.New(value.CreatedAt.UTC()),
	}
}

func userToProto(value application.User) *companyv1.User {
	positionIDs := uuidStrings(value.PositionIDs)
	var vacationAllowance *uint32
	if value.VacationAllowance != nil && *value.VacationAllowance >= 0 {
		converted := uint32(*value.VacationAllowance)
		vacationAllowance = &converted
	}
	return &companyv1.User{
		Id:                value.ID.String(),
		Email:             value.Email,
		FirstName:         value.FirstName,
		LastName:          value.LastName,
		AvatarUrl:         cloneString(value.AvatarURL),
		Phone:             cloneString(value.Phone),
		Role:              userRoleToProto(value.Role),
		Status:            userStatusToProto(value.Status),
		PositionIds:       positionIDs,
		BirthDate:         cloneString(value.BirthDate),
		HiredAt:           cloneString(value.HiredAt),
		VacationAllowance: vacationAllowance,
		CreatedAt:         timestamppb.New(value.CreatedAt.UTC()),
	}
}

func usersToProto(values []application.User) []*companyv1.User {
	result := make([]*companyv1.User, len(values))
	for index := range values {
		result[index] = userToProto(values[index])
	}
	return result
}

func departmentToProto(value application.Department) *companyv1.Department {
	order := uint32(0)
	if value.Order > 0 {
		order = uint32(value.Order)
	}
	return &companyv1.Department{
		Id:                   value.ID.String(),
		Name:                 value.Name,
		ParentId:             optionalUUIDString(value.ParentID),
		HeadUserId:           optionalUUIDString(value.HeadUserID),
		ValuableFinalProduct: cloneString(value.ValuableFinalProduct),
		Order:                order,
	}
}

func departmentsToProto(values []application.Department) []*companyv1.Department {
	result := make([]*companyv1.Department, len(values))
	for index := range values {
		result[index] = departmentToProto(values[index])
	}
	return result
}

func positionToProto(value application.Position) *companyv1.Position {
	level := uint32(0)
	if value.Level > 0 {
		level = uint32(value.Level)
	}
	return &companyv1.Position{
		Id:                value.ID.String(),
		Name:              value.Name,
		DepartmentId:      value.DepartmentID.String(),
		Level:             &level,
		Description:       cloneString(value.Description),
		ArticleIds:        uuidStrings(value.ArticleIDs),
		RequiredCourseIds: uuidStrings(value.RequiredCourseIDs),
	}
}

func positionsToProto(values []application.Position) []*companyv1.Position {
	result := make([]*companyv1.Position, len(values))
	for index := range values {
		result[index] = positionToProto(values[index])
	}
	return result
}

func inviteToProto(value application.Invite) *companyv1.Invite {
	return &companyv1.Invite{
		Id:           value.ID.String(),
		Email:        cloneString(value.Email),
		Token:        value.Token,
		Role:         userRoleToProto(value.Role),
		PositionId:   optionalUUIDString(value.PositionID),
		DepartmentId: optionalUUIDString(value.DepartmentID),
		InvitedById:  value.InvitedByID.String(),
		Status:       inviteStatusToProto(value.Status),
		CreatedAt:    timestamppb.New(value.CreatedAt.UTC()),
	}
}

func invitesToProto(values []application.Invite) []*companyv1.Invite {
	result := make([]*companyv1.Invite, len(values))
	for index := range values {
		result[index] = inviteToProto(values[index])
	}
	return result
}

func authSessionToProto(value application.AuthResult) *companyv1.AuthSession {
	return &companyv1.AuthSession{
		AccessToken:      value.AccessToken,
		RefreshToken:     value.RefreshToken,
		RefreshExpiresAt: timestamppb.New(value.RefreshExpiresAt.UTC()),
		User:             userToProto(value.User),
	}
}

func userRoleToProto(value string) companyv1.UserRole {
	switch value {
	case "owner":
		return companyv1.UserRole_USER_ROLE_OWNER
	case "admin":
		return companyv1.UserRole_USER_ROLE_ADMIN
	case "employee":
		return companyv1.UserRole_USER_ROLE_EMPLOYEE
	case "partner":
		return companyv1.UserRole_USER_ROLE_PARTNER
	default:
		return companyv1.UserRole_USER_ROLE_UNSPECIFIED
	}
}

func userRoleFromProto(value companyv1.UserRole) (string, error) {
	switch value {
	case companyv1.UserRole_USER_ROLE_OWNER:
		return "owner", nil
	case companyv1.UserRole_USER_ROLE_ADMIN:
		return "admin", nil
	case companyv1.UserRole_USER_ROLE_EMPLOYEE:
		return "employee", nil
	case companyv1.UserRole_USER_ROLE_PARTNER:
		return "partner", nil
	default:
		return "", invalidArgument("Некорректная роль пользователя")
	}
}

func userStatusToProto(value string) companyv1.UserStatus {
	switch value {
	case "active":
		return companyv1.UserStatus_USER_STATUS_ACTIVE
	case "invited":
		return companyv1.UserStatus_USER_STATUS_INVITED
	case "deactivated":
		return companyv1.UserStatus_USER_STATUS_DEACTIVATED
	default:
		return companyv1.UserStatus_USER_STATUS_UNSPECIFIED
	}
}

func userStatusFromProto(value companyv1.UserStatus) (string, error) {
	switch value {
	case companyv1.UserStatus_USER_STATUS_ACTIVE:
		return "active", nil
	case companyv1.UserStatus_USER_STATUS_INVITED:
		return "invited", nil
	case companyv1.UserStatus_USER_STATUS_DEACTIVATED:
		return "deactivated", nil
	default:
		return "", invalidArgument("Некорректный статус пользователя")
	}
}

func inviteStatusToProto(value string) companyv1.InviteStatus {
	switch value {
	case "pending":
		return companyv1.InviteStatus_INVITE_STATUS_PENDING
	case "accepted":
		return companyv1.InviteStatus_INVITE_STATUS_ACCEPTED
	case "expired":
		return companyv1.InviteStatus_INVITE_STATUS_EXPIRED
	default:
		return companyv1.InviteStatus_INVITE_STATUS_UNSPECIFIED
	}
}

func parseUUID(value, entity string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, invalidArgument("Некорректный идентификатор " + entity)
	}
	return parsed, nil
}

func parseOptionalUUID(value *string, entity string) (*uuid.UUID, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseUUID(*value, entity)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseUUIDs(values []string, entity string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, len(values))
	for index, value := range values {
		parsed, err := parseUUID(value, entity)
		if err != nil {
			return nil, err
		}
		result[index] = parsed
	}
	return result, nil
}

func optionalLevel(value *uint32) (*int16, error) {
	if value == nil {
		return nil, nil
	}
	if *value > 4 {
		return nil, invalidArgument("Уровень должности должен быть от 0 до 4")
	}
	converted := int16(*value)
	return &converted, nil
}

func optionalVacationAllowance(value *uint32) (*int16, error) {
	if value == nil {
		return nil, nil
	}
	if *value > 366 {
		return nil, invalidArgument("Норма отпуска должна быть от 0 до 366 дней")
	}
	converted := int16(*value)
	return &converted, nil
}

func updateCurrentUserInput(request *companyv1.UpdateCurrentUserRequest) application.UpdateCurrentUserInput {
	phoneSet, phone := clearableString(request.Phone)
	avatarSet, avatar := clearableString(request.AvatarUrl)
	return application.UpdateCurrentUserInput{
		FirstName:    cloneString(request.FirstName),
		LastName:     cloneString(request.LastName),
		SetPhone:     phoneSet,
		Phone:        phone,
		SetAvatarURL: avatarSet,
		AvatarURL:    avatar,
	}
}

func updateCompanyInput(request *companyv1.UpdateCompanyRequest) application.UpdateCompanyInput {
	logoSet, logo := clearableString(request.LogoUrl)
	return application.UpdateCompanyInput{
		Name:       cloneString(request.Name),
		SetLogoURL: logoSet,
		LogoURL:    logo,
	}
}

func clearableString(value *string) (bool, *string) {
	if value == nil {
		return false, nil
	}
	if strings.TrimSpace(*value) == "" {
		return true, nil
	}
	return true, cloneString(value)
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func optionalUUIDString(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].String()
	}
	return result
}
