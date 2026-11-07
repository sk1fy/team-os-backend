package grpc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"

	"github.com/google/uuid"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type actorResolver struct {
	verifier            *sharedauth.TokenVerifier
	academyServiceToken string
}

type actor struct {
	userID, companyID uuid.UUID
	role              string
}

func (a actorResolver) actor(ctx context.Context) (actor, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return actor{}, unauthenticated()
	}
	if values := md.Get("authorization"); len(values) == 1 && a.verifier != nil {
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return actor{}, status.Error(codes.Unauthenticated, "Некорректный заголовок авторизации")
		}
		claims, err := a.verifier.Verify(parts[1])
		if err != nil {
			return actor{}, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
		}
		userID, uerr := uuid.Parse(claims.Subject)
		companyID, cerr := uuid.Parse(claims.CompanyID)
		if uerr != nil || cerr != nil {
			return actor{}, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
		}
		return actor{userID: userID, companyID: companyID, role: claims.Role}, nil
	}
	return actor{}, unauthenticated()
}

func (a actorResolver) cloneActor(ctx context.Context) (actor, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return actor{}, unauthenticated()
	}
	if len(md.Get("authorization")) != 0 {
		return a.actor(ctx)
	}
	credentials := md.Get("x-academy-service-token")
	if a.academyServiceToken != "" && len(credentials) == 1 {
		provided, expected := sha256.Sum256([]byte(credentials[0])), sha256.Sum256([]byte(a.academyServiceToken))
		if subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 {
			return actor{}, unauthenticated()
		}
		users, companies := md.Get("x-user-id"), md.Get("x-company-id")
		if len(users) == 1 && len(companies) == 1 {
			userID, uerr := uuid.Parse(users[0])
			companyID, cerr := uuid.Parse(companies[0])
			if uerr == nil && cerr == nil && userID != uuid.Nil && companyID != uuid.Nil {
				role := ""
				if roles := md.Get("x-user-role"); len(roles) == 1 {
					role = roles[0]
				}
				if role == "admin" || role == "partner" {
					return actor{userID: userID, companyID: companyID, role: role}, nil
				}
			}
		}
	}
	return actor{}, unauthenticated()
}
func unauthenticated() error {
	return status.Error(codes.Unauthenticated, "Требуется авторизация")
}
