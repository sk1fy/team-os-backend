package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (s *Server) BootstrapAmoCompany(
	ctx context.Context,
	request *companyv1.BootstrapAmoCompanyRequest,
) (*companyv1.BootstrapAmoCompanyResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	users := make([]application.AmoCompanyBootstrapUser, len(request.GetUsers()))
	for index, user := range request.GetUsers() {
		if user == nil {
			return nil, invalidArgument("Некорректный список пользователей amoCRM")
		}
		users[index] = application.AmoCompanyBootstrapUser{
			ID: user.GetId(), Email: user.GetEmail(), Name: user.GetName(),
			IsAdmin: user.GetIsAdmin(), IsActive: user.GetIsActive(),
		}
	}
	md, _ := metadata.FromIncomingContext(ctx)
	result, err := s.application.BootstrapAmoCompany(ctx, application.AmoCompanyBootstrapInput{
		AmoAccountID: request.GetAmoAccountId(), CompanyName: request.GetCompanyName(),
		Subdomain: request.GetSubdomain(), SelfUserID: request.GetSelfUserId(), Users: users,
		RequestID: firstMetadataValue(md, "x-request-id"),
	})
	if err != nil {
		return nil, transportError(err)
	}
	if result.Action != "register" || result.Role != "owner" || result.AccessToken == "" {
		return nil, status.Error(codes.Internal, "Внутренняя ошибка сервиса")
	}
	return &companyv1.BootstrapAmoCompanyResponse{
		Action:    companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER,
		CompanyId: result.CompanyID.String(), UserId: result.UserID.String(),
		Role: userRoleToProto(result.Role), AccessToken: result.AccessToken,
	}, nil
}
