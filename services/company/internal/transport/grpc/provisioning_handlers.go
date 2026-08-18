package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) CheckAmoAccount(
	ctx context.Context,
	request *companyv1.CheckAmoAccountRequest,
) (*companyv1.CheckAmoAccountResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	exists, err := s.application.CheckAmoAccount(ctx, request.Provider, request.ExternalAccountId)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.CheckAmoAccountResponse{Exists: exists}, nil
}

func (s *Server) IssueCompanyRegistrationToken(
	ctx context.Context,
	request *companyv1.IssueCompanyRegistrationTokenRequest,
) (*companyv1.IssueCompanyRegistrationTokenResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.IssueCompanyRegistrationToken(ctx, request.Provider, request.ExternalAccountId)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.IssueCompanyRegistrationTokenResponse{
		Token: result.Token, ExpiresAt: timestamppb.New(result.ExpiresAt.UTC()),
	}, nil
}

func (s *Server) ExchangeAmoWidgetSession(
	ctx context.Context,
	request *companyv1.ExchangeAmoWidgetSessionRequest,
) (*companyv1.ExchangeAmoWidgetSessionResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.ExchangeAmoWidgetSession(ctx, application.AmoWidgetSessionInput{
		Token: request.GetToken(), ExternalAccountID: request.GetExternalAccountId(),
		ExternalUserID: request.GetExternalUserId(), Email: request.GetEmail(),
		UserName: request.GetUserName(), CompanyName: request.GetCompanyName(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &companyv1.ExchangeAmoWidgetSessionResponse{ExternalAccountId: &result.ExternalAccountID}
	switch result.Action {
	case "login":
		response.Action = companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_LOGIN
	case "register":
		if result.RegistrationToken == "" && result.SessionToken == "" {
			return nil, status.Error(codes.Internal, "Внутренняя ошибка сервиса")
		}
		response.Action = companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER
	default:
		return nil, status.Error(codes.Internal, "Внутренняя ошибка сервиса")
	}
	if result.RegistrationToken != "" {
		response.RegistrationToken = &result.RegistrationToken
	}
	if result.SessionToken != "" {
		response.SessionToken = &result.SessionToken
		response.Email = &result.Email
		response.CompanyName = &result.CompanyName
		response.RequiresPasswordSetup = &result.RequiresPasswordSetup
	}
	if result.ExpiresAt != nil {
		response.ExpiresAt = timestamppb.New(*result.ExpiresAt)
	}
	return response, nil
}

func (s *Server) ProvisionAmoAdminSession(
	ctx context.Context,
	request *companyv1.ProvisionAmoAdminSessionRequest,
) (*companyv1.ProvisionAmoAdminSessionResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	desiredRole, err := userRoleFromProto(request.GetDesiredRole())
	if err != nil || (desiredRole != "admin" && desiredRole != "owner") {
		return nil, invalidArgument("Роль пользователя amoCRM должна быть admin или owner")
	}
	result, err := s.application.ProvisionAmoAdminSession(ctx, application.AmoAdminSessionInput{
		Provider: request.GetProvider(), ExternalAccountID: request.GetExternalAccountId(),
		ExternalUserID: request.GetExternalUserId(), Email: request.GetEmail(),
		UserName: request.GetUserName(), CompanyName: request.GetCompanyName(), DesiredRole: desiredRole,
	})
	if err != nil {
		return nil, transportError(err)
	}
	action := companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_LOGIN
	if result.Action == "register" {
		action = companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER
	} else if result.Action != "login" {
		return nil, status.Error(codes.Internal, "Внутренняя ошибка сервиса")
	}
	return &companyv1.ProvisionAmoAdminSessionResponse{
		Action: action, ExternalAccountId: result.ExternalAccountID,
		CompanyId: result.CompanyID.String(), UserId: result.UserID.String(),
		Role: userRoleToProto(result.Role), AccessToken: result.AccessToken,
	}, nil
}

func (s *Server) ValidateAmoWidgetContinuation(
	ctx context.Context,
	request *companyv1.ValidateAmoWidgetContinuationRequest,
) (*companyv1.ValidateAmoWidgetContinuationResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.ValidateAmoWidgetContinuation(ctx, request.GetSessionToken())
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ValidateAmoWidgetContinuationResponse{
		Email: result.Email, Login: result.Login, CompanyName: result.CompanyName,
		RequiresPasswordSetup: result.RequiresPasswordSetup,
		ExpiresAt:             timestamppb.New(result.ExpiresAt),
	}, nil
}

func (s *Server) CompleteAmoWidgetContinuation(
	ctx context.Context,
	request *companyv1.CompleteAmoWidgetContinuationRequest,
) (*companyv1.CompleteAmoWidgetContinuationResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.CompleteAmoWidgetContinuation(
		ctx, request.GetSessionToken(), request.GetPassword(), sessionMeta(ctx),
	)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.CompleteAmoWidgetContinuationResponse{Session: authSessionToProto(result)}, nil
}

func (s *Server) ValidateCompanyRegistrationToken(
	ctx context.Context,
	request *companyv1.ValidateCompanyRegistrationTokenRequest,
) (*companyv1.ValidateCompanyRegistrationTokenResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.ValidateCompanyRegistrationToken(ctx, request.Token)
	if err != nil {
		return nil, transportError(err)
	}
	response := &companyv1.ValidateCompanyRegistrationTokenResponse{
		Valid: result.Valid, State: companyRegistrationTokenStateToProto(result.State),
	}
	if result.ExternalAccountID != nil {
		response.ExternalAccountId = result.ExternalAccountID
	}
	if result.ExpiresAt != nil {
		response.ExpiresAt = timestamppb.New(result.ExpiresAt.UTC())
	}
	return response, nil
}

func companyRegistrationTokenStateToProto(value string) companyv1.CompanyRegistrationTokenState {
	switch value {
	case "valid":
		return companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_VALID
	case "expired":
		return companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_EXPIRED
	case "consumed":
		return companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_CONSUMED
	case "revoked":
		return companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_REVOKED
	default:
		return companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_INVALID
	}
}

func (s *Server) ProvisionCompany(
	ctx context.Context,
	request *companyv1.ProvisionCompanyRequest,
) (*companyv1.ProvisionCompanyResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	return nil, retiredRegistrationRPC()
}

func (s *Server) GetProvisionedCompanyStatus(
	ctx context.Context,
	request *companyv1.GetProvisionedCompanyStatusRequest,
) (*companyv1.GetProvisionedCompanyStatusResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	return nil, retiredRegistrationRPC()
}

func (s *Server) GetBootstrapActivation(
	ctx context.Context,
	request *companyv1.GetBootstrapActivationRequest,
) (*companyv1.GetBootstrapActivationResponse, error) {
	return nil, retiredRegistrationRPC()
}

func (s *Server) CompleteBootstrapActivation(
	ctx context.Context,
	request *companyv1.CompleteBootstrapActivationRequest,
) (*companyv1.CompleteBootstrapActivationResponse, error) {
	return nil, retiredRegistrationRPC()
}

func (s *Server) IssueSsoToken(
	ctx context.Context,
	request *companyv1.IssueSsoTokenRequest,
) (*companyv1.IssueSsoTokenResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	return nil, retiredRegistrationRPC()
}

func (s *Server) ExchangeSsoToken(
	ctx context.Context,
	request *companyv1.ExchangeSsoTokenRequest,
) (*companyv1.ExchangeSsoTokenResponse, error) {
	return nil, retiredRegistrationRPC()
}

func (s *Server) GetOnboardingStatus(
	ctx context.Context,
	request *companyv1.GetOnboardingStatusRequest,
) (*companyv1.GetOnboardingStatusResponse, error) {
	return nil, retiredRegistrationRPC()
}

func (s *Server) ReissueOnboardingActivation(
	ctx context.Context,
	request *companyv1.ReissueOnboardingActivationRequest,
) (*companyv1.ReissueOnboardingActivationResponse, error) {
	return nil, retiredRegistrationRPC()
}

func companyStatusToProto(value string) companyv1.CompanyStatus {
	switch value {
	case "onboarding":
		return companyv1.CompanyStatus_COMPANY_STATUS_ONBOARDING
	case "active":
		return companyv1.CompanyStatus_COMPANY_STATUS_ACTIVE
	case "frozen":
		return companyv1.CompanyStatus_COMPANY_STATUS_FROZEN
	case "suspended":
		return companyv1.CompanyStatus_COMPANY_STATUS_SUSPENDED
	default:
		return companyv1.CompanyStatus_COMPANY_STATUS_UNSPECIFIED
	}
}

func retiredRegistrationRPC() error {
	return status.Error(codes.Unimplemented, "Прежний сценарий регистрации отключён")
}
