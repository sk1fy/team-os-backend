package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

func (s *Server) Register(ctx context.Context, request *companyv1.RegisterRequest) (*companyv1.RegisterResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.Register(ctx, application.RegisterInput{
		CompanyName: request.CompanyName,
		Email:       request.Email,
		Password:    request.Password,
		FirstName:   request.FirstName,
		LastName:    request.LastName,
	}, sessionMeta(ctx))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.RegisterResponse{Session: authSessionToProto(result)}, nil
}

func (s *Server) Login(ctx context.Context, request *companyv1.LoginRequest) (*companyv1.LoginResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.Login(ctx, application.LoginInput{
		Email: request.Email, Password: request.Password,
	}, sessionMeta(ctx))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.LoginResponse{Session: authSessionToProto(result)}, nil
}

func (s *Server) LoginWithAccessLink(ctx context.Context, request *companyv1.LoginWithAccessLinkRequest) (*companyv1.LoginWithAccessLinkResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.LoginWithAccessLink(ctx, request.Token, sessionMeta(ctx))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.LoginWithAccessLinkResponse{Session: authSessionToProto(result)}, nil
}

func (s *Server) Refresh(ctx context.Context, request *companyv1.RefreshRequest) (*companyv1.RefreshResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.Refresh(ctx, request.RefreshToken, sessionMeta(ctx))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.RefreshResponse{Session: authSessionToProto(result)}, nil
}

func (s *Server) Logout(ctx context.Context, request *companyv1.LogoutRequest) (*companyv1.LogoutResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	if err := s.application.Logout(ctx, request.RefreshToken); err != nil {
		return nil, transportError(err)
	}
	return &companyv1.LogoutResponse{}, nil
}

func (s *Server) GetInviteByToken(ctx context.Context, request *companyv1.GetInviteByTokenRequest) (*companyv1.GetInviteByTokenResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	invite, err := s.application.GetInviteByToken(ctx, request.Token)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetInviteByTokenResponse{Invite: inviteToProto(invite)}, nil
}

func (s *Server) AcceptInvite(ctx context.Context, request *companyv1.AcceptInviteRequest) (*companyv1.AcceptInviteResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	email := ""
	if request.Email != nil {
		email = *request.Email
	}
	result, err := s.application.AcceptInvite(ctx, application.AcceptInviteInput{
		Token:     request.Token,
		Email:     email,
		FirstName: request.FirstName,
		LastName:  request.LastName,
		Password:  request.Password,
	}, sessionMeta(ctx))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.AcceptInviteResponse{Session: authSessionToProto(result)}, nil
}
