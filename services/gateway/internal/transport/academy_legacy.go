package transport

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
)

const (
	legacyAcademyWritesReadOnlyEnv = "GATEWAY_ACADEMY_LEGACY_WRITES_READ_ONLY"
	legacyAcademyDeprecatedAt      = "@1784678400"
	legacyAcademySunset            = "Thu, 01 Jul 2027 00:00:00 GMT"
	legacyAcademyWarning           = `299 TeamOS "Устаревший маршрут Academy; перейдите на API версий и прохождений"`

	academyCoursesSuccessor      = "/api/v1/academy/courses"
	academyEnrollmentsSuccessor  = "/api/v1/academy/enrollments"
	publicAcademyAccessSuccessor = "/api/v1/public/academy/access/%7Btoken%7D"
)

// markLegacyAcademyEndpoint keeps the v1 response contract intact while
// making migration state visible to clients and intermediaries. The Sunset
// date applies only to these explicitly marked legacy routes; canonical v1
// course/version/enrollment routes are not affected.
func markLegacyAcademyEndpoint(w http.ResponseWriter, successor string) {
	w.Header().Set("Deprecation", legacyAcademyDeprecatedAt)
	w.Header().Set("Sunset", legacyAcademySunset)
	w.Header().Set("Warning", legacyAcademyWarning)
	w.Header().Add("Link", "<"+successor+">; rel=\"successor-version\"")
	w.Header().Add("Access-Control-Expose-Headers", "Deprecation, Sunset, Link, Warning")
}

// guardLegacyAcademyWrite is deliberately opt-in. The frontend still defaults
// to its legacy Academy feature flag, so deployments must first enable the new
// frontend and then set GATEWAY_ACADEMY_LEGACY_WRITES_READ_ONLY=true. Once set,
// legacy reads continue to work but legacy mutations never reach Academy.
func guardLegacyAcademyWrite(w http.ResponseWriter, successor string) bool {
	markLegacyAcademyEndpoint(w, successor)
	raw := strings.TrimSpace(os.Getenv(legacyAcademyWritesReadOnlyEnv))
	if raw == "" {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	if err == nil && !enabled {
		return false
	}
	// A malformed non-empty rollout value is treated as read-only. A typo in
	// production configuration must not silently reopen legacy mutations.
	apierror.Write(w, apierror.New(
		http.StatusGone,
		"Запись через устаревший API Академии отключена; используйте API версий и прохождений",
	))
	return true
}
