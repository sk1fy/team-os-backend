package grpc

import (
	"context"
	"strings"

	"github.com/google/uuid"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type actorResolver struct {
	verifier        *sharedauth.TokenVerifier
	trustedMetadata bool
}

func (a actorResolver) actor(ctx context.Context) (uuid.UUID, uuid.UUID, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return uuid.Nil, uuid.Nil, unauthenticated()
	}
	if values := md.Get("authorization"); len(values) == 1 && a.verifier != nil {
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Некорректный заголовок авторизации")
		}
		claims, err := a.verifier.Verify(parts[1])
		if err != nil {
			return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
		}
		userID, uerr := uuid.Parse(claims.Subject)
		companyID, cerr := uuid.Parse(claims.CompanyID)
		if uerr != nil || cerr != nil {
			return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
		}
		return userID, companyID, nil
	}
	if a.trustedMetadata {
		users, companies := md.Get("x-user-id"), md.Get("x-company-id")
		if len(users) == 1 && len(companies) == 1 {
			userID, uerr := uuid.Parse(users[0])
			companyID, cerr := uuid.Parse(companies[0])
			if uerr == nil && cerr == nil {
				return userID, companyID, nil
			}
		}
	}
	return uuid.Nil, uuid.Nil, unauthenticated()
}
func unauthenticated() error {
	return status.Error(codes.Unauthenticated, "Требуется авторизация")
}
