package outbox

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// Status is the outcome of one cycle.
type Status int

// The outcomes a cycle can have.
const (
	// StatusIdle means there was nothing new to publish.
	StatusIdle Status = iota

	// StatusProcessed means the batch was handled and the position advanced.
	StatusProcessed

	// StatusFaulted means a publish failed; the cycle stopped and the message
	// will be read again.
	StatusFaulted
)

// String names the outcome.
func (s Status) String() string {
	switch s {
	case StatusProcessed:
		return "processed"
	case StatusFaulted:
		return "faulted"
	default:
		return "idle"
	}
}

// Result is what one cycle did.
type Result struct {
	Status    Status
	Published int
	Poisoned  int
}

// ChangeReader reads captured outbox messages.
type ChangeReader interface {
	Read(ctx context.Context, after LSN, batchSize int) (Batch, error)
}

// Checkpoints remembers how far a reader has got.
type Checkpoints interface {
	Load(ctx context.Context) (LSN, error)
	Save(ctx context.Context, lsn LSN) error
}

// Failures counts publish attempts that did not work.
type Failures interface {
	Get(ctx context.Context, messageID uuid.UUID) (Failure, error)
	RecordFailure(ctx context.Context, messageID uuid.UUID, reason string) (int, error)
	MarkPoisoned(ctx context.Context, messageID uuid.UUID) error
}

// Processor performs one cycle: read a batch, publish it, advance the position.
//
// All of the behaviour lives here rather than in the polling loop, so what the
// worker actually does can be tested without waiting for a timer.
type Processor struct {
	reader      ChangeReader
	checkpoints Checkpoints
	failures    Failures
	publisher   messaging.Publisher
	options     Options
	log         *slog.Logger
}

// NewProcessor wires the processor.
func NewProcessor(
	reader ChangeReader,
	checkpoints Checkpoints,
	failures Failures,
	publisher messaging.Publisher,
	options Options,
	log *slog.Logger,
) *Processor {
	return &Processor{
		reader:      reader,
		checkpoints: checkpoints,
		failures:    failures,
		publisher:   publisher,
		options:     options,
		log:         log,
	}
}

// ProcessOnce publishes one batch.
//
// The position is saved after each group, and only once every message in it has
// been accepted or explicitly given up on. A crash between publishing and
// saving re-delivers the group, which is the safe direction to fail: consumers
// deduplicate, and nobody can recover a message that was never sent.
func (p *Processor) ProcessOnce(ctx context.Context) (Result, error) {
	checkpoint, err := p.checkpoints.Load(ctx)
	if err != nil {
		return Result{}, err
	}

	batch, err := p.reader.Read(ctx, checkpoint, p.options.BatchSize)
	if err != nil {
		return Result{}, err
	}
	if len(batch.Groups) == 0 {
		return Result{Status: StatusIdle}, nil
	}

	result := Result{Status: StatusProcessed}

	for _, group := range batch.Groups {
		for _, message := range group.Messages {
			if err := ctx.Err(); err != nil {
				return result, err
			}

			handled, err := p.publish(ctx, message, &result)
			if err != nil {
				return result, err
			}
			if !handled {
				// The message is left where it is and the position stays put,
				// so the next cycle reads it again.
				result.Status = StatusFaulted
				return result, nil
			}
		}

		if err := p.checkpoints.Save(ctx, group.LSN); err != nil {
			return result, err
		}
		checkpoint = group.LSN
	}

	p.log.InfoContext(ctx, "outbox cycle finished",
		"published", result.Published,
		"poisoned", result.Poisoned,
		"checkpoint", checkpoint.String())

	return result, nil
}

// publish sends one message, reporting whether the cycle may continue past it.
//
// Nothing here logs a payload. An id, a contract and an aggregate are enough to
// find a message, and the payload is the one part that could carry something a
// log should not.
func (p *Processor) publish(ctx context.Context, message messaging.Envelope, result *Result) (bool, error) {
	failure, err := p.failures.Get(ctx, message.ID)
	if err != nil {
		return false, err
	}
	if failure.Poisoned {
		p.log.WarnContext(ctx, "skipping a poisoned outbox message",
			"messageId", message.ID, "contract", message.Contract.String())
		return true, nil
	}

	if err := p.publisher.Publish(ctx, message); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}

		return p.recordFailure(ctx, message, err, result)
	}

	result.Published++

	return true, nil
}

// recordFailure counts the attempt and decides whether to keep trying.
func (p *Processor) recordFailure(
	ctx context.Context, message messaging.Envelope, cause error, result *Result,
) (bool, error) {
	attempts, err := p.failures.RecordFailure(ctx, message.ID, cause.Error())
	if err != nil {
		return false, err
	}

	if attempts >= p.options.MaxPublishAttempts {
		if err := p.failures.MarkPoisoned(ctx, message.ID); err != nil {
			return false, err
		}
		result.Poisoned++

		// Giving up is the lesser harm: one message nobody can publish must
		// not stop every message written after it.
		p.log.ErrorContext(ctx, "giving up on an outbox message",
			"messageId", message.ID,
			"contract", message.Contract.String(),
			"aggregateId", message.AggregateID,
			"attempts", attempts,
			"error", cause)

		return true, nil
	}

	p.log.WarnContext(ctx, "publishing an outbox message failed",
		"messageId", message.ID,
		"contract", message.Contract.String(),
		"attempts", attempts,
		"maxAttempts", p.options.MaxPublishAttempts,
		"error", cause)

	return false, nil
}
