package grpc

import (
	tasksv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/tasks/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/application"
)

type Server struct {
	tasksv1.UnimplementedTasksServiceServer

	application *application.Service
	verifier    tokenVerifier
}

func NewServer(applicationService *application.Service, verifier *sharedauth.TokenVerifier) *Server {
	return &Server{application: applicationService, verifier: verifier}
}

var _ tasksv1.TasksServiceServer = (*Server)(nil)