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
		Id:           value.ID.String(),
		Name:         value.Name,
		LogoUrl:      cloneString(value.LogoURL),
		OwnerId:      value.OwnerID.String(),
		CreatedAt:    timestamppb.New(value.CreatedAt.UTC()),
		AmoAccountId: cloneString(value.AmoAccountID),
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
		Source:            userSourceToProto(value.Source),
		AccessMode:        userAccessModeToProto(value.AccessMode),
	}
}

func userAccessModeToProto(value string) companyv1.UserAccessMode {
	switch value {
	case "none":
		return companyv1.UserAccessMode_USER_ACCESS_MODE_NONE
	case "password":
		return companyv1.UserAccessMode_USER_ACCESS_MODE_PASSWORD
	case "link":
		return companyv1.UserAccessMode_USER_ACCESS_MODE_LINK
	default:
		return companyv1.UserAccessMode_USER_ACCESS_MODE_UNSPECIFIED
	}
}

func employeeAccessToProto(value application.EmployeeAccess) *companyv1.UserAccess {
	result := &companyv1.UserAccess{
		Mode:      userAccessModeToProto(value.Mode),
		LinkToken: cloneString(value.LinkToken),
	}
	if value.LinkCreatedAt != nil {
		result.LinkCreatedAt = timestamppb.New(value.LinkCreatedAt.UTC())
	}
	return result
}

func userSourceToProto(value string) companyv1.UserSource {
	switch value {
	case "local":
		return companyv1.UserSource_USER_SOURCE_LOCAL
	case "amo":
		return companyv1.UserSource_USER_SOURCE_AMO
	default:
		return companyv1.UserSource_USER_SOURCE_UNSPECIFIED
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
	amoAccountIDSet, amoAccountID := clearableString(request.AmoAccountId)
	return application.UpdateCompanyInput{
		Name:            cloneString(request.Name),
		SetLogoURL:      logoSet,
		LogoURL:         logo,
		SetAmoAccountID: amoAccountIDSet,
		AmoAccountID:    amoAccountID,
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

func scheduleTemplateFromProto(value *companyv1.ScheduleTemplate) (application.ScheduleTemplate, error) {
	if value == nil {
		return application.ScheduleTemplate{}, invalidArgument("Шаблон графика обязателен")
	}
	days := make([]int, len(value.Days))
	for i, day := range value.Days {
		days[i] = int(day)
	}
	result := application.ScheduleTemplate{Type: value.Type, Days: days, Start: value.Start, End: value.End}
	if value.On != nil {
		result.On = int(*value.On)
	}
	if value.Off != nil {
		result.Off = int(*value.Off)
	}
	if value.CycleStart != nil {
		result.CycleStart = *value.CycleStart
	}
	return result, nil
}
func scheduleTemplateToProto(value application.ScheduleTemplate) *companyv1.ScheduleTemplate {
	days := make([]uint32, len(value.Days))
	for i, day := range value.Days {
		days[i] = uint32(day)
	}
	result := &companyv1.ScheduleTemplate{Type: value.Type, Days: days, Start: value.Start, End: value.End}
	if value.Type == "cycle" {
		on, off, start := uint32(value.On), uint32(value.Off), value.CycleStart
		result.On, result.Off, result.CycleStart = &on, &off, &start
	}
	return result
}
func scheduleToProto(value application.UserSchedule) *companyv1.UserSchedule {
	return &companyv1.UserSchedule{UserId: value.UserID.String(), Template: scheduleTemplateToProto(value.Template)}
}
func schedulesToProto(values []application.UserSchedule) []*companyv1.UserSchedule {
	result := make([]*companyv1.UserSchedule, len(values))
	for i, value := range values {
		result[i] = scheduleToProto(value)
	}
	return result
}
func shiftTypeToProto(value string) companyv1.ShiftType {
	switch value {
	case "work":
		return companyv1.ShiftType_SHIFT_TYPE_WORK
	case "off":
		return companyv1.ShiftType_SHIFT_TYPE_OFF
	case "vacation":
		return companyv1.ShiftType_SHIFT_TYPE_VACATION
	case "sick":
		return companyv1.ShiftType_SHIFT_TYPE_SICK
	case "trip":
		return companyv1.ShiftType_SHIFT_TYPE_TRIP
	default:
		return companyv1.ShiftType_SHIFT_TYPE_UNSPECIFIED
	}
}
func shiftTypeFromProto(value companyv1.ShiftType) (string, error) {
	switch value {
	case companyv1.ShiftType_SHIFT_TYPE_WORK:
		return "work", nil
	case companyv1.ShiftType_SHIFT_TYPE_OFF:
		return "off", nil
	case companyv1.ShiftType_SHIFT_TYPE_VACATION:
		return "vacation", nil
	case companyv1.ShiftType_SHIFT_TYPE_SICK:
		return "sick", nil
	case companyv1.ShiftType_SHIFT_TYPE_TRIP:
		return "trip", nil
	default:
		return "", invalidArgument("Некорректный тип смены")
	}
}
func exceptionToProto(value application.ShiftException) *companyv1.ShiftException {
	return &companyv1.ShiftException{Id: value.ID.String(), UserId: value.UserID.String(), Date: value.Date, Type: shiftTypeToProto(value.Type), Start: cloneString(value.Start), End: cloneString(value.End), Note: cloneString(value.Note)}
}
func exceptionsToProto(values []application.ShiftException) []*companyv1.ShiftException {
	result := make([]*companyv1.ShiftException, len(values))
	for i, value := range values {
		result[i] = exceptionToProto(value)
	}
	return result
}
func distributionAlgorithmToProto(value string) companyv1.DistributionAlgorithm {
	switch value {
	case "round_robin":
		return companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_ROUND_ROBIN
	case "least_loaded":
		return companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_LEAST_LOADED
	case "priority":
		return companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_PRIORITY
	default:
		return companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_UNSPECIFIED
	}
}
func distributionAlgorithmFromProto(value companyv1.DistributionAlgorithm) (string, error) {
	switch value {
	case companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_ROUND_ROBIN:
		return "round_robin", nil
	case companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_LEAST_LOADED:
		return "least_loaded", nil
	case companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_PRIORITY:
		return "priority", nil
	default:
		return "", invalidArgument("Некорректный алгоритм распределения")
	}
}
func distributionEventStatusToProto(value string) companyv1.DistributionEventStatus {
	switch value {
	case "accepted":
		return companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_ACCEPTED
	case "in_progress":
		return companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_IN_PROGRESS
	case "reassigned":
		return companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_REASSIGNED
	case "declined":
		return companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_DECLINED
	default:
		return companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_UNSPECIFIED
	}
}
func distributionGroupToProto(value application.DistributionGroup) *companyv1.DistributionGroup {
	return &companyv1.DistributionGroup{Id: value.ID.String(), Name: value.Name, Description: cloneString(value.Description), Active: value.Active, Algorithm: distributionAlgorithmToProto(value.Algorithm), MemberIds: uuidStrings(value.MemberIDs), DisabledMemberIds: uuidStrings(value.DisabledMemberIDs), Source: value.Source, DealLimit: uint32(value.DealLimit), UnclaimedMinutes: uint32(value.UnclaimedMinutes), CreatedAt: timestamppb.New(value.CreatedAt.UTC())}
}
func distributionGroupsToProto(values []application.DistributionGroup) []*companyv1.DistributionGroup {
	result := make([]*companyv1.DistributionGroup, len(values))
	for i, value := range values {
		result[i] = distributionGroupToProto(value)
	}
	return result
}
func distributionEventToProto(value application.DistributionEvent) *companyv1.DistributionEvent {
	return &companyv1.DistributionEvent{Id: value.ID.String(), GroupId: value.GroupID.String(), DealNumber: uint64(value.DealNumber), UserId: value.UserID.String(), Status: distributionEventStatusToProto(value.Status), CreatedAt: timestamppb.New(value.CreatedAt.UTC())}
}
func distributionEventsToProto(values []application.DistributionEvent) []*companyv1.DistributionEvent {
	result := make([]*companyv1.DistributionEvent, len(values))
	for i, value := range values {
		result[i] = distributionEventToProto(value)
	}
	return result
}
