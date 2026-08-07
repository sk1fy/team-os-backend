package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc/metadata"
)

const provisioningServiceMarker = "provisioning"

func (h *Handler) SetProvisioningServiceToken(token string) {
	h.provisioningServiceToken = strings.TrimSpace(token)
}

func (h *Handler) SetProvisioningServiceProvider(provider string) {
	h.provisioningServiceProvider = strings.TrimSpace(provider)
}

func (h *Handler) SetCompanyServiceToken(token string) {
	h.companyServiceToken = strings.TrimSpace(token)
}

func (h *Handler) ProvisionCompany(w http.ResponseWriter, r *http.Request, params api.ProvisionCompanyParams) {
	if !h.requireProvisioningService(w, r) {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(w, string(params.IdempotencyKey))
	if !ok {
		return
	}
	var input api.ProvisionCompanyInput
	if !decode(w, r, &input) {
		return
	}
	provider, ok := h.requireProvisioningProvider(w, input.Provider)
	if !ok {
		return
	}
	response, err := h.company.ProvisionCompany(h.provisioningContext(r), &companyv1.ProvisionCompanyRequest{
		Provider: provider, ExternalAccountId: input.ExternalAccountId,
		CompanyName: input.CompanyName, InitiatorExternalUserId: input.InitiatorExternalUserId,
		Owner: &companyv1.ProvisioningParticipant{
			ExternalUserId: input.Owner.ExternalUserId, Email: string(input.Owner.Email),
			FirstName: input.Owner.FirstName, LastName: input.Owner.LastName,
		},
		Admin: &companyv1.ProvisioningParticipant{
			ExternalUserId: input.Admin.ExternalUserId, Email: string(input.Admin.Email),
			FirstName: input.Admin.FirstName, LastName: input.Admin.LastName,
		},
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := h.provisionCompanyFromProto(response)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	statusCode := http.StatusOK
	if response.GetCreated() {
		statusCode = http.StatusCreated
	}
	setPrivateNoStore(w)
	writeJSON(w, statusCode, converted)
}

func (h *Handler) GetProvisionedCompanyStatus(
	w http.ResponseWriter,
	r *http.Request,
	params api.GetProvisionedCompanyStatusParams,
) {
	if !h.requireProvisioningService(w, r) {
		return
	}
	response, err := h.company.GetProvisionedCompanyStatus(
		h.provisioningContext(r),
		&companyv1.GetProvisionedCompanyStatusRequest{
			Provider: h.provisioningServiceProvider, ExternalAccountId: params.ExternalAccountId,
		},
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted := api.ProvisionedCompanyStatusResponse{Exists: response.GetExists()}
	if response.GetExists() {
		companyID, parseErr := uuid.Parse(response.GetCompanyId())
		if parseErr != nil {
			h.writeConversionError(w, r, parseErr)
			return
		}
		status, conversionErr := companyStatusFromProto(response.GetCompanyStatus())
		if conversionErr != nil {
			h.writeConversionError(w, r, conversionErr)
			return
		}
		converted.CompanyId = &companyID
		converted.CompanyStatus = &status
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) IssueSsoToken(w http.ResponseWriter, r *http.Request) {
	if !h.requireProvisioningService(w, r) {
		return
	}
	var input api.IssueSsoTokenInput
	if !decode(w, r, &input) {
		return
	}
	provider, ok := h.requireProvisioningProvider(w, input.Provider)
	if !ok {
		return
	}
	response, err := h.company.IssueSsoToken(h.provisioningContext(r), &companyv1.IssueSsoTokenRequest{
		Provider: provider, ExternalAccountId: input.ExternalAccountId,
		ExternalUserId: input.ExternalUserId,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	if response.GetToken() == "" || response.GetExpiresAt() == nil || !response.GetExpiresAt().IsValid() {
		h.writeConversionError(w, r, errors.New("company returned an invalid provisioning session"))
		return
	}
	path := ""
	switch response.GetContinuationKind() {
	case companyv1.ProvisioningContinuationKind_PROVISIONING_CONTINUATION_KIND_ONBOARDING:
		path = "/onboarding"
	case companyv1.ProvisioningContinuationKind_PROVISIONING_CONTINUATION_KIND_SSO:
		path = "/sso"
	default:
		h.writeConversionError(w, r, errors.New("company returned an invalid provisioning continuation"))
		return
	}
	redirectURL, err := h.publicTokenURL(path, response.GetToken())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusCreated, api.SsoIssueResponse{
		RedirectUrl: redirectURL, ExpiresAt: response.GetExpiresAt().AsTime(),
	})
}

func (h *Handler) GetBootstrapActivation(w http.ResponseWriter, r *http.Request, token api.BootstrapToken) {
	response, err := h.company.GetBootstrapActivation(outgoingContext(r), &companyv1.GetBootstrapActivationRequest{Token: token})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := bootstrapActivationFromProto(response.GetActivation())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CompleteBootstrapActivation(w http.ResponseWriter, r *http.Request, token api.BootstrapToken) {
	var input api.CompleteBootstrapActivationInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.CompleteBootstrapActivation(outgoingContext(r), &companyv1.CompleteBootstrapActivationRequest{
		Token: token, Password: input.Password,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	onboarding, err := h.onboardingFromProto(response.GetOnboarding())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	setPrivateNoStore(w)
	session, ok := h.prepareSession(w, r, response.GetSession())
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, api.BootstrapAuthResponse{
		AccessToken: session.AccessToken, User: session.User, Onboarding: onboarding,
	})
}

func (h *Handler) ExchangeSsoToken(w http.ResponseWriter, r *http.Request) {
	var input api.ExchangeSsoTokenInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.ExchangeSsoToken(outgoingContext(r), &companyv1.ExchangeSsoTokenRequest{Token: input.Token})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeSession(w, r, http.StatusOK, response.GetSession())
}

func (h *Handler) GetOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetOnboardingStatus(outgoingContext(r), &companyv1.GetOnboardingStatusRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := h.onboardingFromProto(response.GetOnboarding())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) ReissueOnboardingActivation(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.ReissueOnboardingActivation(outgoingContext(r), &companyv1.ReissueOnboardingActivationRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := h.onboardingFromProto(response.GetOnboarding())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	setPrivateNoStore(w)
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) requireProvisioningService(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Service") ||
		!secureCredentialEqual(parts[1], h.provisioningServiceToken) {
		apierror.Write(w, apierror.Unauthorized("Служебные учётные данные недействительны").WithCode("SERVICE_AUTH_INVALID"))
		return false
	}
	return true
}

func (h *Handler) requireProvisioningProvider(w http.ResponseWriter, provider string) (string, bool) {
	provider = strings.TrimSpace(provider)
	if provider == "" || provider != h.provisioningServiceProvider {
		apierror.Write(w, apierror.Forbidden("Служебной учётной записи недоступен этот провайдер").WithCode("SERVICE_PROVIDER_FORBIDDEN"))
		return "", false
	}
	return provider, true
}

func secureCredentialEqual(provided, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return provided != "" && expected != "" &&
		subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func requiredIdempotencyKey(w http.ResponseWriter, value string) (string, bool) {
	converted := api.OptionalIdempotencyKey(value)
	return resolveIdempotencyKey(w, &converted)
}

func (h *Handler) provisioningContext(r *http.Request) context.Context {
	return metadata.AppendToOutgoingContext(
		outgoingContext(r),
		"x-teamos-service", provisioningServiceMarker,
		"x-teamos-service-token", h.companyServiceToken,
	)
}

func (h *Handler) publicTokenURL(path, token string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(h.cookie.PublicAppURL), "/")
	if base == "" || token == "" {
		return "", errors.New("public app URL or token is empty")
	}
	return base + path + "?token=" + url.QueryEscape(token), nil
}

func (h *Handler) provisionCompanyFromProto(value *companyv1.ProvisionCompanyResponse) (api.ProvisionCompanyResponse, error) {
	if value == nil || value.GetBootstrapToken() == "" ||
		value.GetBootstrapExpiresAt() == nil || !value.GetBootstrapExpiresAt().IsValid() {
		return api.ProvisionCompanyResponse{}, errors.New("company returned an invalid provisioning response")
	}
	companyID, err := uuid.Parse(value.GetCompanyId())
	if err != nil {
		return api.ProvisionCompanyResponse{}, err
	}
	status, err := companyStatusFromProto(value.GetCompanyStatus())
	if err != nil {
		return api.ProvisionCompanyResponse{}, err
	}
	role, err := bootstrapRoleFromProto(value.GetInitiatorRole())
	if err != nil {
		return api.ProvisionCompanyResponse{}, err
	}
	continueURL, err := h.publicTokenURL("/onboarding", value.GetBootstrapToken())
	if err != nil {
		return api.ProvisionCompanyResponse{}, err
	}
	return api.ProvisionCompanyResponse{
		CompanyId: companyID, CompanyStatus: status, Created: value.GetCreated(),
		InitiatorRole: role, ContinueUrl: continueURL,
		ExpiresAt: value.GetBootstrapExpiresAt().AsTime(),
	}, nil
}

func bootstrapActivationFromProto(value *companyv1.BootstrapActivation) (api.BootstrapActivation, error) {
	if value == nil || value.GetExpiresAt() == nil || !value.GetExpiresAt().IsValid() {
		return api.BootstrapActivation{}, errors.New("company returned an invalid bootstrap activation")
	}
	companyID, err := uuid.Parse(value.GetCompanyId())
	if err != nil {
		return api.BootstrapActivation{}, err
	}
	status, err := companyStatusFromProto(value.GetCompanyStatus())
	if err != nil {
		return api.BootstrapActivation{}, err
	}
	user, err := bootstrapParticipantFromProto(value.GetUser())
	if err != nil {
		return api.BootstrapActivation{}, err
	}
	state, err := bootstrapStateFromProto(value.GetState())
	if err != nil {
		return api.BootstrapActivation{}, err
	}
	return api.BootstrapActivation{
		CompanyId: companyID, CompanyName: value.GetCompanyName(), CompanyStatus: status,
		User: user, ExpiresAt: value.GetExpiresAt().AsTime(), State: state,
	}, nil
}

func (h *Handler) onboardingFromProto(value *companyv1.OnboardingState) (api.OnboardingState, error) {
	if value == nil {
		return api.OnboardingState{}, errors.New("company returned an empty onboarding state")
	}
	companyID, err := uuid.Parse(value.GetCompanyId())
	if err != nil {
		return api.OnboardingState{}, err
	}
	status, err := companyStatusFromProto(value.GetCompanyStatus())
	if err != nil {
		return api.OnboardingState{}, err
	}
	result := api.OnboardingState{CompanyId: companyID, CompanyStatus: status, Completed: value.GetCompleted()}
	if value.GetPendingUser() != nil {
		pending, conversionErr := bootstrapParticipantFromProto(value.GetPendingUser())
		if conversionErr != nil {
			return api.OnboardingState{}, conversionErr
		}
		result.PendingUser = &pending
	}
	if value.GetExpiresAt() != nil {
		if !value.GetExpiresAt().IsValid() {
			return api.OnboardingState{}, errors.New("company returned an invalid activation expiration")
		}
		expiresAt := value.GetExpiresAt().AsTime()
		result.ExpiresAt = &expiresAt
	}
	if value.GetActivationToken() != "" {
		activationURL, urlErr := h.publicTokenURL("/onboarding", value.GetActivationToken())
		if urlErr != nil {
			return api.OnboardingState{}, urlErr
		}
		result.ActivationUrl = &activationURL
	}
	return result, nil
}

func bootstrapParticipantFromProto(value *companyv1.BootstrapParticipant) (api.BootstrapParticipant, error) {
	if value == nil {
		return api.BootstrapParticipant{}, errors.New("company returned an empty bootstrap user")
	}
	userID, err := uuid.Parse(value.GetUserId())
	if err != nil {
		return api.BootstrapParticipant{}, err
	}
	role, err := bootstrapRoleFromProto(value.GetRole())
	if err != nil {
		return api.BootstrapParticipant{}, err
	}
	status, err := statusFromProto(value.GetStatus())
	if err != nil {
		return api.BootstrapParticipant{}, err
	}
	return api.BootstrapParticipant{
		UserId: userID, Email: api.Email(value.GetEmail()), FirstName: value.GetFirstName(),
		LastName: value.GetLastName(), Role: role, Status: status,
	}, nil
}

func bootstrapRoleFromProto(value companyv1.UserRole) (api.BootstrapRole, error) {
	switch value {
	case companyv1.UserRole_USER_ROLE_OWNER:
		return api.BootstrapRoleOwner, nil
	case companyv1.UserRole_USER_ROLE_ADMIN:
		return api.BootstrapRoleAdmin, nil
	default:
		return "", errors.New("company returned an invalid bootstrap role")
	}
}

func bootstrapStateFromProto(value companyv1.BootstrapActivationState) (api.BootstrapActivationState, error) {
	switch value {
	case companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_PENDING:
		return api.BootstrapActivationStatePending, nil
	case companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_EXPIRED:
		return api.BootstrapActivationStateExpired, nil
	case companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_CONSUMED:
		return api.BootstrapActivationStateConsumed, nil
	case companyv1.BootstrapActivationState_BOOTSTRAP_ACTIVATION_STATE_COMPLETED:
		return api.BootstrapActivationStateCompleted, nil
	default:
		return "", errors.New("company returned an invalid bootstrap activation state")
	}
}
