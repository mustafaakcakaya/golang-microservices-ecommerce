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
// it with the broker to be delivered again rather than acknowledging it. An
// error wrapped with Reject is the exception - see there.
type Delivery func(ctx context.Context, envelope Envelope) error

// rejection marks a message that can never be handled.
type rejection struct {
	err error
}

func (r *rejection) Error() string { return "message rejected: " + r.err.Error() }

func (r *rejection) Unwrap() error { return r.err }

// Reject marks a message as one that will never succeed, however often it is
// delivered: its content contradicts itself, say, rather than a dependency
// being down.
//
// Retrying such a message forever would hold up everything queued behind it,
// so the subscriber sets it aside instead - where the broker can keep it, in a
// queue of its own a person can inspect - and moves on. Use it only for
// failures that are a property of the message; a transient failure returned
// this way is a lost message.
func Reject(err error) error {
	if err == nil {
		return nil
	}

	return &rejection{err: err}
}

// IsRejected reports whether err, or anything it wraps, came from Reject.
func IsRejected(err error) bool {
	var rejected *rejection
	return errors.As(err, &rejected)
}

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
