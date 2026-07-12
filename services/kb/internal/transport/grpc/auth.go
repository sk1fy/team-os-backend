package grpc

import (
	"context"
	"strings"

	"github.com/google/uuid"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/kb/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type tokenVerifier interface {
	Verify(raw string) (*sharedauth.Claims, error)
}

func (s *Server) actor(ctx context.Context) (application.Actor, error) {
	if s.verifier == nil {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Требуется авторизация")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Требуется авторизация")
	}
	values := md.Get("authorization")
	if len(values) != 1 {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Требуется авторизация")
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Некорректный заголовок авторизации")
	}
	claims, err := s.verifier.Verify(parts[1])
	if err != nil || claims == nil {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
	}
	companyID, err := uuid.Parse(claims.CompanyID)
	if err != nil || strings.TrimSpace(claims.Role) == "" {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
	}
	positionIDs, err := parseUUIDClaims(claims.PositionIDs)
	if err != nil {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
	}
	departmentIDs, err := parseUUIDClaims(claims.DepartmentIDs)
	if err != nil {
		return application.Actor{}, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
	}
	return application.Actor{
		UserID: userID, CompanyID: companyID, Role: claims.Role,
		PositionIDs: positionIDs, DepartmentIDs: departmentIDs,
	}, nil
}

func parseUUIDClaims(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}