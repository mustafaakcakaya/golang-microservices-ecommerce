package messaging

import "context"

// Publisher hands an envelope to a message broker.
//
// It is the only broker-aware seam in the publish path: reading the outbox,
// checkpointing and retrying all work against this interface, so moving between
// brokers changes one implementation and nothing else.
type Publisher interface {
	// Publish must not return until the broker has durably accepted the
	// message. Returning earlier would let the reader record progress past a
	// message the broker can still lose, which is the one failure the outbox
	// exists to rule out.
	Publish(ctx context.Context, envelope Envelope) error

	// Close releases the connection.
	Close() error
}
