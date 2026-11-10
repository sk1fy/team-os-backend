package application

import (
	"context"
	"errors"
	"testing"
)

func TestBrowserEntryCannotUseDatabaseOrImporter(t *testing.T) {
	// No pool, clock, importer or token issuer: retired entry must not touch them.
	service := &Service{}
	for _, admin := range []bool{false, true} {
		for _, active := range []bool{false, true} {
			for _, actor := range []string{"101", "102", ""} {
				bootstrap, err := service.BootstrapAmoCompany(context.Background(), AmoCompanyBootstrapInput{
					AmoAccountID: "31355990", SelfUserID: actor,
					Users: []AmoCompanyBootstrapUser{{ID: actor, Email: "spoof@example.test", Name: "Подмена", IsAdmin: admin, IsActive: active}},
				})
				assertBrowserEntryDisabled(t, err)
				if bootstrap != (AmoCompanyBootstrapResult{}) {
					t.Fatalf("bootstrap returned credentials: %+v", bootstrap)
				}
				login, err := service.AmoAdminSelfLogin(context.Background(), AmoAdminSelfLoginInput{
					AmoAccountID: "31355990", SelfUserID: actor,
					Users: []AmoAdminUserAssertion{{ID: actor, IsAdmin: admin, IsActive: active}},
				})
				assertBrowserEntryDisabled(t, err)
				if login != (AmoAdminSelfLoginResult{}) {
					t.Fatalf("login returned credentials: %+v", login)
				}
			}
		}
	}
}

func assertBrowserEntryDisabled(t *testing.T, err error) {
	t.Helper()
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Kind != ErrorForbidden || appErr.Code != ErrorCodeAmoBrowserEntryDisabled {
		t.Fatalf("expected retired entry error, got %v", err)
	}
}
