// Package inbox makes a consumer safe to deliver to more than once.
//
// It is the other half of the outbox. Publishing is at-least-once by design:
// the worker sends a message and then records its position, so a crash between
// the two sends it again. That is the safe direction to fail - a repeated
// message can be recognised, a lost one cannot be recovered - and this package
// is where the recognising happens.
package inbox

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// Handler does something with a delivered message.
type Handler func(ctx context.Context, envelope messaging.Envelope) error

// Store records which messages a consumer has already handled.
type Store interface {
	// Begin claims a message for a consumer.
	//
	// It returns false only when this consumer already finished the message,
	// which makes the delivery a duplicate. A message that was claimed but
	// never finished - the consumer crashed part way - is claimed again:
	// running the work twice is recoverable, skipping it is not.
	Begin(ctx context.Context, messageID uuid.UUID, consumer string) (bool, error)

	// Complete marks the message as handled, so later deliveries are skipped.
	Complete(ctx context.Context, messageID uuid.UUID, consumer string) error
}

// Idempotent wraps a handler so that a message this consumer has already
// finished is acknowledged without doing the work again.
//
// One gap remains on purpose: the work and the completion are two steps, so a
// crash between them repeats the work. Closing it entirely means writing the
// business change and the completion in one transaction, which only the handler
// can do. A handler that cannot should be naturally repeatable.
func Idempotent(store Store, consumer string, log *slog.Logger, next Handler) Handler {
	return func(ctx context.Context, envelope messaging.Envelope) error {
		if envelope.ID == uuid.Nil {
			// Without an id there is nothing to deduplicate on, and quietly
			// processing it would make every retry a second order.
			return fmt.Errorf(
				"a %s message arrived without an id and cannot be deduplicated; "+
					"publishers must carry the event id as the message id", envelope.Contract)
		}

		claimed, err := store.Begin(ctx, envelope.ID, consumer)
		if err != nil {
			return err
		}
		if !claimed {
			log.InfoContext(ctx, "skipping a message this consumer already handled",
				"messageId", envelope.ID, "contract", envelope.Contract.String(), "consumer", consumer)
			return nil
		}

		if err := next(ctx, envelope); err != nil {
			// Deliberately not completed: the work did not happen, so the next
			// delivery has to run it again.
			return err
		}

		return store.Complete(ctx, envelope.ID, consumer)
	}
}
