package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ProvisionCompany(
	ctx context.Context,
	request *companyv1.ProvisionCompanyRequest,
) (*companyv1.ProvisionCompanyResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil || request.Owner == nil || request.Admin == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.ProvisionCompany(ctx, application.ProvisionCompanyInput{
		Provider: request.Provider, ExternalAccountID: request.ExternalAccountId,
		CompanyName: request.CompanyName, InitiatorExternalUserID: request.InitiatorExternalUserId,
		Owner: application.ProvisioningParticipantInput{
			ExternalUserID: request.Owner.ExternalUserId, Email: request.Owner.Email,
			FirstName: request.Owner.FirstName, LastName: request.Owner.LastName,
		},
		Admin: application.ProvisioningParticipantInput{
			ExternalUserID: request.Admin.ExternalUserId, Email: request.Admin.Email,
			FirstName: request.Admin.FirstName, LastName: request.Admin.LastName,
		},
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ProvisionCompanyResponse{
		CompanyId: result.CompanyID.String(), CompanyStatus: companyStatusToProto(result.CompanyStatus),
		Created: result.Created, InitiatorRole: userRoleToProto(result.InitiatorRole),
		BootstrapToken:     result.BootstrapToken,
		BootstrapExpiresAt: timestamppb.New(result.BootstrapExpiresAt.UTC()),
	}, nil
}

func (s *Server) GetProvisionedCompanyStatus(
	ctx context.Context,
	request *companyv1.GetProvisionedCompanyStatusRequest,
) (*companyv1.GetProvisionedCompanyStatusResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.GetProvisionedCompanyStatus(ctx, request.Provider, request.ExternalAccountId)
	if err != nil {
		return nil, transportError(err)
	}
	response := &companyv1.GetProvisionedCompanyStatusResponse{Exists: result.Exists}
	if result.CompanyID != nil {
		companyID := result.CompanyID.String()
		response.CompanyId = &companyID
	}
	if result.CompanyStatus != nil {
		response.CompanyStatus = companyStatusToProto(*result.CompanyStatus)
	}
	return response, nil
}

func (s *Server) GetBootstrapActivation(
	ctx context.Context,
	request *companyv1.GetBootstrapActivationRequest,
) (*companyv1.GetBootstrapActivationResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.GetBootstrapActivation(ctx, request.Token)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetBootstrapActivationResponse{
		Activation: bootstrapActivationToProto(result),
	}, nil
}

func (s *Server) CompleteBootstrapActivation(
	ctx context.Context,
	request *companyv1.CompleteBootstrapActivationRequest,
) (*companyv1.CompleteBootstrapActivationResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.CompleteBootstrapActivation(ctx, application.CompleteBootstrapInput{
		Token: request.Token, Password: request.Password,
	}, sessionMeta(ctx))
	if err != nil {
		return nil, transportError(err)
	}
	response := &companyv1.CompleteBootstrapActivationResponse{
		Session: authSessionToProto(result.Session), Onboarding: onboardingStateToProto(result.Onboarding),
	}
	return response, nil
}

func (s *Server) IssueSsoToken(
	ctx context.Context,
	request *companyv1.IssueSsoTokenRequest,
) (*companyv1.IssueSsoTokenResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.IssueSsoToken(ctx, application.IssueSsoTokenInput{
		Provider: request.Provider, ExternalAccountID: request.ExternalAccountId,
		ExternalUserID: request.ExternalUserId,
	})
	if err != nil {
		return nil, transportError(err)
	}
	continuation := companyv1.ProvisioningContinuationKind_PROVISIONING_CONTINUATION_KIND_SSO
	if result.Kind == "onboarding" {
		continuation = companyv1.ProvisioningContinuationKind_PROVISIONING_CONTINUATION_KIND_ONBOARDING
	}
	return &companyv1.IssueSsoTokenResponse{
		Token: result.Token, ExpiresAt: timestamppb.New(result.ExpiresAt.UTC()),
		ContinuationKind: continuation,
	}, nil
}

func (s *Server) ExchangeSsoToken(
	ctx context.Context,
	request *companyv1.ExchangeSsoTokenRequest,
) (*companyv1.ExchangeSsoTokenResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.ExchangeSsoToken(ctx, request.Token, sessionMeta(ctx))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ExchangeSsoTokenResponse{Session: authSessionToProto(result)}, nil
}

func (s *Server) GetOnboardingStatus(
	ctx context.Context,
	request *companyv1.GetOnboardingStatusRequest,
) (*companyv1.GetOnboardingStatusResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.GetOnboardingStatus(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetOnboardingStatusResponse{Onboarding: onboardingStateToProto(result)}, nil
}

func (s *Server) ReissueOnboardingActivation(
	ctx context.Context,
	request *companyv1.ReissueOnboardingActivationRequest,
) (*companyv1.ReissueOnboardingActivationResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.ReissueOnboardingActivation(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ReissueOnboardingActivationResponse{Onboarding: onboardingStateToProto(result)}, nil
}

func bootstrapActivationToProto(value application.BootstrapActivation) *companyv1.BootstrapActivation {
	return &companyv1.BootstrapActivation{
		CompanyId: value.CompanyID.String(), CompanyName: value.CompanyName,
		CompanyStatus: companyStatusToProto(value.CompanyStatus),
		User:          bootstrapParticipantToProto(value.Participant),
		ExpiresAt:     timestamppb.New(value.ExpiresAt.UTC()), State: bootstrapStateToProto(value.State),
	}
}

func bootstrapParticipantToProto(value application.BootstrapParticipant) *companyv1.BootstrapParticipant {
	return &companyv1.BootstrapParticipant{
		UserId: value.UserID.String(), Email: value.Email, FirstName: value.FirstName,
		LastName: value.LastName, Role: userRoleToProto(value.Role), Status: userStatusToProto(value.Status),
	}
}

func onboardingStateToProto(value application.OnboardingState) *companyv1.OnboardingState {
	result := &companyv1.OnboardingState{
		CompanyId: value.CompanyID.String(), CompanyStatus: companyStatusToProto(value.CompanyStatus),
		Completed: value.Completed, ActivationToken: value.ActivationToken,
	}
	if value.PendingUser != nil {
		result.PendingUser = bootstrapParticipantToProto(*value.PendingUser)
	}
	if value.ExpiresAt != nil {
		result.ExpiresAt = timestamppb.New(value.ExpiresAt.UTC())
	}
	return result
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

func bootstrapStateToProto(value string) companyv1.BootstrapActivationState {
	switch value {
	case "pending":
		return companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_PENDING
	case "expired":
		return companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_EXPIRED
	case "consumed":
		return companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_CONSUMED
	case "completed":
		return companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_COMPLETED
	default:
		return companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_UNSPECIFIED
	}
}
