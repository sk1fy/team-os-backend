package transport

import (
	"errors"
	"net/http"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/amochallenge"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) AmoAdminSelfLogin(
	w http.ResponseWriter,
	r *http.Request,
) {
	setPrivateNoStore(w)
	claims, ok := amochallenge.FromContext(r.Context())
	if !ok {
		h.writeConversionError(w, r, errors.New("amoCRM browser challenge middleware missing"))
		return
	}
	var input api.AmoAdminSelfLoginInput
	if !decodeStrict(w, r, &input) {
		return
	}
	users := make([]*companyv1.AmoAdminUserAssertion, len(input.Users))
	for index, user := range input.Users {
		users[index] = &companyv1.AmoAdminUserAssertion{
			Id: user.Id, IsAdmin: user.IsAdmin, IsActive: user.IsActive,
		}
	}
	response, err := h.company.AmoAdminSelfLogin(
		h.provisioningContext(r),
		&companyv1.AmoAdminSelfLoginRequest{
			AmoAccountId: claims.AmoAccountID, SelfUserId: input.SelfUserId, Users: users,
		},
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	if !response.GetAllowed() || response.GetAction() != companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_LOGIN ||
		response.GetAccessToken() == "" {
		h.writeConversionError(w, r, errors.New("company returned invalid amoCRM self-login result"))
		return
	}
	role := api.UserRole("")
	switch response.GetRole() {
	case companyv1.UserRole_USER_ROLE_ADMIN:
		role = api.UserRoleAdmin
	case companyv1.UserRole_USER_ROLE_OWNER:
		role = api.UserRoleOwner
	default:
		h.writeConversionError(w, r, errors.New("company returned invalid amoCRM self-login role"))
		return
	}
	writeJSON(w, http.StatusOK, api.AmoAdminSelfLoginResponse{
		Allowed: true, Action: "login", Role: role,
		RedirectUrl: h.accessLinkURL(r, response.GetAccessToken()),
	})
}
