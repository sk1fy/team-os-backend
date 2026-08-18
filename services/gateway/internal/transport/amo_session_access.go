package transport

import (
	"errors"
	"net/http"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) CheckAmoSessionAccess(w http.ResponseWriter, r *http.Request) {
	setPrivateNoStore(w)
	var input api.AmoSessionAccessInput
	if !decodeStrict(w, r, &input) {
		return
	}
	response, err := h.company.CheckAmoSessionAccess(
		outgoingContext(r),
		&companyv1.CheckAmoSessionAccessRequest{AmoAccountId: input.AmoAccountId},
	)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	if !response.GetAllowed() || response.GetRedirectUrl() != "/schedule" {
		h.writeConversionError(w, r, errors.New("company returned invalid amoCRM session access"))
		return
	}
	role := api.UserRole("")
	switch response.GetRole() {
	case companyv1.UserRole_USER_ROLE_ADMIN:
		role = api.UserRoleAdmin
	case companyv1.UserRole_USER_ROLE_OWNER:
		role = api.UserRoleOwner
	default:
		h.writeConversionError(w, r, errors.New("company returned invalid amoCRM session role"))
		return
	}
	writeJSON(w, http.StatusOK, api.AmoSessionAccessResponse{
		Allowed: true, Role: role, RedirectUrl: response.GetRedirectUrl(),
	})
}
