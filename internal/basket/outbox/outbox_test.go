package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// fakeStore is the recorded checkouts, in memory.
type fakeStore struct {
	mu       sync.Mutex
	messages map[uuid.UUID]*outbox.Message
	due      []uuid.UUID
	poisoned map[uuid.UUID]string
	failures map[uuid.UUID]time.Time
}

func newFakeStore(messages ...outbox.Message) *fakeStore {
	store := &fakeStore{
		messages: map[uuid.UUID]*outbox.Message{},
		poisoned: map[uuid.UUID]string{},
		failures: map[uuid.UUID]time.Time{},
	}
	for _, message := range messages {
		stored := message
		store.messages[message.Envelope.ID] = &stored
		store.due = append(store.due, message.Envelope.ID)
	}

	return store
}

func (s *fakeStore) Due(_ context.Context, limit int, _ time.Time) ([]outbox.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ready []outbox.Message
	for _, id := range s.due {
		message, held := s.messages[id]
		if !held || s.poisoned[id] != "" {
			continue
		}
		ready = append(ready, *message)
		if len(ready) == limit {
			break
		}
	}

	return ready, nil
}

func (s *fakeStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.messages, id)

	return nil
}

func (s *fakeStore) Fail(_ context.Context, id uuid.UUID, _ string, nextAttempt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if message, held := s.messages[id]; held {
		message.Attempts++
	}
	s.failures[id] = nextAttempt

	return nil
}

func (s *fakeStore) Poison(_ context.Context, id uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.poisoned[id] = reason

	return nil
}

func (s *fakeStore) held(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, held := s.messages[id]

	return held
}

// fakePublisher stands in for the broker.
type fakePublisher struct {
	mu        sync.Mutex
	published []uuid.UUID
	failWith  error
}

func (p *fakePublisher) Publish(_ context.Context, envelope messaging.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.failWith != nil {
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

func message() outbox.Message {
	return outbox.Message{
		Envelope: messaging.Envelope{
			ID:          uuid.New(),
			Contract:    messaging.Contract{EventType: "basket.checkout-requested", Version: 1},
			AggregateID: uuid.NewString(),
			Payload:     []byte(`{}`),
		},
	}
}

func newRelay(store outbox.Store, publisher messaging.Publisher, options outbox.Options) *outbox.Relay {
	return outbox.NewRelay(store, publisher, options, slog.New(slog.DiscardHandler))
}

func TestAPublishedCheckoutIsRemoved(t *testing.T) {
	t.Parallel()

	recorded := message()
	store := newFakeStore(recorded)
	publisher := &fakePublisher{}

	published, err := newRelay(store, publisher, outbox.DefaultOptions()).ProcessOnce(t.Context())
	if err != nil {
		t.Fatalf("relaying: %v", err)
	}

	if published != 1 || publisher.count() != 1 {
		t.Fatalf("published %d messages, want one", publisher.count())
	}
	// Removed only after the broker took it. The outbox is a queue of work,
	// not the record of what was ordered - that belongs to Ordering.
	if store.held(recorded.Envelope.ID) {
		t.Error("a published checkout should not stay in the outbox")
	}
}

func TestACheckoutTheBrokerRefusesIsKept(t *testing.T) {
	t.Parallel()

	recorded := message()
	store := newFakeStore(recorded)
	publisher := &fakePublisher{failWith: errors.New("the broker is unreachable")}

	published, err := newRelay(store, publisher, outbox.DefaultOptions()).ProcessOnce(t.Context())
	if err != nil {
		t.Fatalf("relaying: %v", err)
	}

	if published != 0 {
		t.Errorf("published = %d, want none", published)
	}
	// The whole point: a broker outage delays the announcement instead of
	// losing it.
	if !store.held(recorded.Envelope.ID) {
		t.Error("an unpublished checkout must stay in the outbox")
	}
	// And it is not retried immediately, or a broken broker becomes a hot
	// loop.
	if next, tried := store.failures[recorded.Envelope.ID]; !tried || !next.After(time.Now()) {
		t.Errorf("next attempt = %v, want it pushed into the future", next)
	}
}

func TestACheckoutIsGivenUpOnAfterTooManyAttempts(t *testing.T) {
	t.Parallel()

	recorded := message()
	store := newFakeStore(recorded)
	publisher := &fakePublisher{failWith: errors.New("the broker will never take this")}

	options := outbox.DefaultOptions()
	options.MaxAttempts = 3
	relay := newRelay(store, publisher, options)

	for attempt := 1; attempt <= options.MaxAttempts; attempt++ {
		if _, err := relay.ProcessOnce(t.Context()); err != nil {
			t.Fatalf("cycle %d: %v", attempt, err)
		}
	}

	// One message nobody can publish must not hold up every checkout behind
	// it. The row stays as the record of what was skipped.
	if store.poisoned[recorded.Envelope.ID] == "" {
		t.Error("a message that never publishes should eventually be given up on")
	}
	if !store.held(recorded.Envelope.ID) {
		t.Error("a poisoned message should stay as the record of what was skipped")
	}
}

func TestAPoisonedCheckoutDoesNotBlockTheRest(t *testing.T) {
	t.Parallel()

	stubborn, healthy := message(), message()
	store := newFakeStore(stubborn, healthy)
	store.poisoned[stubborn.Envelope.ID] = "given up on earlier"

	publisher := &fakePublisher{}

	if _, err := newRelay(store, publisher, outbox.DefaultOptions()).ProcessOnce(t.Context()); err != nil {
		t.Fatalf("relaying: %v", err)
	}

	if publisher.count() != 1 || publisher.published[0] != healthy.Envelope.ID {
		t.Errorf("published %v, want only the healthy message", publisher.published)
	}
}

func TestTheRelayStopsCleanlyWhenCancelled(t *testing.T) {
	t.Parallel()

	options := outbox.DefaultOptions()
	options.PollInterval = 10 * time.Millisecond

	relay := newRelay(newFakeStore(), &fakePublisher{}, options)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	time.Sleep(40 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// Stopping between cycles loses nothing: what is unpublished is still
		// in the table.
		if err != nil {
			t.Errorf("stopping returned %v, want a clean shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the relay did not stop")
	}
}
