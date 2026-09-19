package messaging

import "context"

// Delivery is what a handler does with one received message.
//
// Returning an error means the message was not handled: the subscriber leaves
// it with the broker to be delivered again rather than acknowledging it.
type Delivery func(ctx context.Context, envelope Envelope) error

// Subscriber receives messages from a broker and hands them to a Delivery.
//
// It is the reading counterpart of Publisher and the only broker-aware seam on
// the consuming side, so a service that consumes messages does not know which
// broker it is consuming from.
type Subscriber interface {
	// Subscribe delivers messages until the context is cancelled, which is a
	// clean stop rather than a failure.
	//
	// A message is acknowledged only after the delivery returns without error,
	// so a consumer that crashes mid-work sees the message again. Delivery is
	// at-least-once on this side too, and the inbox is what makes the effect
	// happen once.
	Subscribe(ctx context.Context, deliver Delivery) error

	// Close releases the connection.
	Close() error
}
