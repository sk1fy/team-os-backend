// Package grpc exposes the company application service over the internal
// protobuf contract.
package grpc

import (
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

// Server is the gRPC adapter for application.Service.
type Server struct {
	companyv1.UnimplementedCompanyServiceServer

	application *application.Service
	verifier    tokenVerifier
}

// NewServer constructs a company gRPC server. The verifier is mandatory for
// every RPC except the public authentication and invitation methods.
func NewServer(applicationService *application.Service, verifier *sharedauth.TokenVerifier) *Server {
	return &Server{application: applicationService, verifier: verifier}
}

var _ companyv1.CompanyServiceServer = (*Server)(nil)
