package inbox_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/inbox"
)

// fakeStore is an in-memory inbox; the SQL Server one has its own tests.
//
// It is keyed by message and consumer together, like the table it stands in
// for: what one consumer has handled says nothing about another.
type claim struct {
	messageID uuid.UUID
	consumer  string
}

type fakeStore struct {
	completed map[claim]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{completed: map[claim]bool{}}
}

func (s *fakeStore) Begin(_ context.Context, messageID uuid.UUID, consumer string) (bool, error) {
	return !s.completed[claim{messageID, consumer}], nil
}

func (s *fakeStore) Complete(_ context.Context, messageID uuid.UUID, consumer string) error {
	s.completed[claim{messageID, consumer}] = true
	return nil
}

func envelope() messaging.Envelope {
	return messaging.Envelope{
		ID:       uuid.New(),
		Contract: messaging.Contract{EventType: "ordering.order-created", Version: 1},
		Payload:  []byte(`{}`),
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestARepeatedDeliveryDoesTheWorkOnce(t *testing.T) {
	t.Parallel()

	runs := 0
	handle := inbox.Idempotent(newFakeStore(), "billing", discard(),
		func(context.Context, messaging.Envelope) error {
			runs++
			return nil
		})

	message := envelope()
	for range 3 {
		if err := handle(t.Context(), message); err != nil {
			t.Fatalf("handling: %v", err)
		}
	}

	// Delivery is at-least-once, so this is the normal case rather than an
	// exceptional one.
	if runs != 1 {
		t.Errorf("the handler ran %d times, want once", runs)
	}
}

func TestDifferentConsumersEachGetTheirTurn(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	message := envelope()

	for _, consumer := range []string{"billing", "shipping"} {
		ran := false
		handle := inbox.Idempotent(store, consumer, discard(),
			func(context.Context, messaging.Envelope) error {
				ran = true
				return nil
			})

		if err := handle(t.Context(), message); err != nil {
			t.Fatalf("handling for %s: %v", consumer, err)
		}
		// One consumer having handled a message says nothing about another.
		if !ran {
			t.Errorf("%s never saw the message", consumer)
		}
	}
}

func TestAFailedHandlerIsRunAgainOnTheNextDelivery(t *testing.T) {
	t.Parallel()

	runs := 0
	failing := errors.New("the downstream call failed")

	handle := inbox.Idempotent(newFakeStore(), "billing", discard(),
		func(context.Context, messaging.Envelope) error {
			runs++
			if runs == 1 {
				return failing
			}
			return nil
		})

	message := envelope()

	if err := handle(t.Context(), message); !errors.Is(err, failing) {
		t.Fatalf("error = %v, want the handler's failure to surface", err)
	}
	if err := handle(t.Context(), message); err != nil {
		t.Fatalf("second delivery: %v", err)
	}

	// The work did not happen the first time, so it must not be recorded as
	// done: a message is only finished once the handler says so.
	if runs != 2 {
		t.Errorf("the handler ran %d times, want it retried after failing", runs)
	}
}

func TestAMessageWithoutAnIDIsRefused(t *testing.T) {
	t.Parallel()

	ran := false
	handle := inbox.Idempotent(newFakeStore(), "billing", discard(),
		func(context.Context, messaging.Envelope) error {
			ran = true
			return nil
		})

	err := handle(t.Context(), messaging.Envelope{
		Contract: messaging.Contract{EventType: "ordering.order-created", Version: 1},
	})

	// Nothing to deduplicate on. Handling it anyway would turn every retry
	// into a second order.
	if err == nil {
		t.Fatal("a message with no id should be refused")
	}
	if ran {
		t.Error("the handler ran on a message that cannot be deduplicated")
	}
}
