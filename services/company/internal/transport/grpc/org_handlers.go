package grpc

import (
	"context"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

func (s *Server) GetDepartments(ctx context.Context, request *companyv1.GetDepartmentsRequest) (*companyv1.GetDepartmentsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	departments, err := s.application.ListDepartments(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetDepartmentsResponse{Departments: departmentsToProto(departments)}, nil
}

func (s *Server) CreateDepartment(ctx context.Context, request *companyv1.CreateDepartmentRequest) (*companyv1.CreateDepartmentResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	parentID, err := parseOptionalUUID(request.ParentId, "родительского отдела")
	if err != nil {
		return nil, err
	}
	headUserID, err := parseOptionalUUID(request.HeadUserId, "руководителя")
	if err != nil {
		return nil, err
	}
	department, err := s.application.CreateDepartment(ctx, actor, application.CreateDepartmentInput{
		Name:                 request.Name,
		ParentID:             parentID,
		HeadUserID:           headUserID,
		ValuableFinalProduct: cloneString(request.ValuableFinalProduct),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.CreateDepartmentResponse{Department: departmentToProto(department)}, nil
}

func (s *Server) UpdateDepartment(ctx context.Context, request *companyv1.UpdateDepartmentRequest) (*companyv1.UpdateDepartmentResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "отдела")
	if err != nil {
		return nil, err
	}
	if request.ClearHeadUserId && request.HeadUserId != nil {
		return nil, invalidArgument("Нельзя одновременно назначить и очистить руководителя")
	}
	if request.ClearValuableFinalProduct && request.ValuableFinalProduct != nil {
		return nil, invalidArgument("Нельзя одновременно задать и очистить ценный конечный продукт")
	}
	headUserID, err := parseOptionalUUID(request.HeadUserId, "руководителя")
	if err != nil {
		return nil, err
	}
	valuableFinalProduct := cloneString(request.ValuableFinalProduct)
	if request.ClearValuableFinalProduct {
		valuableFinalProduct = nil
	}
	department, err := s.application.UpdateDepartment(ctx, actor, application.UpdateDepartmentInput{
		ID:                      id,
		Name:                    cloneString(request.Name),
		SetHeadUserID:           request.HeadUserId != nil || request.ClearHeadUserId,
		HeadUserID:              headUserID,
		SetValuableFinalProduct: request.ValuableFinalProduct != nil || request.ClearValuableFinalProduct,
		ValuableFinalProduct:    valuableFinalProduct,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.UpdateDepartmentResponse{Department: departmentToProto(department)}, nil
}

func (s *Server) DeleteDepartment(ctx context.Context, request *companyv1.DeleteDepartmentRequest) (*companyv1.DeleteDepartmentResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "отдела")
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteDepartment(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &companyv1.DeleteDepartmentResponse{}, nil
}

func (s *Server) MoveDepartment(ctx context.Context, request *companyv1.MoveDepartmentRequest) (*companyv1.MoveDepartmentResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "отдела")
	if err != nil {
		return nil, err
	}
	parentID, err := parseOptionalUUID(request.ParentId, "родительского отдела")
	if err != nil {
		return nil, err
	}
	department, err := s.application.MoveDepartment(ctx, actor, id, parentID)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.MoveDepartmentResponse{Department: departmentToProto(department)}, nil
}

func (s *Server) GetPositions(ctx context.Context, request *companyv1.GetPositionsRequest) (*companyv1.GetPositionsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	positions, err := s.application.ListPositions(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetPositionsResponse{Positions: positionsToProto(positions)}, nil
}

func (s *Server) GetPosition(ctx context.Context, request *companyv1.GetPositionRequest) (*companyv1.GetPositionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "должности")
	if err != nil {
		return nil, err
	}
	position, err := s.application.GetPosition(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetPositionResponse{Position: positionToProto(position)}, nil
}

func (s *Server) CreatePosition(ctx context.Context, request *companyv1.CreatePositionRequest) (*companyv1.CreatePositionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	departmentID, err := parseUUID(request.DepartmentId, "отдела")
	if err != nil {
		return nil, err
	}
	level, err := optionalLevel(request.Level)
	if err != nil {
		return nil, err
	}
	position, err := s.application.CreatePosition(ctx, actor, application.CreatePositionInput{
		Name:         request.Name,
		DepartmentID: departmentID,
		Level:        level,
		Description:  cloneString(request.Description),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.CreatePositionResponse{Position: positionToProto(position)}, nil
}

func (s *Server) UpdatePosition(ctx context.Context, request *companyv1.UpdatePositionRequest) (*companyv1.UpdatePositionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "должности")
	if err != nil {
		return nil, err
	}
	departmentID, err := parseOptionalUUID(request.DepartmentId, "отдела")
	if err != nil {
		return nil, err
	}
	level, err := optionalLevel(request.Level)
	if err != nil {
		return nil, err
	}
	position, err := s.application.UpdatePosition(ctx, actor, application.UpdatePositionInput{
		ID:             id,
		Name:           cloneString(request.Name),
		DepartmentID:   departmentID,
		Level:          level,
		SetDescription: request.Description != nil,
		Description:    cloneString(request.Description),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.UpdatePositionResponse{Position: positionToProto(position)}, nil
}

func (s *Server) DeletePosition(ctx context.Context, request *companyv1.DeletePositionRequest) (*companyv1.DeletePositionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "должности")
	if err != nil {
		return nil, err
	}
	if err = s.application.DeletePosition(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &companyv1.DeletePositionResponse{}, nil
}

func (s *Server) MovePosition(ctx context.Context, request *companyv1.MovePositionRequest) (*companyv1.MovePositionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "должности")
	if err != nil {
		return nil, err
	}
	departmentID, err := parseUUID(request.DepartmentId, "отдела")
	if err != nil {
		return nil, err
	}
	position, err := s.application.MovePosition(ctx, actor, id, departmentID)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.MovePositionResponse{Position: positionToProto(position)}, nil
}

func (s *Server) GetUsers(ctx context.Context, request *companyv1.GetUsersRequest) (*companyv1.GetUsersResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	users, err := s.application.ListUsers(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetUsersResponse{Users: usersToProto(users)}, nil
}

func (s *Server) GetUser(ctx context.Context, request *companyv1.GetUserRequest) (*companyv1.GetUserResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "сотрудника")
	if err != nil {
		return nil, err
	}
	user, err := s.application.GetUser(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetUserResponse{User: userToProto(user)}, nil
}

func (s *Server) CreateUser(ctx context.Context, request *companyv1.CreateUserRequest) (*companyv1.CreateUserResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	role, err := userRoleFromProto(request.Role)
	if err != nil {
		return nil, err
	}
	positionIDs, err := parseUUIDs(request.PositionIds, "должностей")
	if err != nil {
		return nil, err
	}
	user, err := s.application.CreateUser(ctx, actor, application.CreateUserInput{
		FirstName:   request.FirstName,
		LastName:    request.LastName,
		Email:       request.Email,
		Phone:       cloneString(request.Phone),
		Role:        role,
		PositionIDs: positionIDs,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.CreateUserResponse{User: userToProto(user)}, nil
}

func (s *Server) UpdateUser(ctx context.Context, request *companyv1.UpdateUserRequest) (*companyv1.UpdateUserResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "сотрудника")
	if err != nil {
		return nil, err
	}
	phoneSet, phone := clearableString(request.Phone)
	birthDateSet, birthDate := clearableString(request.BirthDate)
	hiredAtSet, hiredAt := clearableString(request.HiredAt)
	vacationAllowance, err := optionalVacationAllowance(request.VacationAllowance)
	if err != nil {
		return nil, err
	}
	var role *string
	if request.Role != nil {
		value, mapErr := userRoleFromProto(*request.Role)
		if mapErr != nil {
			return nil, mapErr
		}
		role = &value
	}
	var userStatus *string
	if request.Status != nil {
		value, mapErr := userStatusFromProto(*request.Status)
		if mapErr != nil {
			return nil, mapErr
		}
		userStatus = &value
	}
	var positionIDs []uuid.UUID
	if request.UpdatePositionIds {
		positionIDs, err = parseUUIDs(request.PositionIds, "должностей")
		if err != nil {
			return nil, err
		}
	}
	user, err := s.application.UpdateUser(ctx, actor, application.UpdateUserInput{
		ID:                   id,
		FirstName:            cloneString(request.FirstName),
		LastName:             cloneString(request.LastName),
		SetPhone:             phoneSet,
		Phone:                phone,
		SetBirthDate:         birthDateSet,
		BirthDate:            birthDate,
		SetHiredAt:           hiredAtSet,
		HiredAt:              hiredAt,
		SetVacationAllowance: request.VacationAllowance != nil,
		VacationAllowance:    vacationAllowance,
		Role:                 role,
		Status:               userStatus,
		SetPositionIDs:       request.UpdatePositionIds,
		PositionIDs:          positionIDs,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.UpdateUserResponse{User: userToProto(user)}, nil
}

func (s *Server) GetInvites(ctx context.Context, request *companyv1.GetInvitesRequest) (*companyv1.GetInvitesResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	invites, err := s.application.ListInvites(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetInvitesResponse{Invites: invitesToProto(invites)}, nil
}

func (s *Server) InviteUser(ctx context.Context, request *companyv1.InviteUserRequest) (*companyv1.InviteUserResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	role, err := userRoleFromProto(request.Role)
	if err != nil {
		return nil, err
	}
	positionID, err := parseOptionalUUID(request.PositionId, "должности")
	if err != nil {
		return nil, err
	}
	departmentID, err := parseOptionalUUID(request.DepartmentId, "отдела")
	if err != nil {
		return nil, err
	}
	invite, err := s.application.InviteUser(ctx, actor, application.InviteUserInput{
		Email:        cloneString(request.Email),
		Role:         role,
		PositionID:   positionID,
		DepartmentID: departmentID,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.InviteUserResponse{Invite: inviteToProto(invite)}, nil
}

func (s *Server) ResendInvite(ctx context.Context, request *companyv1.ResendInviteRequest) (*companyv1.ResendInviteResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "приглашения")
	if err != nil {
		return nil, err
	}
	invite, err := s.application.ResendInvite(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ResendInviteResponse{Invite: inviteToProto(invite)}, nil
}

func (s *Server) RevokeInvite(ctx context.Context, request *companyv1.RevokeInviteRequest) (*companyv1.RevokeInviteResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "приглашения")
	if err != nil {
		return nil, err
	}
	if err = s.application.RevokeInvite(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &companyv1.RevokeInviteResponse{}, nil
}

func (s *Server) GetUsersByIds(ctx context.Context, request *companyv1.GetUsersByIdsRequest) (*companyv1.GetUsersByIdsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	userIDs, err := parseUUIDs(request.UserIds, "пользователей")
	if err != nil {
		return nil, err
	}
	users, err := s.application.GetUsersByIDs(ctx, actor, userIDs)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetUsersByIdsResponse{Users: usersToProto(users)}, nil
}

func (s *Server) ResolvePositionUsers(ctx context.Context, request *companyv1.ResolvePositionUsersRequest) (*companyv1.ResolvePositionUsersResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	positionID, err := parseUUID(request.PositionId, "должности")
	if err != nil {
		return nil, err
	}
	userIDs, err := s.application.ResolvePositionUsers(ctx, actor, positionID)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ResolvePositionUsersResponse{UserIds: uuidStrings(userIDs)}, nil
}

func (s *Server) ResolveDepartmentUsers(ctx context.Context, request *companyv1.ResolveDepartmentUsersRequest) (*companyv1.ResolveDepartmentUsersResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	departmentID, err := parseUUID(request.DepartmentId, "отдела")
	if err != nil {
		return nil, err
	}
	userIDs, err := s.application.ResolveDepartmentUsers(ctx, actor, departmentID, request.IncludeDescendants)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ResolveDepartmentUsersResponse{UserIds: uuidStrings(userIDs)}, nil
}
