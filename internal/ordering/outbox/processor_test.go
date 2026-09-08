package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// The processor's behaviour needs no database and no broker: the point of
// keeping the loop and the decisions apart is that the decisions can be tested
// like this.

type fakeReader struct {
	batches []outbox.Batch
	read    int
}

func (r *fakeReader) Read(context.Context, outbox.LSN, int) (outbox.Batch, error) {
	if r.read >= len(r.batches) {
		return outbox.Batch{}, nil
	}

	batch := r.batches[r.read]
	r.read++

	return batch, nil
}

type fakeCheckpoints struct {
	saved []outbox.LSN
}

func (c *fakeCheckpoints) Load(context.Context) (outbox.LSN, error) {
	if len(c.saved) == 0 {
		return nil, nil
	}

	return c.saved[len(c.saved)-1], nil
}

func (c *fakeCheckpoints) Save(_ context.Context, lsn outbox.LSN) error {
	c.saved = append(c.saved, lsn)
	return nil
}

func (c *fakeCheckpoints) last() outbox.LSN {
	if len(c.saved) == 0 {
		return nil
	}

	return c.saved[len(c.saved)-1]
}

type fakeFailures struct {
	attempts map[uuid.UUID]int
	poisoned map[uuid.UUID]bool
}

func newFakeFailures() *fakeFailures {
	return &fakeFailures{attempts: map[uuid.UUID]int{}, poisoned: map[uuid.UUID]bool{}}
}

func (f *fakeFailures) Get(_ context.Context, id uuid.UUID) (outbox.Failure, error) {
	return outbox.Failure{Attempts: f.attempts[id], Poisoned: f.poisoned[id]}, nil
}

func (f *fakeFailures) RecordFailure(_ context.Context, id uuid.UUID, _ string) (int, error) {
	f.attempts[id]++
	return f.attempts[id], nil
}

func (f *fakeFailures) MarkPoisoned(_ context.Context, id uuid.UUID) error {
	f.poisoned[id] = true
	return nil
}

type fakePublisher struct {
	mu        sync.Mutex
	published []uuid.UUID
	failWith  error
	failIDs   map[uuid.UUID]bool
}

func (p *fakePublisher) Publish(_ context.Context, envelope messaging.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.failWith != nil && (p.failIDs == nil || p.failIDs[envelope.ID]) {
		return p.failWith
	}

	p.published = append(p.published, envelope.ID)

	return nil
}

func (p *fakePublisher) Close() error { return nil }

func (p *fakePublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.published)
}

func envelope() messaging.Envelope {
	return messaging.Envelope{
		ID:          uuid.New(),
		Contract:    messaging.Contract{EventType: "ordering.order-created", Version: 1},
		AggregateID: uuid.NewString(),
		Payload:     []byte(`{}`),
	}
}

func group(lsn byte, messages ...messaging.Envelope) outbox.Group {
	return outbox.Group{LSN: outbox.LSN{0, 0, 0, 0, 0, 0, 0, 0, 0, lsn}, Messages: messages}
}

func newProcessor(
	reader outbox.ChangeReader,
	checkpoints outbox.Checkpoints,
	failures outbox.Failures,
	publisher messaging.Publisher,
	options outbox.Options,
) *outbox.Processor {
	return outbox.NewProcessor(reader, checkpoints, failures, publisher, options,
		slog.New(slog.DiscardHandler))
}

func TestACycleWithNothingToDoIsIdle(t *testing.T) {
	t.Parallel()

	checkpoints := &fakeCheckpoints{}
	processor := newProcessor(&fakeReader{}, checkpoints, newFakeFailures(),
		&fakePublisher{}, outbox.DefaultOptions())

	result, err := processor.ProcessOnce(t.Context())
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if result.Status != outbox.StatusIdle {
		t.Errorf("status = %v, want idle", result.Status)
	}
	if checkpoints.last() != nil {
		t.Error("an idle cycle must not move the position")
	}
}

func TestTheCheckpointAdvancesOnceAGroupIsFullyPublished(t *testing.T) {
	t.Parallel()

	first, second := envelope(), envelope()
	reader := &fakeReader{batches: []outbox.Batch{{Groups: []outbox.Group{
		group(1, first),
		group(2, second),
	}}}}
	checkpoints := &fakeCheckpoints{}
	publisher := &fakePublisher{}

	processor := newProcessor(reader, checkpoints, newFakeFailures(), publisher, outbox.DefaultOptions())

	result, err := processor.ProcessOnce(t.Context())
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if result.Published != 2 || publisher.count() != 2 {
		t.Errorf("published %d, want both messages", publisher.count())
	}
	// Saved per group, not once at the end: a crash mid-batch then re-delivers
	// only the groups that were not finished.
	if len(checkpoints.saved) != 2 {
		t.Errorf("saved %d checkpoints, want one per group", len(checkpoints.saved))
	}
	if checkpoints.last().Compare(group(2).LSN) != 0 {
		t.Errorf("checkpoint = %s, want the last group", checkpoints.last())
	}
}

func TestAFailedPublishLeavesTheCheckpointBehindTheFailure(t *testing.T) {
	t.Parallel()

	first, second := envelope(), envelope()
	reader := &fakeReader{batches: []outbox.Batch{{Groups: []outbox.Group{
		group(1, first),
		group(2, second),
	}}}}
	checkpoints := &fakeCheckpoints{}
	publisher := &fakePublisher{
		failWith: errors.New("broker unreachable"),
		failIDs:  map[uuid.UUID]bool{second.ID: true},
	}

	processor := newProcessor(reader, checkpoints, newFakeFailures(), publisher, outbox.DefaultOptions())

	result, err := processor.ProcessOnce(t.Context())
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if result.Status != outbox.StatusFaulted {
		t.Errorf("status = %v, want faulted", result.Status)
	}
	// The position stays before the message that failed, so the next cycle
	// reads it again. This is what makes delivery at-least-once.
	if checkpoints.last().Compare(group(1).LSN) != 0 {
		t.Errorf("checkpoint = %s, want the last group that fully published", checkpoints.last())
	}
}

func TestAMessageIsGivenUpOnAfterTheConfiguredAttempts(t *testing.T) {
	t.Parallel()

	stubborn, healthy := envelope(), envelope()
	failures := newFakeFailures()
	checkpoints := &fakeCheckpoints{}
	publisher := &fakePublisher{
		failWith: errors.New("the broker will never take this"),
		failIDs:  map[uuid.UUID]bool{stubborn.ID: true},
	}

	options := outbox.DefaultOptions()
	options.MaxPublishAttempts = 3

	// Each cycle re-reads the same group, as the real reader would while the
	// checkpoint stays put. The failing message is first in the group, so the
	// one behind it does not get through until the first is given up on.
	batch := outbox.Batch{Groups: []outbox.Group{group(1, stubborn, healthy)}}
	for attempt := 1; attempt <= options.MaxPublishAttempts; attempt++ {
		reader := &fakeReader{batches: []outbox.Batch{batch}}
		processor := newProcessor(reader, checkpoints, failures, publisher, options)

		if _, err := processor.ProcessOnce(t.Context()); err != nil {
			t.Fatalf("cycle %d: %v", attempt, err)
		}
	}

	poisoned, err := failures.Get(t.Context(), stubborn.ID)
	if err != nil {
		t.Fatalf("reading failures: %v", err)
	}
	if !poisoned.Poisoned {
		t.Fatal("a message that never publishes should eventually be given up on")
	}
	// Giving up is what lets everything written after it through: one message
	// nobody can publish must not stop the stream.
	if publisher.count() != 1 || publisher.published[0] != healthy.ID {
		t.Errorf("published %d messages, want the healthy one to get through", publisher.count())
	}
	if checkpoints.last().Compare(group(1).LSN) != 0 {
		t.Error("the position should advance once the group is published or given up on")
	}
}

func TestAPoisonedMessageIsSkippedRatherThanRetried(t *testing.T) {
	t.Parallel()

	poisoned := envelope()
	failures := newFakeFailures()
	failures.poisoned[poisoned.ID] = true

	publisher := &fakePublisher{}
	reader := &fakeReader{batches: []outbox.Batch{{Groups: []outbox.Group{group(1, poisoned)}}}}

	processor := newProcessor(reader, &fakeCheckpoints{}, failures, publisher, outbox.DefaultOptions())

	result, err := processor.ProcessOnce(t.Context())
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if publisher.count() != 0 {
		t.Error("a message already given up on should not be sent again")
	}
	if result.Status != outbox.StatusProcessed {
		t.Errorf("status = %v, want the cycle to continue past it", result.Status)
	}
}

// fakeCycles stands in for the processor so the loop can be tested without one.
type fakeCycles struct {
	mu      sync.Mutex
	results []outbox.Result
	err     error
	calls   int
}

func (c *fakeCycles) ProcessOnce(context.Context) (outbox.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls++

	if c.err != nil {
		return outbox.Result{}, c.err
	}
	if len(c.results) == 0 {
		return outbox.Result{Status: outbox.StatusIdle}, nil
	}

	result := c.results[0]
	c.results = c.results[1:]

	return result, nil
}

func (c *fakeCycles) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.calls
}

type readyImmediately struct{}

func (readyImmediately) Wait(context.Context) error { return nil }

func TestTheWorkerStopsCleanlyWhenCancelled(t *testing.T) {
	t.Parallel()

	options := outbox.DefaultOptions()
	options.PollInterval = 10 * time.Millisecond

	cycles := &fakeCycles{}
	worker := outbox.NewWorker(cycles, readyImmediately{}, options, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// A cancelled context is a clean stop: the position is stored after
		// every group, so stopping between cycles loses nothing.
		if err != nil {
			t.Errorf("stopping returned %v, want a clean shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the worker did not stop")
	}

	if cycles.count() == 0 {
		t.Error("the worker never ran a cycle")
	}
}

func TestAWorkerThatKeepsFailingReportsItselfUnhealthy(t *testing.T) {
	t.Parallel()

	options := outbox.DefaultOptions()
	options.RetryBaseDelay = time.Millisecond
	options.RetryMaxDelay = 5 * time.Millisecond

	cycles := &fakeCycles{err: errors.New("the broker is down")}
	worker := outbox.NewWorker(cycles, readyImmediately{}, options, slog.New(slog.DiscardHandler))

	if err := worker.Health(t.Context()); err != nil {
		t.Fatalf("a worker that has not run yet is healthy, got %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = worker.Run(ctx) }()

	// A worker that is running but publishing nothing is exactly what a
	// liveness check misses.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if worker.Health(t.Context()) != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("repeated failures never made the worker report itself unhealthy")
}
