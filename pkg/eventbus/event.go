// Package eventbus defines the stable TeamOS event envelope and a small NATS
// JetStream adapter. Domain payloads remain owned by their publishing service.
package eventbus

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidEvent indicates a malformed common event envelope.
	ErrInvalidEvent = errors.New("invalid event envelope")
	// ErrInvalidSubject indicates a subject outside the versioned TeamOS scheme.
	ErrInvalidSubject = errors.New("invalid event subject")
)

var (
	subjectPattern       = regexp.MustCompile(`^teamos\.[a-z][a-z0-9_-]*\.[a-z][a-z0-9_-]*\.[a-z][a-z0-9_-]*\.v[1-9][0-9]*$`)
	filterSubjectPattern = regexp.MustCompile(`^teamos\.(?:[a-z][a-z0-9_-]*|\*)\.(?:[a-z][a-z0-9_-]*|\*)\.(?:[a-z][a-z0-9_-]*|\*)\.v[1-9][0-9]*$`)
)

// Event is the common envelope stored in the outbox and published to NATS.
type Event struct {
	EventID    string          `json:"eventId"`
	OccurredAt time.Time       `json:"occurredAt"`
	CompanyID  string          `json:"companyId"`
	ActorID    string          `json:"actorId"`
	Payload    json.RawMessage `json:"payload"`
}

// NewEvent creates a validated event with a UUIDv4 identifier and UTC time.
func NewEvent(companyID, actorID string, payload any) (Event, error) {
	encodedPayload := json.RawMessage(`{}`)
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return Event{}, fmt.Errorf("encode event payload: %w", err)
		}
		encodedPayload = data
	}

	eventID, err := newEventID()
	if err != nil {
		return Event{}, err
	}
	event := Event{
		EventID:    eventID,
		OccurredAt: time.Now().UTC(),
		CompanyID:  strings.TrimSpace(companyID),
		ActorID:    strings.TrimSpace(actorID),
		Payload:    encodedPayload,
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

// Validate checks only common-envelope invariants. Payload schema validation is
// deliberately performed by the owning service's generated protobuf types.
func (e Event) Validate() error {
	switch {
	case strings.TrimSpace(e.EventID) == "":
		return fmt.Errorf("%w: eventId is required", ErrInvalidEvent)
	case e.OccurredAt.IsZero():
		return fmt.Errorf("%w: occurredAt is required", ErrInvalidEvent)
	case strings.TrimSpace(e.CompanyID) == "":
		return fmt.Errorf("%w: companyId is required", ErrInvalidEvent)
	case len(e.Payload) == 0 || !json.Valid(e.Payload):
		return fmt.Errorf("%w: payload must be valid JSON", ErrInvalidEvent)
	default:
		return nil
	}
}

// EncodeEvent serializes a validated common envelope.
func EncodeEvent(event Event) ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encode event: %w", err)
	}
	return data, nil
}

// DecodeEvent parses and validates a common envelope. Unknown additive fields
// are intentionally ignored for rolling compatibility.
func DecodeEvent(data []byte) (Event, error) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return Event{}, fmt.Errorf("%w: decode JSON: %w", ErrInvalidEvent, err)
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

// ValidateSubject enforces teamos.<service>.<entity>.<action>.vN.
func ValidateSubject(subject string) error {
	if !subjectPattern.MatchString(subject) {
		return fmt.Errorf("%w: %q", ErrInvalidSubject, subject)
	}
	return nil
}

// ValidateFilterSubject accepts the same versioned shape as ValidateSubject
// plus single-token NATS wildcards for durable consumer filters.
func ValidateFilterSubject(subject string) error {
	if !filterSubjectPattern.MatchString(subject) {
		return fmt.Errorf("%w: %q", ErrInvalidSubject, subject)
	}
	return nil
}

func newEventID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate event UUID: %w", err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
