package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
)

func TestValidateAmoAdminAssertion(t *testing.T) {
	valid := []AmoAdminUserAssertion{{ID: "101", IsAdmin: true, IsActive: true}, {ID: "102"}}
	self, target, err := validateAmoAdminAssertion("101", valid)
	if err != nil || self != "101" || !target.IsAdmin || !target.IsActive {
		t.Fatalf("self=%q target=%#v err=%v", self, target, err)
	}
	for _, test := range []struct {
		name  string
		self  string
		users []AmoAdminUserAssertion
	}{
		{name: "invalid self", self: "user", users: valid},
		{name: "missing self", self: "103", users: valid},
		{name: "duplicate", self: "101", users: append(valid, AmoAdminUserAssertion{ID: "101"})},
		{name: "invalid user", self: "101", users: []AmoAdminUserAssertion{{ID: "01"}}},
		{name: "empty", self: "101"},
		{name: "too many", self: "101", users: make([]AmoAdminUserAssertion, maxAmoAdminAssertionUsers+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := validateAmoAdminAssertion(test.self, test.users); err == nil {
				t.Fatal("invalid assertion accepted")
			}
		})
	}
}

func TestAmoAdminSelfLoginFailsClosedWhenForcedImportFails(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	companyID := uuid.New()
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	expectAmoSessionIntegration(mock, companyID, "active", now)
	expectAmoSessionCompany(mock, companyID, "active", now)
	expectAmoSessionCompany(mock, companyID, "active", now)
	service := &Service{
		pool: mock, now: func() time.Time { return now },
		externalUsers: failingExternalEmployees{err: errors.New("ssd unavailable")},
		amoSyncTTL:    time.Hour,
		amoSyncStates: map[uuid.UUID]*amoSyncState{
			companyID: {lastAttempt: now},
		},
	}
	_, err = service.AmoAdminSelfLogin(context.Background(), AmoAdminSelfLoginInput{
		AmoAccountID: "31355990", SelfUserID: "101",
		Users: []AmoAdminUserAssertion{{ID: "101", IsAdmin: true, IsActive: true}},
	})
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorUpstream ||
		applicationErr.Code != ErrorCodeAmoAdminSelfLoginUnavailable {
		t.Fatalf("error=%v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAmoAdminSelfLoginRequiresConfiguredImporter(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	companyID := uuid.New()
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	expectAmoSessionIntegration(mock, companyID, "active", now)
	expectAmoSessionCompany(mock, companyID, "active", now)
	_, err = (&Service{pool: mock}).AmoAdminSelfLogin(context.Background(), AmoAdminSelfLoginInput{
		AmoAccountID: "31355990", SelfUserID: "101",
		Users: []AmoAdminUserAssertion{{ID: "101", IsAdmin: true, IsActive: true}},
	})
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorUpstream {
		t.Fatalf("error=%v", err)
	}
}
