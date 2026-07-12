package eventbus_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
)

func TestJetStreamPublishSubscribe(t *testing.T) {
	url := os.Getenv("EVENTBUS_TEST_NATS_URL")
	if url == "" {
		t.Skip("EVENTBUS_TEST_NATS_URL is not set")
	}

	connection, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect to NATS: %v", err)
	}
	t.Cleanup(connection.Close)

	jetStream, err := connection.JetStream()
	if err != nil {
		t.Fatalf("JetStream context: %v", err)
	}
	suffix := time.Now().UTC().UnixNano()
	stream := fmt.Sprintf("TEST_EVENTS_%d", suffix)
	subject := fmt.Sprintf("teamos.test.event.created_%d.v1", suffix)
	if _, addErr := jetStream.AddStream(&nats.StreamConfig{Name: stream, Subjects: []string{subject}}); addErr != nil {
		t.Fatalf("add stream: %v", addErr)
	}
	t.Cleanup(func() {
		if deleteErr := jetStream.DeleteStream(stream); deleteErr != nil {
			t.Errorf("delete stream: %v", deleteErr)
		}
	})

	bus, err := eventbus.New(connection)
	if err != nil {
		t.Fatalf("create bus: %v", err)
	}
	event, err := eventbus.NewEvent("company-1", "user-1", map[string]string{"userId": "user-2"})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	if readyErr := bus.Ready(ctx); readyErr != nil {
		t.Fatalf("bus readiness: %v", readyErr)
	}
	received := make(chan eventbus.Event, 1)
	durable := fmt.Sprintf("test-consumer-%d", suffix)
	_, err = bus.Subscribe(ctx, eventbus.ConsumerConfig{
		Subject:    subject,
		Stream:     stream,
		Durable:    durable,
		Queue:      durable,
		MaxDeliver: 2,
	}, eventbus.HandlerFunc(func(_ context.Context, incoming eventbus.Event) (bool, error) {
		received <- incoming
		return true, nil
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Publish(ctx, subject, event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case incoming := <-received:
		if incoming.EventID != event.EventID {
			t.Fatalf("event ID = %q, want %q", incoming.EventID, event.EventID)
		}
	case <-ctx.Done():
		t.Fatalf("wait for event: %v", ctx.Err())
	}
}
