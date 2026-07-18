package eventbus

import (
	"strings"
	"testing"
)

func TestValidateConsumerConfigRejectsInvalidDurableName(t *testing.T) {
	tests := []string{"notifications-org.user", "notifications org-user", "notifications\torg-user"}
	for _, durable := range tests {
		t.Run(durable, func(t *testing.T) {
			config := ConsumerConfig{Subject: "teamos.org.user.created.v1", Durable: durable}
			err := validateConsumerConfig(&config)
			if err == nil || !strings.Contains(err.Error(), "invalid durable consumer name") {
				t.Fatalf("validateConsumerConfig() error = %v", err)
			}
		})
	}
}

func TestValidateConsumerConfigAcceptsValidDurableName(t *testing.T) {
	config := ConsumerConfig{
		Subject: "teamos.org.user.created.v1",
		Durable: "notifications-org-user-created-v1",
	}
	if err := validateConsumerConfig(&config); err != nil {
		t.Fatalf("validateConsumerConfig() error = %v", err)
	}
	if config.Queue != config.Durable {
		t.Fatalf("queue = %q, ожидалось %q", config.Queue, config.Durable)
	}
}
