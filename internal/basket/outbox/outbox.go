// Package outbox publishes the checkouts Basket has recorded.
//
// Basket has no change data capture to read from - its database is PostgreSQL -
// so the relay polls the table instead. The guarantee is the same as Ordering's:
// the message is written in the transaction that produced it, and a separate
// process is responsible for getting it to the broker.
package outbox

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// Message is one recorded checkout and how often publishing it has failed.
type Message struct {
	Envelope messaging.Envelope
	Attempts int
}

// Store is the recorded checkouts.
type Store interface {
	// Due returns messages ready to publish, oldest first.
	Due(ctx context.Context, limit int, now time.Time) ([]Message, error)

	// Delete removes a message that has been published.
	Delete(ctx context.Context, id uuid.UUID) error

	// Fail records a failed attempt and when to try again.
	Fail(ctx context.Context, id uuid.UUID, reason string, nextAttempt time.Time) error

	// Poison gives up on a message.
	Poison(ctx context.Context, id uuid.UUID, reason string) error
}

// Options tune the relay.
type Options struct {
	BatchSize      int
	PollInterval   time.Duration
	MaxAttempts    int
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
}

// DefaultOptions are what the relay runs with unless configured otherwise.
func DefaultOptions() Options {
	return Options{
		BatchSize:      50,
		PollInterval:   2 * time.Second,
		MaxAttempts:    10,
		RetryBaseDelay: time.Second,
		RetryMaxDelay:  time.Minute,
	}
}

// Relay publishes what the outbox holds.
type Relay struct {
	store     Store
	publisher messaging.Publisher
	options   Options
	log       *slog.Logger
}

// NewRelay wires the relay.
func NewRelay(store Store, publisher messaging.Publisher, options Options, log *slog.Logger) *Relay {
	return &Relay{store: store, publisher: publisher, options: options, log: log}
}

// Run polls until the context is cancelled, which is a clean stop.
func (r *Relay) Run(ctx context.Context) error {
	r.log.InfoContext(ctx, "checkout relay started")

	for {
		published, err := r.ProcessOnce(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "a checkout relay cycle failed", "error", err)
		}

		// Drain while there is work, then go back to waiting.
		delay := r.options.PollInterval
		if published > 0 {
			delay = 0
		}

		select {
		case <-ctx.Done():
			r.log.InfoContext(ctx, "checkout relay stopped")
			return nil
		case <-time.After(delay):
		}
	}
}

// ProcessOnce publishes one batch and reports how many went out.
//
// A message is removed only after the broker has accepted it. A crash in
// between leaves it in the table and it goes out again under the same id,
// which the consumer's inbox recognises - at-least-once, on purpose, because a
// repeat can be recognised and a loss cannot be recovered.
func (r *Relay) ProcessOnce(ctx context.Context) (int, error) {
	messages, err := r.store.Due(ctx, r.options.BatchSize, time.Now())
	if err != nil {
		return 0, err
	}

	published := 0
	for _, message := range messages {
		// Shutting down mid-batch is a clean stop: what is left is still in
		// the table and goes out on the next run.
		if stopped(ctx) {
			return published, nil
		}

		sent, err := r.publish(ctx, message)
		if err != nil {
			return published, err
		}
		// Only a message the broker actually took counts. Counting a failure
		// would both misreport the cycle and make the relay think there is
		// more work waiting rather than backing off.
		if sent {
			published++
		}
	}

	if published > 0 {
		r.log.InfoContext(ctx, "checkout relay published messages", "count", published)
	}

	return published, nil
}

// stopped reports whether the relay has been asked to shut down.
func stopped(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// publish sends one message and records what happened to it, reporting
// whether the broker took it.
func (r *Relay) publish(ctx context.Context, message Message) (bool, error) {
	err := r.publisher.Publish(ctx, message.Envelope)
	if err == nil {
		return true, r.store.Delete(ctx, message.Envelope.ID)
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, err
	}

	attempts := message.Attempts + 1
	if attempts >= r.options.MaxAttempts {
		// Giving up is the lesser harm: one message nobody can publish must
		// not hold up every checkout behind it. The row stays as the record of
		// what was skipped and why.
		r.log.ErrorContext(ctx, "giving up on a checkout",
			"messageId", message.Envelope.ID,
			"contract", message.Envelope.Contract.String(),
			"attempts", attempts,
			"error", err)

		return false, r.store.Poison(ctx, message.Envelope.ID, err.Error())
	}

	r.log.WarnContext(ctx, "publishing a checkout failed",
		"messageId", message.Envelope.ID,
		"attempts", attempts,
		"maxAttempts", r.options.MaxAttempts,
		"error", err)

	return false, r.store.Fail(ctx, message.Envelope.ID, err.Error(), time.Now().Add(r.backoff(attempts)))
}

// backoff grows the wait with each failed attempt, up to the ceiling.
func (r *Relay) backoff(attempts int) time.Duration {
	const maxExponent = 10

	exponent := min(float64(attempts-1), maxExponent)
	delay := min(float64(r.options.RetryBaseDelay)*math.Pow(2, exponent), float64(r.options.RetryMaxDelay))

	// Jitter, so several instances that fail together do not return together.
	delay += delay * 0.2 * rand.Float64() //nolint:gosec // scheduling jitter, not a secret

	return time.Duration(delay)
}
