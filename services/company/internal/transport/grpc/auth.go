package grpc

import (
	"context"
	"net"
	"strings"

	"github.com/google/uuid"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
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

	return application.Actor{UserID: userID, CompanyID: companyID, Role: claims.Role}, nil
}

func sessionMeta(ctx context.Context) application.SessionMeta {
	md, _ := metadata.FromIncomingContext(ctx)
	userAgent := firstMetadataValue(md, "x-user-agent")
	if userAgent == "" {
		userAgent = firstMetadataValue(md, "user-agent")
	}
	address := firstForwardedAddress(firstMetadataValue(md, "x-forwarded-for"))
	if address == "" {
		address = firstMetadataValue(md, "x-real-ip")
	}
	if address == "" {
		address = firstMetadataValue(md, "x-client-ip")
	}
	if address == "" {
		if remotePeer, ok := peer.FromContext(ctx); ok && remotePeer.Addr != nil {
			address = hostOnly(remotePeer.Addr.String())
		}
	}
	return application.SessionMeta{UserAgent: userAgent, IPAddress: address}
}

func firstMetadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func firstForwardedAddress(value string) string {
	address, _, _ := strings.Cut(value, ",")
	return hostOnly(strings.TrimSpace(address))
}

func hostOnly(value string) string {
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return strings.Trim(value, "[]")
}
