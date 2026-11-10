package grpc

import (
	"context"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) GetAmoEntryCompanyStatus(ctx context.Context, request *companyv1.GetAmoEntryCompanyStatusRequest) (*companyv1.GetAmoEntryCompanyStatusResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.GetAmoEntryCompanyStatus(ctx, request.AccountId)
	if err != nil {
		return nil, transportError(err)
	}
	response := &companyv1.GetAmoEntryCompanyStatusResponse{State: result.State}
	if result.CompanyID != nil {
		value := result.CompanyID.String()
		response.CompanyId = &value
	}
	return response, nil
}

func (s *Server) ProvisionAmoWidgetEntry(ctx context.Context, request *companyv1.ProvisionAmoWidgetEntryRequest) (*companyv1.ProvisionAmoWidgetEntryResponse, error) {
	if err := s.authorizeProvisioning(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.ProvisionAmoWidgetEntry(ctx, application.AmoWidgetEntryInput{
		AmoEntrySubject: application.AmoEntrySubject{AccountID: request.AccountId, ActorID: request.ActorId, IntegrationID: request.IntegrationId, InstallationID: request.InstallationId},
		Intent:          request.Intent, Name: request.Name, Email: request.Email,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ProvisionAmoWidgetEntryResponse{CompanyId: result.CompanyID.String(), UserId: result.UserID.String(), Code: result.Code, ExpiresAt: timestamppb.New(result.ExpiresAt)}, nil
}

func (s *Server) ExchangeAmoCompanyEntry(ctx context.Context, request *companyv1.ExchangeAmoCompanyEntryRequest) (*companyv1.ExchangeAmoCompanyEntryResponse, error) {
	if request == nil {
		return nil, invalidRequest()
	}
	result, err := s.application.ExchangeAmoCompanyEntry(ctx, request.Code, sessionMeta(ctx))
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ExchangeAmoCompanyEntryResponse{Session: authSessionToProto(result)}, nil
}
