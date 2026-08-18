package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
)

func (s *Server) CheckAmoSessionAccess(
	ctx context.Context,
	request *companyv1.CheckAmoSessionAccessRequest,
) (*companyv1.CheckAmoSessionAccessResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.CheckAmoSessionAccess(ctx, actor, request.GetAmoAccountId())
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.CheckAmoSessionAccessResponse{
		Allowed: result.Allowed, Role: userRoleToProto(result.Role), RedirectUrl: result.RedirectURL,
	}, nil
}
