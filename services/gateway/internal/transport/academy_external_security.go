package transport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"google.golang.org/grpc/metadata"
)

const (
	externalSessionCookieName = "teamos_academy_external"
	academyVisitorCookieName  = "teamos_academy_visitor"
	academyVisitorBytes       = 32
	academyVisitorMaxAge      = 365 * 24 * time.Hour
)

func (h *Handler) externalOutgoingContext(r *http.Request) context.Context {
	ctx := outgoingContext(r)
	cookie, err := r.Cookie(externalSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "x-external-session", cookie.Value)
}

func (h *Handler) setExternalSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	maxAge := max(0, int(time.Until(expiresAt).Seconds()))
	http.SetCookie(w, &http.Cookie{
		Name: externalSessionCookieName, Value: token,
		Path: "/api/v1/public/academy", HttpOnly: true,
		Secure: h.cookie.Secure, SameSite: http.SameSiteLaxMode,
		Expires: expiresAt.UTC(), MaxAge: maxAge,
	})
}

// academyVisitorHash maintains a random first-party 256-bit visitor cookie
// but forwards only its one-way hash to Academy. The raw cookie and client IP
// are never placed in gRPC metadata, analytics payloads, or logs.
func (h *Handler) academyVisitorHash(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var raw []byte
	if cookie, err := r.Cookie(academyVisitorCookieName); err == nil {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(cookie.Value)
		if decodeErr == nil && len(decoded) == academyVisitorBytes {
			raw = decoded
		}
	}
	if len(raw) == 0 {
		raw = make([]byte, academyVisitorBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		expiresAt := time.Now().UTC().Add(academyVisitorMaxAge)
		http.SetCookie(w, &http.Cookie{
			Name: academyVisitorCookieName, Value: base64.RawURLEncoding.EncodeToString(raw),
			Path: "/api/v1/public/academy", HttpOnly: true, Secure: h.cookie.Secure,
			SameSite: http.SameSiteLaxMode, Expires: expiresAt, MaxAge: int(academyVisitorMaxAge.Seconds()),
		})
	}
	digest := sha256.Sum256(raw)
	return digest[:], nil
}

func academyAttribution(r *http.Request) (source, medium, campaign, content, term, referrer *string) {
	query := r.URL.Query()
	source = cleanAnalyticsValue(query.Get("utm_source"), 256)
	medium = cleanAnalyticsValue(query.Get("utm_medium"), 256)
	campaign = cleanAnalyticsValue(query.Get("utm_campaign"), 256)
	content = cleanAnalyticsValue(query.Get("utm_content"), 256)
	term = cleanAnalyticsValue(query.Get("utm_term"), 256)
	referrer = cleanReferrer(r.Referer())
	return
}

func cleanAnalyticsValue(value string, limit int) *string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if value == "" {
		return nil
	}
	if len(value) > limit {
		value = strings.ToValidUTF8(value[:limit], "")
	}
	return &value
}

func cleanReferrer(value string) *string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil
	}
	parsed.User = nil
	parsed.Fragment = ""
	parsed.RawQuery = ""
	return cleanAnalyticsValue(parsed.String(), 2048)
}

// requireExternalCSRF protects mutations authenticated by the external
// HttpOnly cookie. The public app origin is configured and compared as an
// origin tuple; the access/session token is never exposed to JavaScript.
func (h *Handler) requireExternalCSRF(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	configured, configuredErr := url.Parse(h.cookie.PublicAppURL)
	provided, providedErr := url.Parse(origin)
	if configuredErr != nil || providedErr != nil || origin == "" ||
		!strings.EqualFold(configured.Scheme, provided.Scheme) ||
		!strings.EqualFold(configured.Host, provided.Host) {
		apierror.Write(w, apierror.Forbidden("Источник запроса внешней сессии не подтверждён"))
		return false
	}
	return true
}
