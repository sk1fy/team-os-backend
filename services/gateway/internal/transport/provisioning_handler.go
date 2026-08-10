package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc/metadata"
)

const provisioningServiceMarker = "provisioning"

func (h *Handler) SetProvisioningServiceToken(token string) {
	h.provisioningServiceToken = strings.TrimSpace(token)
}

func (h *Handler) SetProvisioningAllowUnauthenticated(allow bool) {
	h.provisioningAllowUnauthenticated = allow
}

func (h *Handler) SetProvisioningServiceProvider(provider string) {
	h.provisioningServiceProvider = strings.TrimSpace(provider)
}

func (h *Handler) SetCompanyServiceToken(token string) {
	h.companyServiceToken = strings.TrimSpace(token)
}

func (h *Handler) CheckAmoAccount(w http.ResponseWriter, r *http.Request, amoAccountID string) {
	response, err := h.company.CheckAmoAccount(outgoingContext(r), &companyv1.CheckAmoAccountRequest{
		Provider: h.provisioningServiceProvider, ExternalAccountId: amoAccountID,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusOK, api.AmoAccountAvailabilityResponse{Exists: response.GetExists()})
}

func (h *Handler) IssueCompanyRegistrationToken(w http.ResponseWriter, r *http.Request) {
	if !h.requireProvisioningService(w, r) {
		return
	}
	var input api.IssueCompanyRegistrationTokenInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.IssueCompanyRegistrationToken(
		h.provisioningContext(r),
		&companyv1.IssueCompanyRegistrationTokenRequest{
			Provider: h.provisioningServiceProvider, ExternalAccountId: input.AmoAccountId,
		},
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	if response.GetToken() == "" || response.GetExpiresAt() == nil || !response.GetExpiresAt().IsValid() {
		h.writeConversionError(w, r, errors.New("company returned an invalid registration token"))
		return
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusCreated, api.CompanyRegistrationTokenResponse{
		RegistrationToken: response.GetToken(), ExpiresAt: response.GetExpiresAt().AsTime(),
	})
}

func (h *Handler) ValidateCompanyRegistrationToken(w http.ResponseWriter, r *http.Request) {
	var input api.ValidateCompanyRegistrationTokenInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.ValidateCompanyRegistrationToken(
		outgoingContext(r),
		&companyv1.ValidateCompanyRegistrationTokenRequest{Token: input.RegistrationToken},
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	state, err := companyRegistrationTokenStateFromProto(response.GetState())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	result := api.CompanyRegistrationTokenValidationResponse{Valid: response.GetValid(), State: state}
	if response.ExternalAccountId != nil {
		result.AmoAccountId = response.ExternalAccountId
	}
	if response.ExpiresAt != nil && response.ExpiresAt.IsValid() {
		expiresAt := response.ExpiresAt.AsTime()
		result.ExpiresAt = &expiresAt
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ExchangeAmoWidgetSession(w http.ResponseWriter, r *http.Request, params api.ExchangeAmoWidgetSessionParams) {
	setPrivateNoStore(w)
	var input api.AmoWidgetSessionInput
	if !decodeOptional(w, r, &input) {
		return
	}
	headerToken := strings.TrimSpace(r.Header.Get("X-Auth-Token"))
	if params.XAuthToken != nil {
		headerToken = strings.TrimSpace(*params.XAuthToken)
	}
	bodyToken := ""
	if input.Token != nil {
		bodyToken = strings.TrimSpace(*input.Token)
	}
	if headerToken != "" && bodyToken != "" {
		apierror.Write(w, apierror.BadRequest("Передайте токен amoCRM только одним способом"))
		return
	}
	token := bodyToken
	if token == "" {
		token = headerToken
	}
	if len(token) < 32 || len(token) > 8192 {
		apierror.Write(w, apierror.BadRequest("Некорректный токен amoCRM"))
		return
	}
	request := &companyv1.ExchangeAmoWidgetSessionRequest{Token: token}
	if input.User != nil {
		request.ExternalUserId = &input.User.Id
		email := string(input.User.Email)
		request.Email = &email
		if input.User.Name != nil {
			request.UserName = input.User.Name
		}
		companyName := ""
		if input.Account != nil {
			if input.Account.Name != nil {
				companyName = strings.TrimSpace(*input.Account.Name)
			}
			if companyName == "" && input.Account.Subdomain != nil {
				companyName = strings.TrimSpace(*input.Account.Subdomain)
			}
		}
		if companyName != "" {
			request.CompanyName = &companyName
		}
	}
	response, err := h.company.ExchangeAmoWidgetSession(
		outgoingContext(r),
		request,
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	result := api.AmoWidgetSessionResponse{}
	switch response.GetAction() {
	case companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_LOGIN:
		result.Action = api.Login
	case companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER:
		if response.SessionToken == nil && (response.RegistrationToken == nil || response.GetRegistrationToken() == "" ||
			response.ExpiresAt == nil || !response.ExpiresAt.IsValid()) {
			h.writeConversionError(w, r, errors.New("company returned an invalid amoCRM widget registration session"))
			return
		}
		result.Action = api.Register
		if response.RegistrationToken != nil {
			result.RegistrationToken = response.RegistrationToken
		}
	default:
		h.writeConversionError(w, r, errors.New("company returned an invalid amoCRM widget session action"))
		return
	}
	if response.ExternalAccountId != nil {
		result.AmoAccountId = response.ExternalAccountId
	}
	if response.SessionToken != nil {
		if response.GetSessionToken() == "" || response.GetEmail() == "" || response.GetCompanyName() == "" ||
			response.ExpiresAt == nil || !response.ExpiresAt.IsValid() {
			h.writeConversionError(w, r, errors.New("company returned an invalid amoCRM widget continuation"))
			return
		}
		result.SessionToken = response.SessionToken
	}
	if response.Email != nil {
		email := api.Email(response.GetEmail())
		result.Email = &email
	}
	if response.CompanyName != nil {
		result.CompanyName = response.CompanyName
	}
	if response.RequiresPasswordSetup != nil {
		result.RequiresPasswordSetup = response.RequiresPasswordSetup
	}
	if response.ExpiresAt != nil && response.ExpiresAt.IsValid() {
		expiresAt := response.ExpiresAt.AsTime()
		result.ExpiresAt = &expiresAt
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ValidateAmoWidgetContinuation(w http.ResponseWriter, r *http.Request) {
	setPrivateNoStore(w)
	var input api.AmoWidgetContinuationInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.ValidateAmoWidgetContinuation(
		outgoingContext(r),
		&companyv1.ValidateAmoWidgetContinuationRequest{SessionToken: input.SessionToken},
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	if response.GetEmail() == "" || response.GetCompanyName() == "" ||
		response.GetExpiresAt() == nil || !response.GetExpiresAt().IsValid() {
		h.writeConversionError(w, r, errors.New("company returned an invalid amoCRM continuation"))
		return
	}
	writeJSON(w, http.StatusOK, api.AmoWidgetContinuationResponse{
		Email: api.Email(response.GetEmail()), CompanyName: response.GetCompanyName(),
		RequiresPasswordSetup: response.GetRequiresPasswordSetup(),
		ExpiresAt:             response.GetExpiresAt().AsTime(),
	})
}

func (h *Handler) CompleteAmoWidgetContinuation(w http.ResponseWriter, r *http.Request) {
	var input api.CompleteAmoWidgetContinuationInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.CompleteAmoWidgetContinuation(
		outgoingContext(r),
		&companyv1.CompleteAmoWidgetContinuationRequest{
			SessionToken: input.SessionToken, Password: input.Password,
		},
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeSession(w, r, http.StatusOK, response.GetSession())
}

func companyRegistrationTokenStateFromProto(value companyv1.CompanyRegistrationTokenState) (api.CompanyRegistrationTokenState, error) {
	switch value {
	case companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_VALID:
		return api.CompanyRegistrationTokenStateValid, nil
	case companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_INVALID:
		return api.CompanyRegistrationTokenStateInvalid, nil
	case companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_EXPIRED:
		return api.CompanyRegistrationTokenStateExpired, nil
	case companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_CONSUMED:
		return api.CompanyRegistrationTokenStateConsumed, nil
	case companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_REVOKED:
		return api.CompanyRegistrationTokenStateRevoked, nil
	default:
		return "", errors.New("company returned an invalid registration token state")
	}
}

// ProvisionCompany is a deprecated v1 contract tombstone. The former SSO,
// bootstrap and company-provisioning implementations are deliberately gone.
func (h *Handler) ProvisionCompany(w http.ResponseWriter, r *http.Request, _ api.ProvisionCompanyParams) {
	if !h.requireProvisioningService(w, r) {
		return
	}
	writeRetiredRegistrationFlow(w)
}

func (h *Handler) GetProvisionedCompanyStatus(w http.ResponseWriter, r *http.Request, _ api.GetProvisionedCompanyStatusParams) {
	if !h.requireProvisioningService(w, r) {
		return
	}
	writeRetiredRegistrationFlow(w)
}

func (h *Handler) IssueSsoToken(w http.ResponseWriter, r *http.Request) {
	if !h.requireProvisioningService(w, r) {
		return
	}
	writeRetiredRegistrationFlow(w)
}

func (h *Handler) GetBootstrapActivation(w http.ResponseWriter, _ *http.Request, _ api.BootstrapToken) {
	writeRetiredRegistrationFlow(w)
}

func (h *Handler) CompleteBootstrapActivation(w http.ResponseWriter, _ *http.Request, _ api.BootstrapToken) {
	writeRetiredRegistrationFlow(w)
}

func (h *Handler) ExchangeSsoToken(w http.ResponseWriter, _ *http.Request) {
	writeRetiredRegistrationFlow(w)
}

func (h *Handler) GetOnboardingStatus(w http.ResponseWriter, _ *http.Request) {
	writeRetiredRegistrationFlow(w)
}

func (h *Handler) ReissueOnboardingActivation(w http.ResponseWriter, _ *http.Request) {
	writeRetiredRegistrationFlow(w)
}

func writeRetiredRegistrationFlow(w http.ResponseWriter) {
	setPrivateNoStore(w)
	apierror.Write(w, apierror.New(http.StatusGone, "Прежний сценарий регистрации отключён").WithCode("REGISTRATION_FLOW_RETIRED"))
}

func (h *Handler) requireProvisioningService(w http.ResponseWriter, r *http.Request) bool {
	if h.provisioningAllowUnauthenticated {
		return true
	}
	if r == nil {
		apierror.Write(w, apierror.Unauthorized("Служебная авторизация недействительна").WithCode("SERVICE_AUTH_INVALID"))
		return false
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Service") ||
		!secureCredentialEqual(parts[1], h.provisioningServiceToken) {
		apierror.Write(w, apierror.Unauthorized("Служебная авторизация недействительна").WithCode("SERVICE_AUTH_INVALID"))
		return false
	}
	return true
}

func secureCredentialEqual(left, right string) bool {
	leftHash, rightHash := sha256.Sum256([]byte(left)), sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func (h *Handler) provisioningContext(r *http.Request) context.Context {
	return metadata.AppendToOutgoingContext(
		r.Context(),
		"x-teamos-service", provisioningServiceMarker,
		"x-teamos-service-token", h.companyServiceToken,
		"x-teamos-provider", h.provisioningServiceProvider,
	)
}
