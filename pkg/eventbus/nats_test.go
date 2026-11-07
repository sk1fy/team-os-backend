package eventbus

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestPublishWithoutAckHasDeadline(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = conn.Write([]byte("INFO {\"server_id\":\"silent\",\"version\":\"2.10.0\",\"proto\":1,\"headers\":true,\"max_payload\":1048576}\r\n"))
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					if scanner.Text() == "PING" {
						_, _ = conn.Write([]byte("PONG\r\n"))
					}
				}
			}()
		}
	}()
	bus, err := Connect("nats://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bus.connection.Close)
	event, err := NewEvent("00000000-0000-4000-8000-000000000001", "", map[string]string{"title": "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- bus.Publish(ctx, "teamos.tasks.task.assigned.v1", event) }()
	go func() {
		done <- bus.moveToDLQ(ctx, "teamos.dlq.tasks.consumer.v1", &nats.Msg{Subject: "teamos.tasks.task.assigned.v1", Data: []byte("invalid event")})
	}()
	for range 2 {
		select {
		case err = <-done:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("missing ack must time out, got %v", err)
			}
		case <-time.After(6 * time.Second):
			t.Fatal("publication or dead-letter forwarding without ack did not release the caller within 6 seconds")
		}
	}
	short, shortCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer shortCancel()
	if err = bus.Publish(short, "teamos.dlq.tasks.publisher.v1", event); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline must override the default publication timeout, got %v", err)
	}
}

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
