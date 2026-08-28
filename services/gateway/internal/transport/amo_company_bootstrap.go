package transport

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/amochallenge"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) BootstrapAmoCompany(w http.ResponseWriter, r *http.Request) {
	setPrivateNoStore(w)
	claims, ok := amochallenge.FromContext(r.Context())
	if !ok || claims.Purpose != amochallenge.PurposeCompanyBootstrap {
		h.writeConversionError(w, r, errors.New("amoCRM company bootstrap challenge middleware missing"))
		return
	}
	var input api.AmoCompanyBootstrapInput
	if !decodeStrict(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Account.Id) != claims.AmoAccountID {
		apierror.Write(w, apierror.Forbidden("Account ID amoCRM не совпадает с challenge").WithCode("AMO_COMPANY_BOOTSTRAP_ACCOUNT_MISMATCH"))
		return
	}
	users := make([]*companyv1.AmoCompanyBootstrapUser, len(input.Users))
	for index, user := range input.Users {
		users[index] = &companyv1.AmoCompanyBootstrapUser{
			Id: user.Id, Email: string(user.Email), Name: user.Name,
			IsAdmin: user.IsAdmin, IsActive: user.IsActive,
		}
	}
	companyName := ""
	if input.Account.Name != nil {
		companyName = strings.TrimSpace(*input.Account.Name)
	}
	subdomain := ""
	if input.Account.Subdomain != nil {
		subdomain = strings.TrimSpace(*input.Account.Subdomain)
	}
	response, err := h.company.BootstrapAmoCompany(
		h.provisioningContext(r),
		&companyv1.BootstrapAmoCompanyRequest{
			AmoAccountId: claims.AmoAccountID, CompanyName: companyName, Subdomain: subdomain,
			SelfUserId: strings.TrimSpace(input.SelfUserId), Users: users,
		},
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	companyID, companyErr := uuid.Parse(response.GetCompanyId())
	userID, userErr := uuid.Parse(response.GetUserId())
	if companyErr != nil || userErr != nil ||
		response.GetAction() != companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER ||
		response.GetRole() != companyv1.UserRole_USER_ROLE_OWNER || response.GetAccessToken() == "" {
		h.writeConversionError(w, r, errors.New("company returned invalid amoCRM company bootstrap result"))
		return
	}
	writeJSON(w, http.StatusCreated, api.AmoCompanyBootstrapResponse{
		Action: "register", CompanyId: companyID, UserId: userID, Role: api.UserRoleOwner,
		RedirectUrl: h.accessLinkURL(r, response.GetAccessToken()),
	})
}
