package transport

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errInvalidAmoEntryResponse = errors.New("Некорректный ответ входа amoCRM")

func (h *Handler) requireAmoEntryService(w http.ResponseWriter, r *http.Request) bool {
	if h.provisioningServiceProvider != "rakurs" {
		apierror.Write(w, apierror.Unauthorized("Служебная авторизация недействительна").WithCode("SERVICE_AUTH_INVALID"))
		return false
	}
	return h.requireProvisioningService(w, r)
}

func (h *Handler) GetAmoEntryCompanyStatus(w http.ResponseWriter, r *http.Request) {
	setPrivateNoStore(w)
	if !h.requireAmoEntryService(w, r) {
		return
	}
	var input api.AmoEntryCompanyStatusInput
	if !decodeStrict(w, r, &input) {
		return
	}
	response, err := h.company.GetAmoEntryCompanyStatus(h.provisioningContext(r), &companyv1.GetAmoEntryCompanyStatusRequest{AccountId: input.AccountId})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	result := api.AmoEntryCompanyStatus{State: api.AmoEntryCompanyStatusState(response.GetState())}
	if response.CompanyId != nil {
		id, parseErr := uuid.Parse(response.GetCompanyId())
		if parseErr != nil || id == uuid.Nil {
			h.writeConversionError(w, r, errInvalidAmoEntryResponse)
			return
		}
		result.CompanyId = &id
	}
	if (result.State != api.AmoEntryCompanyStatusStateAbsent && result.State != api.AmoEntryCompanyStatusStatePresent && result.State != api.AmoEntryCompanyStatusStateBlocked) || (result.State == api.AmoEntryCompanyStatusStatePresent && result.CompanyId == nil) {
		h.writeConversionError(w, r, errInvalidAmoEntryResponse)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ProvisionAmoWidgetEntry(w http.ResponseWriter, r *http.Request) {
	setPrivateNoStore(w)
	if !h.requireAmoEntryService(w, r) {
		return
	}
	var input api.AmoWidgetEntryInput
	if !decodeStrict(w, r, &input) {
		return
	}
	response, err := h.company.ProvisionAmoWidgetEntry(h.provisioningContext(r), &companyv1.ProvisionAmoWidgetEntryRequest{
		AccountId: input.AccountId, ActorId: input.ActorId, IntegrationId: input.IntegrationId.String(), InstallationId: input.InstallationId.String(), Intent: string(input.Intent), Name: input.Name, Email: input.Email,
	})
	if err != nil {
		rpcStatus, ok := status.FromError(err)
		if ok && rpcStatus.Code() == codes.InvalidArgument && grpcErrorCode(rpcStatus) == "AMO_PROFILE_UNAVAILABLE" {
			apierror.Write(w, apierror.New(http.StatusUnprocessableEntity, "В профиле amoCRM отсутствует корректный email. Обновите профиль и повторите вход").WithCode("AMO_PROFILE_UNAVAILABLE"))
			return
		}
		h.writeRPCError(w, r, err)
		return
	}
	companyID, err := uuid.Parse(response.GetCompanyId())
	if err != nil || companyID == uuid.Nil {
		h.writeConversionError(w, r, errInvalidAmoEntryResponse)
		return
	}
	userID, err := uuid.Parse(response.GetUserId())
	if err != nil || userID == uuid.Nil || response.Code == "" || response.ExpiresAt == nil || !response.ExpiresAt.IsValid() {
		h.writeConversionError(w, r, errInvalidAmoEntryResponse)
		return
	}
	writeJSON(w, http.StatusOK, api.AmoWidgetEntry{CompanyId: companyID, UserId: userID, Code: response.Code, ExpiresAt: response.ExpiresAt.AsTime()})
}

func (h *Handler) ExchangeAmoCompanyEntry(w http.ResponseWriter, r *http.Request) {
	setPrivateNoStore(w)
	var input api.AmoEntryExchangeInput
	if !decodeStrict(w, r, &input) {
		return
	}
	response, err := h.company.ExchangeAmoCompanyEntry(outgoingContext(r), &companyv1.ExchangeAmoCompanyEntryRequest{Code: input.Code})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	prepared, ok := h.prepareSession(w, r, response.GetSession())
	if ok {
		writeJSON(w, http.StatusOK, prepared)
	}
}
