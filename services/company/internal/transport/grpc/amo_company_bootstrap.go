package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

func (s *Server) BootstrapAmoCompany(ctx context.Context, _ *companyv1.BootstrapAmoCompanyRequest) (*companyv1.BootstrapAmoCompanyResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	_, err := s.application.BootstrapAmoCompany(ctx, application.AmoCompanyBootstrapInput{})
	return nil, transportError(err)
}
