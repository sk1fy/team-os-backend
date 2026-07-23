package consumers

import (
	"strings"
	"testing"

	"github.com/sk1fy/team-os-backend/pkg/eventbus"
)

func TestDurableNamesAreValidAndUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(subjects))
	for _, subject := range subjects {
		if err := eventbus.ValidateSubject(subject); err != nil {
			t.Errorf("subject %q не соответствует схеме TeamOS: %v", subject, err)
		}
		name := durableName(subject)
		if strings.ContainsAny(name, ". \t\r\n") {
			t.Errorf("durableName(%q) = %q: имя содержит запрещённые символы", subject, name)
		}
		if _, exists := seen[name]; exists {
			t.Errorf("durableName(%q) = %q: имя не уникально", subject, name)
		}
		seen[name] = struct{}{}
	}
}

func TestDurableName(t *testing.T) {
	const subject = "teamos.org.user.created.v1"
	const want = "notifications-org-user-created-v1"
	if got := durableName(subject); got != want {
		t.Fatalf("durableName(%q) = %q, ожидалось %q", subject, got, want)
	}
}

func TestVerificationEmailSubjectHasDurableConsumer(t *testing.T) {
	const want = "teamos.academy.external_email_verification.requested.v1"
	for _, subject := range subjects {
		if subject == want {
			return
		}
	}
	t.Fatalf("для %q не зарегистрирован durable consumer", want)
}
