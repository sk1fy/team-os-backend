package transport

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

const sessionSelectorCookie = "teamos_session"
const sessionRoutingHeader = "X-Teamos-Session"
const legacySessionID = "legacy"
const maxRefreshCookieLength = 512
const sessionSelectorTTL = 365 * 24 * time.Hour

type refreshRoute struct {
	sessionID  string
	cookieName string
	familyID   *string
}

func requestRefreshRoute(r *http.Request) (refreshRoute, bool) {
	selected, explicit := r.Header[http.CanonicalHeaderKey(sessionRoutingHeader)]
	raw := ""
	if explicit {
		if len(selected) != 1 {
			return refreshRoute{}, false
		}
		raw = selected[0]
		if raw == legacySessionID {
			return refreshRoute{sessionID: legacySessionID, cookieName: refreshCookieName}, true
		}
	} else if cookie, err := r.Cookie(sessionSelectorCookie); err == nil {
		raw = cookie.Value
	} else {
		return refreshRoute{sessionID: legacySessionID, cookieName: refreshCookieName}, true
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.String() != raw {
		return refreshRoute{}, false
	}
	return refreshRoute{sessionID: raw, familyID: &raw, cookieName: refreshCookieName + "_" + raw}, true
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request, _ api.RefreshParams) {
	setPrivateNoStore(w)
	route, ok := requestRefreshRoute(r)
	if !ok {
		apierror.Write(w, apierror.Unauthorized("Сессия недействительна или истекла"))
		return
	}
	cookie, err := r.Cookie(route.cookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" || len(cookie.Value) > maxRefreshCookieLength {
		apierror.Write(w, apierror.Unauthorized("Сессия недействительна или истекла"))
		return
	}
	response, err := h.company.Refresh(outgoingContext(r), &companyv1.RefreshRequest{RefreshToken: cookie.Value, FamilyId: route.familyID})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	session := response.GetSession()
	if route.familyID != nil && session.GetFamilyId() != *route.familyID {
		h.writeConversionError(w, r, errors.New("company returned a different session family"))
		return
	}
	prepared, ok := h.sessionResponse(w, r, session, route)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, prepared)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request, _ api.LogoutParams) {
	setPrivateNoStore(w)
	route, ok := requestRefreshRoute(r)
	if !ok {
		apierror.Write(w, apierror.Unauthorized("Сессия недействительна или истекла"))
		return
	}
	token := ""
	if cookie, err := r.Cookie(route.cookieName); err == nil {
		token = cookie.Value
	}
	if len(token) > maxRefreshCookieLength {
		apierror.Write(w, apierror.Unauthorized("Сессия недействительна или истекла"))
		return
	}
	if _, err := h.company.Logout(outgoingContext(r), &companyv1.LogoutRequest{RefreshToken: token, FamilyId: route.familyID}); err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.clearSessionCookie(w, route.cookieName)
	w.WriteHeader(http.StatusNoContent)
}

// Only explicit session creation selects a family. A delayed refresh or logout
// response must never replace the selection made by a newer login.
func (h *Handler) prepareSession(w http.ResponseWriter, r *http.Request, session *companyv1.AuthSession) (api.AuthResponse, bool) {
	setPrivateNoStore(w)
	id, err := uuid.Parse(session.GetFamilyId())
	if err != nil || id == uuid.Nil {
		h.writeConversionError(w, r, errors.New("company returned an invalid session family"))
		return api.AuthResponse{}, false
	}
	family := id.String()
	response, ok := h.sessionResponse(w, r, session, refreshRoute{sessionID: family, cookieName: refreshCookieName + "_" + family})
	if !ok {
		return api.AuthResponse{}, false
	}
	h.setSessionCookie(w, sessionSelectorCookie, family, time.Now().Add(sessionSelectorTTL))
	h.clearSessionCookie(w, refreshCookieName)
	return response, true
}

func (h *Handler) sessionResponse(w http.ResponseWriter, r *http.Request, session *companyv1.AuthSession, route refreshRoute) (api.AuthResponse, bool) {
	if session == nil || session.GetRefreshToken() == "" || len(session.GetRefreshToken()) > maxRefreshCookieLength || session.GetAccessToken() == "" || session.GetRefreshExpiresAt() == nil || !session.GetRefreshExpiresAt().IsValid() {
		h.writeConversionError(w, r, errors.New("company returned an invalid auth session"))
		return api.AuthResponse{}, false
	}
	user, err := userFromProto(session.GetUser())
	if err != nil {
		h.writeConversionError(w, r, err)
		return api.AuthResponse{}, false
	}
	h.setSessionCookie(w, route.cookieName, session.GetRefreshToken(), session.GetRefreshExpiresAt().AsTime())
	return api.AuthResponse{AccessToken: session.GetAccessToken(), User: user, SessionId: &route.sessionID}, true
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/api/v1/auth", Expires: expires,
		MaxAge: max(1, int(time.Until(expires).Seconds())), HttpOnly: true, Secure: h.cookie.Secure, SameSite: http.SameSiteLaxMode})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/api/v1/auth", Expires: time.Unix(1, 0), MaxAge: -1,
		HttpOnly: true, Secure: h.cookie.Secure, SameSite: http.SameSiteLaxMode})
}
