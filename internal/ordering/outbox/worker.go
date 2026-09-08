package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// unhealthyAfter is how many cycles in a row may fail before the worker reports
// itself unhealthy. One failure is a broker hiccup; several in a row is
// something an operator should see.
const unhealthyAfter = 5

// Cycles is the work the worker drives, one batch at a time.
type Cycles interface {
	ProcessOnce(ctx context.Context) (Result, error)
}

// ReadinessWaiter blocks until the worker has something to read from.
type ReadinessWaiter interface {
	Wait(ctx context.Context) error
}

// Worker runs the processor on a loop.
//
// It is only the shell: waiting, backing off and shutting down. Everything that
// decides what happens to a message lives in the processor, which is why that
// can be tested without a clock - and the loop takes both of its collaborators
// as interfaces so it can be tested without a database.
type Worker struct {
	cycles    Cycles
	readiness ReadinessWaiter
	options   Options
	log       *slog.Logger

	consecutiveFailures atomic.Int64
}

// NewWorker wires the worker.
func NewWorker(cycles Cycles, readiness ReadinessWaiter, options Options, log *slog.Logger) *Worker {
	return &Worker{cycles: cycles, readiness: readiness, options: options, log: log}
}

// Run polls until the context is cancelled.
//
// A cancelled context is a clean stop, not a failure: the position is stored
// after every group, so stopping between cycles loses nothing.
func (w *Worker) Run(ctx context.Context) error {
	if err := w.waitUntilReady(ctx); err != nil {
		return err
	}
	if stopped(ctx) {
		// Shut down while still waiting for capture. Nothing had started, so
		// there is nothing to report.
		w.log.InfoContext(ctx, "outbox worker stopped")
		return nil
	}

	w.log.InfoContext(ctx, "outbox worker started", "consumer", w.options.ConsumerName)

	for {
		delay := w.cycle(ctx)

		if delay > 0 {
			select {
			case <-ctx.Done():
				w.log.InfoContext(ctx, "outbox worker stopped")
				return nil
			case <-time.After(delay):
			}
			continue
		}

		if stopped(ctx) {
			w.log.InfoContext(ctx, "outbox worker stopped")
			return nil
		}
	}
}

// stopped reports whether the worker has been asked to shut down.
func stopped(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// waitUntilReady blocks until capture is usable. A wait that ends because the
// worker is shutting down is not a failure, so only a real problem is returned.
func (w *Worker) waitUntilReady(ctx context.Context) error {
	if err := w.readiness.Wait(ctx); err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}

// Health reports whether the worker is getting its work done.
//
// A worker that cannot publish is still running and still answering, which is
// exactly the failure a liveness check misses; this is what makes it visible.
func (w *Worker) Health(context.Context) error {
	if failures := w.consecutiveFailures.Load(); failures >= unhealthyAfter {
		return fmt.Errorf("the last %d outbox cycles failed", failures)
	}

	return nil
}

// cycle runs the processor once and returns how long to wait afterwards.
func (w *Worker) cycle(ctx context.Context) time.Duration {
	result, err := w.cycles.ProcessOnce(ctx)

	switch {
	case err != nil && ctx.Err() != nil:
		return 0

	case err != nil:
		return w.backoffFor(ctx, err)

	case result.Status == StatusFaulted:
		w.consecutiveFailures.Add(1)
		return w.backoff(w.consecutiveFailures.Load())

	case result.Status == StatusProcessed:
		w.consecutiveFailures.Store(0)
		// There was work, so there may be more; drain before idling.
		return 0

	default:
		w.consecutiveFailures.Store(0)
		return w.options.PollInterval
	}
}

// backoffFor logs a cycle that failed outright and returns the delay.
func (w *Worker) backoffFor(ctx context.Context, err error) time.Duration {
	failures := w.consecutiveFailures.Add(1)

	var expired *CheckpointExpiredError
	var notReady *NotReadyError

	switch {
	case errors.As(err, &expired):
		// Loudest level available: messages may already be gone, and no amount
		// of retrying can bring them back.
		w.log.ErrorContext(ctx, "the outbox checkpoint is outside the retention window; "+
			"messages may have been lost and manual reconciliation is required", "error", err)
	case errors.As(err, &notReady):
		w.log.WarnContext(ctx, "waiting for change data capture", "reason", notReady.Reason)
	default:
		w.log.ErrorContext(ctx, "an outbox cycle failed", "error", err)
	}

	return w.backoff(failures)
}

// backoff grows the delay with each consecutive failure, up to the configured
// ceiling.
func (w *Worker) backoff(failures int64) time.Duration {
	const maxExponent = 10

	exponent := min(float64(failures-1), maxExponent)
	delay := min(float64(w.options.RetryBaseDelay)*math.Pow(2, exponent), float64(w.options.RetryMaxDelay))

	// Up to a fifth of the delay in jitter, so several workers that fail at the
	// same moment do not all come back at the same moment.
	delay += delay * 0.2 * rand.Float64() //nolint:gosec // scheduling jitter, not a secret

	return time.Duration(delay)
}
