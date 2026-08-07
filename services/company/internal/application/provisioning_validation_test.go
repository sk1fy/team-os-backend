package application

import (
	"bytes"
	"testing"
)

func TestNormalizeProvisionCompanyInput(t *testing.T) {
	base := ProvisionCompanyInput{
		Provider: "rakurs", ExternalAccountID: "31355990", CompanyName: " Компания ",
		InitiatorExternalUserID: "owner-1", IdempotencyKey: "request-1",
		Owner: ProvisioningParticipantInput{
			ExternalUserID: "owner-1", Email: "OWNER@example.com", FirstName: " Иван ", LastName: " Иванов ",
		},
		Admin: ProvisioningParticipantInput{
			ExternalUserID: "admin-1", Email: "admin@example.com", FirstName: "Анна", LastName: "Петрова",
		},
	}
	normalized, firstHash, err := normalizeProvisionCompanyInput(base)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.CompanyName != "Компания" || normalized.Owner.Email != "owner@example.com" ||
		normalized.Owner.FirstName != "Иван" {
		t.Fatalf("некорректная нормализация: %#v", normalized)
	}
	_, secondHash, err := normalizeProvisionCompanyInput(base)
	if err != nil || !bytes.Equal(firstHash, secondHash) {
		t.Fatalf("хеш запроса нестабилен: %x %x, %v", firstHash, secondHash, err)
	}
}

func TestNormalizeProvisionCompanyInputRejectsRoleConfusion(t *testing.T) {
	base := ProvisionCompanyInput{
		Provider: "rakurs", ExternalAccountID: "account", CompanyName: "Компания",
		InitiatorExternalUserID: "outsider", IdempotencyKey: "request",
		Owner: ProvisioningParticipantInput{ExternalUserID: "same", Email: "owner@example.com", FirstName: "Иван", LastName: "Иванов"},
		Admin: ProvisioningParticipantInput{ExternalUserID: "same", Email: "admin@example.com", FirstName: "Анна", LastName: "Петрова"},
	}
	if _, _, err := normalizeProvisionCompanyInput(base); !isCompanyError(err, ErrorValidation) {
		t.Fatalf("один сотрудник принят в двух ролях: %v", err)
	}
	base.Admin.ExternalUserID = "admin"
	if _, _, err := normalizeProvisionCompanyInput(base); !isCompanyError(err, ErrorValidation) {
		t.Fatalf("посторонний инициатор принят: %v", err)
	}
}

func TestNormalizeProvisionedCompanyLookup(t *testing.T) {
	provider, accountID, err := normalizeProvisionedCompanyLookup(" rakurs ", " 31355990 ")
	if err != nil || provider != "rakurs" || accountID != "31355990" {
		t.Fatalf("provider=%q accountID=%q err=%v", provider, accountID, err)
	}
	if _, _, err = normalizeProvisionedCompanyLookup("Rakurs", "31355990"); !isCompanyError(err, ErrorValidation) {
		t.Fatalf("invalid provider error = %v", err)
	}
	if _, _, err = normalizeProvisionedCompanyLookup("rakurs", " "); !isCompanyError(err, ErrorValidation) {
		t.Fatalf("empty account error = %v", err)
	}
}
