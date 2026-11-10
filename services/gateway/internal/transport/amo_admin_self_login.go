package transport

import (
	"net/http"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
)

func (h *Handler) AmoAdminSelfLogin(w http.ResponseWriter, _ *http.Request) {
	setPrivateNoStore(w)
	apierror.Write(w, apierror.Forbidden("Вход по данным браузера отключён. Откройте TeamOS из виджета amoCRM").WithCode("AMO_BROWSER_ENTRY_DISABLED"))
}
