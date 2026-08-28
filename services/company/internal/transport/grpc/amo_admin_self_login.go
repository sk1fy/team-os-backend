package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (s *Server) AmoAdminSelfLogin(
	ctx context.Context,
	request *companyv1.AmoAdminSelfLoginRequest,
) (*companyv1.AmoAdminSelfLoginResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	users := make([]application.AmoAdminUserAssertion, len(request.GetUsers()))
	for index, user := range request.GetUsers() {
		if user == nil {
			return nil, invalidArgument("Некорректный снимок пользователей amoCRM")
		}
		users[index] = application.AmoAdminUserAssertion{
			ID: user.GetId(), IsAdmin: user.GetIsAdmin(), IsActive: user.GetIsActive(),
		}
	}
	md, _ := metadata.FromIncomingContext(ctx)
	result, err := s.application.AmoAdminSelfLogin(ctx, application.AmoAdminSelfLoginInput{
		AmoAccountID: request.GetAmoAccountId(), SelfUserID: request.GetSelfUserId(), Users: users,
		RequestID: firstMetadataValue(md, "x-request-id"),
	})
	if err != nil {
		return nil, transportError(err)
	}
	if !result.Allowed || result.Action != "login" || result.AccessToken == "" ||
		(result.Role != "admin" && result.Role != "owner") {
		return nil, status.Error(codes.Internal, "Внутренняя ошибка сервиса")
	}
	return &companyv1.AmoAdminSelfLoginResponse{
		Allowed: result.Allowed, Action: companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_LOGIN,
		Role: userRoleToProto(result.Role), AccessToken: result.AccessToken,
	}, nil
}
