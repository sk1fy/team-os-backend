package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

func (s *Server) AmoAdminSelfLogin(ctx context.Context, _ *companyv1.AmoAdminSelfLoginRequest) (*companyv1.AmoAdminSelfLoginResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	_, err := s.application.AmoAdminSelfLogin(ctx, application.AmoAdminSelfLoginInput{})
	return nil, transportError(err)
}
