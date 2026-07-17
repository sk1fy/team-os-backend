package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) GetUserAccess(ctx context.Context, request *companyv1.GetUserAccessRequest) (*companyv1.GetUserAccessResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	userID, err := parseUUID(request.Id, "сотрудника")
	if err != nil {
		return nil, err
	}
	access, err := s.application.GetUserAccess(ctx, actor, userID)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetUserAccessResponse{Access: employeeAccessToProto(access)}, nil
}

func (s *Server) SetUserPasswordAccess(ctx context.Context, request *companyv1.SetUserPasswordAccessRequest) (*companyv1.SetUserPasswordAccessResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	userID, err := parseUUID(request.Id, "сотрудника")
	if err != nil {
		return nil, err
	}
	password, err := s.application.SetPasswordAccess(ctx, actor, userID, application.SetPasswordAccessInput{
		Password: request.Password,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.SetUserPasswordAccessResponse{Password: password}, nil
}

func (s *Server) SetUserLinkAccess(ctx context.Context, request *companyv1.SetUserLinkAccessRequest) (*companyv1.SetUserLinkAccessResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	userID, err := parseUUID(request.Id, "сотрудника")
	if err != nil {
		return nil, err
	}
	access, err := s.application.SetLinkAccess(ctx, actor, userID)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.SetUserLinkAccessResponse{
		Token: access.Token, CreatedAt: timestamppb.New(access.CreatedAt.UTC()),
	}, nil
}

func (s *Server) RevokeUserAccess(ctx context.Context, request *companyv1.RevokeUserAccessRequest) (*companyv1.RevokeUserAccessResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	userID, err := parseUUID(request.Id, "сотрудника")
	if err != nil {
		return nil, err
	}
	if err = s.application.RevokeAccess(ctx, actor, userID); err != nil {
		return nil, transportError(err)
	}
	return &companyv1.RevokeUserAccessResponse{}, nil
}
