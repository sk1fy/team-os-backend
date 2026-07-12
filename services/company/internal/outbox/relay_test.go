package outbox

import (
	"context"
	"testing"

	"github.com/sk1fy/team-os-backend/pkg/eventbus"
)

type publisherFunc func(context.Context, string, eventbus.Event, ...eventbus.PublishOption) error

func (function publisherFunc) Publish(ctx context.Context, subject string, event eventbus.Event, options ...eventbus.PublishOption) error {
	return function(ctx, subject, event, options...)
}

func TestNewRelayDefaultsLogger(t *testing.T) {
	relay := NewRelay(nil, publisherFunc(func(context.Context, string, eventbus.Event, ...eventbus.PublishOption) error { return nil }), nil)
	if relay.logger == nil || relay.pollInterval <= 0 {
		t.Fatal("relay defaults were not initialized")
	}
}
