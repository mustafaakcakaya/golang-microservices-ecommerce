package messaging

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Envelope is an integration event as it is stored and transported.
//
// It is broker-agnostic on purpose: the same value is written to the outbox,
// read back by the worker and handed to whichever publisher is configured. The
// payload stays as bytes the whole way, so the publisher never has to re-encode
// what the writer already encoded - a round trip through a Go struct is a
// chance to lose a field the current build no longer knows about.
type Envelope struct {
	// ID is the event id, carried to the broker as the message id.
	ID uuid.UUID

	// Contract says how Payload should be read.
	Contract Contract

	// AggregateID groups the events of one aggregate. Brokers that partition
	// use it as the key, which is what keeps an aggregate's events in order.
	AggregateID string

	CorrelationID string
	OccurredOnUTC time.Time

	// Payload is the JSON-encoded contract; never a domain or storage type.
	Payload []byte
}

// NewEnvelope encodes an event for storage.
func NewEnvelope(event Event, aggregateID string) (Envelope, error) {
	if aggregateID == "" {
		return Envelope{}, fmt.Errorf("integration event %s has no aggregate id", event.Contract())
	}

	payload, err := Marshal(event)
	if err != nil {
		return Envelope{}, err
	}

	meta := event.Metadata()
	if meta.ID == uuid.Nil {
		return Envelope{}, fmt.Errorf("integration event %s has no id", event.Contract())
	}

	return Envelope{
		ID:            meta.ID,
		Contract:      event.Contract(),
		AggregateID:   aggregateID,
		CorrelationID: meta.CorrelationID,
		OccurredOnUTC: meta.OccurredOnUTC,
		Payload:       payload,
	}, nil
}

// Marshal encodes an event into its stored payload.
//
// The writer and every reader go through this one function, so a change to how
// events are encoded cannot be made on one side only.
func Marshal(event Event) ([]byte, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", event.Contract(), err)
	}

	return payload, nil
}

// Unmarshal decodes a stored payload into the contract it was written from.
func Unmarshal(payload []byte, event Event) error {
	if err := json.Unmarshal(payload, event); err != nil {
		return fmt.Errorf("decoding %s: %w", event.Contract(), err)
	}

	return nil
}
