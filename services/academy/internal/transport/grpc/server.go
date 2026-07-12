package grpc

import (
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

type Server struct {
	academyv1.UnimplementedAcademyServiceServer

	application *application.Service
	verifier    tokenVerifier
}

func NewServer(applicationService *application.Service, verifier *sharedauth.TokenVerifier) *Server {
	return &Server{application: applicationService, verifier: verifier}
}

var _ academyv1.AcademyServiceServer = (*Server)(nil)
