package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
)

func (s *Server) GetCurrentUser(ctx context.Context, request *companyv1.GetCurrentUserRequest) (*companyv1.GetCurrentUserResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	user, err := s.application.GetCurrentUser(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetCurrentUserResponse{User: userToProto(user)}, nil
}

func (s *Server) UpdateCurrentUser(ctx context.Context, request *companyv1.UpdateCurrentUserRequest) (*companyv1.UpdateCurrentUserResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	user, err := s.application.UpdateCurrentUser(ctx, actor, updateCurrentUserInput(request))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.UpdateCurrentUserResponse{User: userToProto(user)}, nil
}

func (s *Server) GetCompany(ctx context.Context, request *companyv1.GetCompanyRequest) (*companyv1.GetCompanyResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	company, err := s.application.GetCompany(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetCompanyResponse{Company: companyToProto(company)}, nil
}

func (s *Server) UpdateCompany(ctx context.Context, request *companyv1.UpdateCompanyRequest) (*companyv1.UpdateCompanyResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	company, err := s.application.UpdateCompany(ctx, actor, updateCompanyInput(request))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.UpdateCompanyResponse{Company: companyToProto(company)}, nil
}
