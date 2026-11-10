package config

import (
	"strings"
	"testing"
)

func TestAmoCompanyEntryConfiguration(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "postgres://localhost/company")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("COMPANY_GATEWAY_SERVICE_TOKEN", strings.Repeat("g", 32))
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_TOKEN", strings.Repeat("p", 32))
	t.Setenv("COMPANY_ENTRY_SUBJECT_CHECK_URL", "")
	t.Setenv("COMPANY_ENTRY_SUBJECT_CHECK_KEY", "")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPANY_ENTRY_SUBJECT_CHECK_URL", "https://core.example.test/internal/v1/company-entry/subject-check")
	for _, bad := range []string{"", strings.Repeat("x", 31), strings.Repeat("g", 32), strings.Repeat("p", 32), strings.Repeat("x", 32) + " ", strings.Repeat("x", 32) + "\n"} {
		t.Setenv("COMPANY_ENTRY_SUBJECT_CHECK_KEY", bad)
		if _, err := Load(); err == nil {
			t.Fatal("invalid callback key accepted")
		}
	}
	t.Setenv("COMPANY_ENTRY_SUBJECT_CHECK_KEY", strings.Repeat("x", 32))
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://core.example.test/internal/v1/company-entry/subject-check", "https://core.example.test/wrong", "https://core.example.test/internal/v1/company-entry/subject-check?secret=x"} {
		t.Setenv("COMPANY_ENTRY_SUBJECT_CHECK_URL", bad)
		if _, err := Load(); err == nil {
			t.Fatal("unsafe callback URL accepted")
		}
	}
}
