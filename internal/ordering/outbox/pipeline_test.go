package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/inbox"
)

// These tests run the whole path on real infrastructure: an order is saved,
// SQL Server captures the outbox insert, the processor reads it and a publisher
// receives it. What they are really about is the failure in the middle - the
// pipeline is only worth anything if it behaves when a step does not finish.

// recordingBroker stands in for a broker and remembers what reached it.
type recordingBroker struct {
	mu        sync.Mutex
	delivered []messaging.Envelope
}

func (b *recordingBroker) Publish(_ context.Context, envelope messaging.Envelope) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.delivered = append(b.delivered, envelope)

	return nil
}

func (b *recordingBroker) Close() error { return nil }

// deliveriesOf returns what reached the broker for one aggregate.
func (b *recordingBroker) deliveriesOf(aggregateID string) []messaging.Envelope {
	b.mu.Lock()
	defer b.mu.Unlock()

	var mine []messaging.Envelope
	for _, envelope := range b.delivered {
		if envelope.AggregateID == aggregateID {
			mine = append(mine, envelope)
		}
	}

	return mine
}

// checkpointsThatCannotSave stands in for a worker that dies after publishing
// and before recording where it got to.
type checkpointsThatCannotSave struct {
	outbox.Checkpoints
}

func (c *checkpointsThatCannotSave) Save(context.Context, outbox.LSN) error {
	return errors.New("the worker died before it could record its position")
}

func pipelineFor(
	t *testing.T, checkpoints outbox.Checkpoints, publisher messaging.Publisher,
) *outbox.Processor {
	t.Helper()

	return outbox.NewProcessor(
		outbox.NewReader(sharedDB),
		checkpoints,
		outbox.NewFailureStore(sharedDB),
		publisher,
		outbox.DefaultOptions(),
		slog.New(slog.DiscardHandler),
	)
}

// startedJustBefore gives a reader a position immediately before the order's
// messages, so a run picks up that order and nothing older.
func startedJustBefore(t *testing.T, group outbox.Group, consumer string) outbox.Checkpoints {
	t.Helper()

	checkpoints := outbox.NewCheckpointStore(sharedDB, consumer)
	if err := checkpoints.Save(t.Context(), previous(t, group.LSN)); err != nil {
		t.Fatalf("seeding the checkpoint: %v", err)
	}

	return checkpoints
}

func TestAnOrderTravelsFromTheDatabaseToTheBroker(t *testing.T) {
	skipWithoutDocker(t)

	order := placeOrder(t, "ORD_F", false)
	group := waitForGroup(t, outbox.NewReader(sharedDB), nil, order.ID.String(), 1)

	broker := &recordingBroker{}
	checkpoints := startedJustBefore(t, group, "pipeline-"+uuid.NewString())

	if _, err := pipelineFor(t, checkpoints, broker).ProcessOnce(t.Context()); err != nil {
		t.Fatalf("processing: %v", err)
	}

	delivered := broker.deliveriesOf(order.ID.String())
	if len(delivered) != 1 {
		t.Fatalf("the broker received %d messages, want the one order", len(delivered))
	}
	if delivered[0].Contract.EventType != "ordering.order-created" {
		t.Errorf("contract = %s, want ordering.order-created", delivered[0].Contract)
	}
}

func TestARestartResumesRatherThanRepublishing(t *testing.T) {
	skipWithoutDocker(t)

	order := placeOrder(t, "ORD_G", false)
	group := waitForGroup(t, outbox.NewReader(sharedDB), nil, order.ID.String(), 1)

	broker := &recordingBroker{}
	checkpoints := startedJustBefore(t, group, "pipeline-"+uuid.NewString())

	if _, err := pipelineFor(t, checkpoints, broker).ProcessOnce(t.Context()); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// A new processor over the same stored position, as a replaced worker
	// would be.
	if _, err := pipelineFor(t, checkpoints, broker).ProcessOnce(t.Context()); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if delivered := broker.deliveriesOf(order.ID.String()); len(delivered) != 1 {
		t.Errorf("the broker received %d messages, want one: the position survived the restart",
			len(delivered))
	}
}

func TestACrashBeforeCheckpointingRedeliversAndTheInboxAbsorbsIt(t *testing.T) {
	skipWithoutDocker(t)

	order := placeOrder(t, "ORD_H", false)
	group := waitForGroup(t, outbox.NewReader(sharedDB), nil, order.ID.String(), 1)

	broker := &recordingBroker{}
	checkpoints := startedJustBefore(t, group, "pipeline-"+uuid.NewString())

	// The message reaches the broker, and then the worker dies before it can
	// record that it did.
	crashing := pipelineFor(t, &checkpointsThatCannotSave{Checkpoints: checkpoints}, broker)
	if _, err := crashing.ProcessOnce(t.Context()); err == nil {
		t.Fatal("the failing checkpoint save should surface")
	}

	// The replacement worker starts from the last position that was actually
	// stored, so it sends the message again. This is at-least-once delivery,
	// and it is the safe direction to fail: a repeat can be recognised, a lost
	// message cannot be recovered.
	if _, err := pipelineFor(t, checkpoints, broker).ProcessOnce(t.Context()); err != nil {
		t.Fatalf("second run: %v", err)
	}

	delivered := broker.deliveriesOf(order.ID.String())
	if len(delivered) != 2 {
		t.Fatalf("the broker received %d messages, want the duplicate a crash produces", len(delivered))
	}

	// The consumer is what turns at-least-once into once. Both deliveries go
	// through the inbox; the work happens for the first one only.
	runs := 0
	handle := inbox.Idempotent(mssql.NewInboxStore(sharedDB), "pipeline-consumer",
		slog.New(slog.DiscardHandler),
		func(context.Context, messaging.Envelope) error {
			runs++
			return nil
		})

	for i, envelope := range delivered {
		if err := handle(t.Context(), envelope); err != nil {
			t.Fatalf("handling delivery %d: %v", i+1, err)
		}
	}

	if runs != 1 {
		t.Errorf("the consumer did the work %d times, want once", runs)
	}
	// Both deliveries carry the same id, which is the only thing that makes
	// recognising the duplicate possible.
	if delivered[0].ID != delivered[1].ID {
		t.Error("a redelivery must carry the same message id")
	}
}
