package authmw

import (
	"net/http"
	"testing"
)

func TestAmoCompanyEntryPublicBoundaries(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/amocrm/entry-exchange", "/api/v1/provisioning/amocrm/company-status", "/api/v1/provisioning/amocrm/widget-entries"} {
		if !isPublic(http.MethodPost, path) {
			t.Fatalf("dedicated authentication route requires user JWT %s", path)
		}
		if isPublic(http.MethodGet, path) || isPublic(http.MethodPost, path+"/extra") {
			t.Fatalf("overbroad public route %s", path)
		}
	}
}
