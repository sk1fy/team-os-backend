package eventbus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/sk1fy/team-os-backend/pkg/httpx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	messageIDHeader       = "Nats-Msg-Id"
	originalSubjectHeader = "Teamos-Original-Subject"
)

// Publisher is implemented by Bus and can be replaced by a test double in
// application services.
type Publisher interface {
	Publish(context.Context, string, Event, ...PublishOption) error
}

type publishOptions struct {
	headers map[string]string
}

// PublishOption customizes event headers without exposing NATS types to domain
// and application packages.
type PublishOption func(*publishOptions)

// WithHeader attaches non-sensitive metadata such as request correlation IDs.
func WithHeader(name, value string) PublishOption {
	return func(options *publishOptions) {
		if options.headers == nil {
			options.headers = make(map[string]string)
		}
		options.headers[name] = value
	}
}

// Bus is a thin JetStream adapter. Streams themselves are provisioned by
// deployment code, not implicitly by application processes.
type Bus struct {
	connection *nats.Conn
	jetStream  nats.JetStreamContext
}

// Connect opens a NATS connection and initializes a JetStream context.
func Connect(url string, options ...nats.Option) (*Bus, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("NATS URL is required")
	}
	connection, err := nats.Connect(url, options...)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	bus, err := New(connection)
	if err != nil {
		connection.Close()
		return nil, err
	}
	return bus, nil
}

// New creates a bus from an existing connection.
func New(connection *nats.Conn) (*Bus, error) {
	if connection == nil {
		return nil, errors.New("NATS connection is required")
	}
	jetStream, err := connection.JetStream()
	if err != nil {
		return nil, fmt.Errorf("initialize JetStream: %w", err)
	}
	return &Bus{connection: connection, jetStream: jetStream}, nil
}

// Ready verifies both the NATS connection and JetStream API. A reconnecting
// client is considered not ready so traffic moves to another service replica.
func (b *Bus) Ready(ctx context.Context) error {
	if ctx == nil {
		return errors.New("readiness context is required")
	}
	if b == nil || b.connection == nil || b.jetStream == nil {
		return errors.New("event bus is not initialized")
	}
	if !b.connection.IsConnected() {
		return fmt.Errorf("NATS connection is not ready: %s", b.connection.Status())
	}
	if err := b.connection.FlushWithContext(ctx); err != nil {
		return fmt.Errorf("flush NATS connection: %w", err)
	}
	if _, err := b.jetStream.AccountInfo(nats.Context(ctx)); err != nil {
		return fmt.Errorf("query JetStream account: %w", err)
	}
	return nil
}

// Publish persists an event in JetStream. Nats-Msg-Id enables server-side
// de-duplication within the stream's configured duplicate window.
func (b *Bus) Publish(ctx context.Context, subject string, event Event, options ...PublishOption) error {
	if b == nil || b.jetStream == nil {
		return errors.New("event bus is not initialized")
	}
	if err := ValidateSubject(subject); err != nil {
		return err
	}
	data, err := EncodeEvent(event)
	if err != nil {
		return err
	}

	configuration := publishOptions{headers: make(map[string]string)}
	for _, option := range options {
		if option != nil {
			option(&configuration)
		}
	}

	message := &nats.Msg{Subject: subject, Header: nats.Header{}, Data: data}
	for name, value := range configuration.headers {
		message.Header.Set(name, value)
	}
	message.Header.Set(messageIDHeader, event.EventID)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(message.Header))

	if _, err := b.jetStream.PublishMsg(message, nats.Context(ctx)); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// Drain flushes pending messages and closes the connection gracefully.
func (b *Bus) Drain() error {
	if b == nil || b.connection == nil || b.connection.IsClosed() {
		return nil
	}
	if err := b.connection.Drain(); err != nil {
		return fmt.Errorf("drain NATS connection: %w", err)
	}
	return nil
}

// IdempotentHandler owns the database transaction that atomically checks
// processed_events, applies the event and records its ID. A duplicate returns
// handled=false, err=nil and is acknowledged normally.
type IdempotentHandler interface {
	HandleOnce(context.Context, Event) (handled bool, err error)
}

// HandlerFunc adapts a function to IdempotentHandler.
type HandlerFunc func(context.Context, Event) (bool, error)

// HandleOnce implements IdempotentHandler.
func (handler HandlerFunc) HandleOnce(ctx context.Context, event Event) (bool, error) {
	return handler(ctx, event)
}

// ConsumerConfig defines a durable, horizontally scalable queue subscription.
type ConsumerConfig struct {
	Subject    string
	Stream     string
	Durable    string
	Queue      string
	DLQSubject string
	AckWait    time.Duration
	NakDelay   time.Duration
	MaxDeliver int
	OnError    func(error)
}

// Subscribe starts a durable queue consumer. Invalid envelopes are terminated;
// handler failures are retried and optionally moved to a DLQ at MaxDeliver.
func (b *Bus) Subscribe(ctx context.Context, config ConsumerConfig, handler IdempotentHandler) (*nats.Subscription, error) {
	if b == nil || b.jetStream == nil {
		return nil, errors.New("event bus is not initialized")
	}
	if handler == nil {
		return nil, errors.New("event handler is required")
	}
	if err := validateConsumerConfig(&config); err != nil {
		return nil, err
	}

	options := []nats.SubOpt{
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.Durable(config.Durable),
		nats.DeliverAll(),
		nats.MaxDeliver(config.MaxDeliver),
	}
	if config.AckWait > 0 {
		options = append(options, nats.AckWait(config.AckWait))
	}
	if config.Stream != "" {
		options = append(options, nats.BindStream(config.Stream))
	}

	subscription, err := b.jetStream.QueueSubscribe(config.Subject, config.Queue, func(message *nats.Msg) {
		b.consumeMessage(ctx, config, handler, message)
	}, options...)
	if err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", config.Subject, err)
	}

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			b.updateConsumerMetrics(subscription, config)
			select {
			case <-ctx.Done():
				if err := subscription.Drain(); err != nil {
					reportConsumerError(config, fmt.Errorf("drain subscription: %w", err))
				}
				return
			case <-ticker.C:
			}
		}
	}()

	return subscription, nil
}

func (b *Bus) updateConsumerMetrics(subscription *nats.Subscription, config ConsumerConfig) {
	info, err := subscription.ConsumerInfo()
	if err != nil {
		return
	}
	consumer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(config.Durable)
	httpx.SetGauge("teamos_consumer_lag_messages", `consumer="`+consumer+`"`, float64(info.NumPending))
}

func (b *Bus) consumeMessage(ctx context.Context, config ConsumerConfig, handler IdempotentHandler, message *nats.Msg) {
	event, err := DecodeEvent(message.Data)
	if err != nil {
		reportConsumerError(config, err)
		if config.DLQSubject != "" {
			if dlqErr := b.moveToDLQ(ctx, config.DLQSubject, message); dlqErr == nil {
				if ackErr := message.Ack(); ackErr != nil {
					reportConsumerError(config, fmt.Errorf("ack invalid DLQ event: %w", ackErr))
				}
				return
			} else {
				reportConsumerError(config, dlqErr)
			}
		}
		if ackErr := message.Term(); ackErr != nil {
			reportConsumerError(config, fmt.Errorf("terminate invalid event: %w", ackErr))
		}
		return
	}

	handlerContext := otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(message.Header))
	_, err = handler.HandleOnce(handlerContext, event)
	if err == nil {
		if ackErr := message.Ack(); ackErr != nil {
			reportConsumerError(config, fmt.Errorf("ack event %s: %w", event.EventID, ackErr))
		}
		return
	}

	reportConsumerError(config, fmt.Errorf("handle event %s: %w", event.EventID, err))
	if b.shouldMoveToDLQ(config, message) {
		if dlqErr := b.moveToDLQ(ctx, config.DLQSubject, message); dlqErr == nil {
			if ackErr := message.Ack(); ackErr != nil {
				reportConsumerError(config, fmt.Errorf("ack DLQ event %s: %w", event.EventID, ackErr))
			}
			return
		} else {
			reportConsumerError(config, dlqErr)
		}
	}

	var ackErr error
	if config.NakDelay > 0 {
		ackErr = message.NakWithDelay(config.NakDelay)
	} else {
		ackErr = message.Nak()
	}
	if ackErr != nil {
		reportConsumerError(config, fmt.Errorf("nak event %s: %w", event.EventID, ackErr))
	}
}

func validateConsumerConfig(config *ConsumerConfig) error {
	if err := ValidateFilterSubject(config.Subject); err != nil {
		return err
	}
	if strings.TrimSpace(config.Durable) == "" {
		return errors.New("durable consumer name is required")
	}
	if strings.ContainsAny(config.Durable, ". \t\r\n") {
		return fmt.Errorf("invalid durable consumer name %q", config.Durable)
	}
	if config.Queue == "" {
		config.Queue = config.Durable
	}
	if config.MaxDeliver <= 0 {
		config.MaxDeliver = 5
	}
	if config.DLQSubject != "" && !isConcreteNATSSubject(config.DLQSubject) {
		return fmt.Errorf("invalid DLQ subject %q", config.DLQSubject)
	}
	return nil
}

func (b *Bus) shouldMoveToDLQ(config ConsumerConfig, message *nats.Msg) bool {
	if config.DLQSubject == "" {
		return false
	}
	metadata, err := message.Metadata()
	return err == nil && metadata.NumDelivered >= uint64(config.MaxDeliver)
}

func (b *Bus) moveToDLQ(ctx context.Context, subject string, original *nats.Msg) error {
	header := make(nats.Header, len(original.Header)+1)
	for name, values := range original.Header {
		header[name] = append([]string(nil), values...)
	}
	header.Set(originalSubjectHeader, original.Subject)
	if originalID := header.Get(messageIDHeader); originalID != "" {
		header.Set(messageIDHeader, originalID+"-dlq")
	}
	message := &nats.Msg{Subject: subject, Header: header, Data: original.Data}
	if _, err := b.jetStream.PublishMsg(message, nats.Context(ctx)); err != nil {
		return fmt.Errorf("publish event to DLQ %s: %w", subject, err)
	}
	label := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(subject)
	httpx.AddGauge("teamos_dlq_messages", `subject="`+label+`"`, 1)
	return nil
}

func reportConsumerError(config ConsumerConfig, err error) {
	if config.OnError != nil {
		config.OnError(err)
	}
}

func isConcreteNATSSubject(subject string) bool {
	if strings.TrimSpace(subject) != subject || subject == "" || strings.ContainsAny(subject, " *>\t\r\n") {
		return false
	}
	for _, token := range strings.Split(subject, ".") {
		if token == "" {
			return false
		}
	}
	return true
}
