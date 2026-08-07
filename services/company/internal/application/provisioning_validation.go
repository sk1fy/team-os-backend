package application

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
)

func normalizeProvisionCompanyInput(input ProvisionCompanyInput) (ProvisionCompanyInput, []byte, error) {
	var err error
	input.Provider, err = normalizeExternalIdentifier(input.Provider, 32, "Укажите провайдера")
	if err != nil || !validProvider(input.Provider) {
		return ProvisionCompanyInput{}, nil, validation("Некорректный провайдер")
	}
	input.ExternalAccountID, err = normalizeExternalIdentifier(input.ExternalAccountID, 255, "Укажите внешний аккаунт")
	if err != nil {
		return ProvisionCompanyInput{}, nil, err
	}
	input.CompanyName, err = requiredText(input.CompanyName, "Укажите название компании")
	if err != nil {
		return ProvisionCompanyInput{}, nil, err
	}
	input.InitiatorExternalUserID, err = normalizeExternalIdentifier(input.InitiatorExternalUserID, 255, "Укажите инициатора")
	if err != nil {
		return ProvisionCompanyInput{}, nil, err
	}
	input.IdempotencyKey, err = normalizeExternalIdentifier(input.IdempotencyKey, 255, "Укажите ключ идемпотентности")
	if err != nil {
		return ProvisionCompanyInput{}, nil, err
	}
	input.Owner, err = normalizeProvisioningParticipant(input.Owner)
	if err != nil {
		return ProvisionCompanyInput{}, nil, err
	}
	input.Admin, err = normalizeProvisioningParticipant(input.Admin)
	if err != nil {
		return ProvisionCompanyInput{}, nil, err
	}
	if input.Owner.ExternalUserID == input.Admin.ExternalUserID {
		return ProvisionCompanyInput{}, nil, validation("Владелец и администратор должны быть разными сотрудниками")
	}
	if input.Owner.Email == input.Admin.Email {
		return ProvisionCompanyInput{}, nil, validation("Владелец и администратор должны иметь разные email")
	}
	if input.InitiatorExternalUserID != input.Owner.ExternalUserID &&
		input.InitiatorExternalUserID != input.Admin.ExternalUserID {
		return ProvisionCompanyInput{}, nil, validation("Инициатор должен быть выбран владельцем или администратором")
	}

	payload, marshalErr := json.Marshal(struct {
		Provider                string
		ExternalAccountID       string
		CompanyName             string
		InitiatorExternalUserID string
		Owner                   ProvisioningParticipantInput
		Admin                   ProvisioningParticipantInput
	}{
		Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
		CompanyName: input.CompanyName, InitiatorExternalUserID: input.InitiatorExternalUserID,
		Owner: input.Owner, Admin: input.Admin,
	})
	if marshalErr != nil {
		return ProvisionCompanyInput{}, nil, internal("Не удалось подготовить provisioning-запрос", marshalErr)
	}
	hash := sha256.Sum256(payload)
	return input, hash[:], nil
}

func normalizeProvisioningParticipant(input ProvisioningParticipantInput) (ProvisioningParticipantInput, error) {
	var err error
	input.ExternalUserID, err = normalizeExternalIdentifier(input.ExternalUserID, 255, "Укажите внешний ID сотрудника")
	if err != nil {
		return ProvisioningParticipantInput{}, err
	}
	input.Email, err = normalizeEmail(input.Email)
	if err != nil {
		return ProvisioningParticipantInput{}, err
	}
	input.FirstName, err = requiredText(input.FirstName, "Укажите имя сотрудника")
	if err != nil {
		return ProvisioningParticipantInput{}, err
	}
	input.LastName, err = requiredText(input.LastName, "Укажите фамилию сотрудника")
	if err != nil {
		return ProvisioningParticipantInput{}, err
	}
	return input, nil
}

func normalizeProvisionedCompanyLookup(provider, externalAccountID string) (string, string, error) {
	provider, err := normalizeExternalIdentifier(provider, 32, "Укажите провайдера")
	if err != nil || !validProvider(provider) {
		return "", "", validation("Некорректный провайдер")
	}
	externalAccountID, err = normalizeExternalIdentifier(externalAccountID, 255, "Укажите внешний аккаунт")
	if err != nil {
		return "", "", err
	}
	return provider, externalAccountID, nil
}

func normalizeExternalIdentifier(value string, maximum int, emptyMessage string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", validation(emptyMessage)
	}
	if len(value) > maximum {
		return "", validation("Значение слишком длинное")
	}
	return value, nil
}

func validProvider(value string) bool {
	for index, character := range value {
		if index == 0 {
			if character < 'a' || character > 'z' {
				return false
			}
			continue
		}
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return len(value) >= 2
}
