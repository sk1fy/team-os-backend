package application

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestAmoCompanyEntryRejectsMalformedSubjectBeforeStorage(t *testing.T) {
	subject := AmoEntrySubject{AccountID: "123", ActorID: "7", IntegrationID: uuid.NewString(), InstallationID: uuid.NewString()}
	for _, invalid := range []string{"", "0", "01", "+1", " 1", "1.0", "9223372036854775808"} {
		changed := subject
		changed.AccountID = invalid
		if validateAmoEntrySubject(changed) == nil {
			t.Fatalf("accepted account %q", invalid)
		}
		changed = subject
		changed.ActorID = invalid
		if validateAmoEntrySubject(changed) == nil {
			t.Fatalf("accepted actor %q", invalid)
		}
	}
	if validateAmoEntrySubject(subject) != nil {
		t.Fatal("canonical subject denied")
	}
	service := &Service{}
	_, err := service.ProvisionAmoWidgetEntry(context.Background(), AmoWidgetEntryInput{AmoEntrySubject: subject, Intent: "owner"})
	if err == nil {
		t.Fatal("accepted caller role as intent")
	}
	_, err = service.ProvisionAmoWidgetEntry(context.Background(), AmoWidgetEntryInput{AmoEntrySubject: subject, Intent: "create"})
	if err == nil {
		t.Fatal("unconfigured callback did not disable provisioning")
	}
}
