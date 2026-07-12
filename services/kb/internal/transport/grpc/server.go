package grpc

import (
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/kb/internal/application"
)

type Server struct {
	kbv1.UnimplementedKbServiceServer

	application *application.Service
	verifier    tokenVerifier
}

func NewServer(applicationService *application.Service, verifier *sharedauth.TokenVerifier) *Server {
	return &Server{application: applicationService, verifier: verifier}
}

var _ kbv1.KbServiceServer = (*Server)(nil)