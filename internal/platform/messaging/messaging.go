// Package messaging defines what leaves a service.
//
// An integration event is an explicit, versioned contract between services. It
// is deliberately not a domain event and not a persistence entity: those change
// whenever the code inside a service changes, and publishing them would make
// every internal refactor a breaking change for everyone else.
//
// Two rules hold for everything in this package:
//
// Payloads carry only what a consumer needs. Card numbers, security codes and
// anything else a consumer has no business knowing are never mapped into a
// contract, so they cannot reach the outbox, the broker or a log line.
//
// Contracts are versioned rather than edited. A stored message outlives the
// build that wrote it, so the (event type, version) pair travels with every
// message and a reader that does not know the pair refuses it instead of
// guessing.
package messaging

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Contract is the wire identity of an integration event.
//
// It is what a stored message is resolved by, so neither field may change
// meaning: renaming a Go type is free, renaming an EventType is a breaking
// change for every consumer and every message already in the outbox.
type Contract struct {
	EventType string
	Version   int
}

// String renders the contract for logs and errors.
func (c Contract) String() string {
	return fmt.Sprintf("%s v%d", c.EventType, c.Version)
}

// Header carries the fields every integration event has.
//
// It is embedded in each contract, which both gives the contract its metadata
// and satisfies the Metadata method of Event.
type Header struct {
	// ID is generated once, when the domain event is mapped, and travels all
	// the way to the broker as the message id. Delivery is at-least-once, so
	// this is what a consumer deduplicates on.
	ID uuid.UUID `json:"id"`

	OccurredOnUTC time.Time `json:"occurredOnUtc"`

	CorrelationID string `json:"correlationId,omitempty"`
}

// NewHeader builds the metadata of a freshly mapped event.
func NewHeader(correlationID string) Header {
	return Header{
		ID:            uuid.New(),
		OccurredOnUTC: time.Now().UTC(),
		CorrelationID: correlationID,
	}
}

// Metadata implements the metadata half of Event for every contract that
// embeds Header.
func (h Header) Metadata() Header { return h }

// Event is implemented by every integration event contract.
type Event interface {
	// Contract returns the wire identity of this event.
	Contract() Contract

	// Metadata returns the id, timestamp and correlation id.
	Metadata() Header
}
