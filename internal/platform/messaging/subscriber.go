package messaging

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

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

// stopped reports whether the service has been asked to shut down.
func stopped(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// resubscribe bounds how quickly a dropped subscription is reopened.
const (
	resubscribeBaseDelay = time.Second
	resubscribeMaxDelay  = 30 * time.Second
)

// KeepSubscribed reopens a subscription whenever it drops, until the context
// is cancelled.
//
// A broker that goes away is an outage, not a reason for a service to stop. A
// service that died with it would also stop answering the requests it can
// still serve - reads do not need the broker - and would need something
// outside to notice and restart it. The messages wait in the broker meanwhile.
func KeepSubscribed(ctx context.Context, subscriber Subscriber, deliver Delivery, log *slog.Logger) error {
	for attempt := 1; ; attempt++ {
		err := subscriber.Subscribe(ctx, deliver)

		// Checked before the error: a subscription that ended because the
		// service is shutting down is a clean stop.
		if stopped(ctx) {
			return nil
		}
		if err == nil {
			// A clean return without a cancelled context means the broker
			// closed the subscription; open it again.
			err = errors.New("the subscription ended")
		}

		delay := min(resubscribeBaseDelay<<min(attempt-1, 5), resubscribeMaxDelay)
		log.WarnContext(ctx, "the subscription dropped; reopening it",
			"error", err, "retryIn", delay)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
}
