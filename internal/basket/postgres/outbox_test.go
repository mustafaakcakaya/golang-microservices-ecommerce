package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/postgres"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

func announcement(aggregateID string) messaging.Envelope {
	return messaging.Envelope{
		ID:            uuid.New(),
		Contract:      messaging.Contract{EventType: "basket.checkout-submitted", Version: 1},
		AggregateID:   aggregateID,
		CorrelationID: "trace-1",
		OccurredOnUTC: time.Now().UTC(),
		Payload:       []byte(`{"userName":"mustafa"}`),
	}
}

func TestCheckoutClearsTheBasketAndRecordsTheAnnouncement(t *testing.T) {
	repo, pool := newRepositoryWithPool(t)
	store := postgres.NewOutboxStore(pool)
	ctx := context.Background()

	if err := repo.Store(ctx, cart("checkout-1", item("iPhone X", 1, 500))); err != nil {
		t.Fatalf("storing: %v", err)
	}

	message := announcement("checkout-1")
	if err := repo.Checkout(ctx, "checkout-1", message); err != nil {
		t.Fatalf("checking out: %v", err)
	}

	if _, err := repo.GetByUserName(ctx, "checkout-1"); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("the basket survived the checkout (error %v)", err)
	}

	due, err := store.Due(ctx, 10, time.Now())
	if err != nil {
		t.Fatalf("reading due: %v", err)
	}
	recorded := find(due, message.ID)
	if recorded == nil {
		t.Fatal("the announcement was not recorded")
	}
	// The payload reaches the broker exactly as it was written, so it has to
	// survive the round trip byte for byte.
	if string(recorded.Envelope.Payload) != string(message.Payload) {
		t.Errorf("payload = %s, want %s", recorded.Envelope.Payload, message.Payload)
	}
	if recorded.Envelope.Contract != message.Contract ||
		recorded.Envelope.AggregateID != message.AggregateID ||
		recorded.Envelope.CorrelationID != message.CorrelationID {
		t.Errorf("recorded = %+v, want the envelope that was written", recorded.Envelope)
	}
}

func TestCheckingOutAnUnknownBasketRecordsNothing(t *testing.T) {
	repo, pool := newRepositoryWithPool(t)
	store := postgres.NewOutboxStore(pool)
	ctx := context.Background()

	message := announcement("customer-2")
	err := repo.Checkout(ctx, "never-existed", message)

	// The basket is gone, so someone else checked it out and recorded the
	// message already - or it was never there.
	if apperr.KindOf(err) != apperr.KindNotFound {
		t.Fatalf("kind = %v, want KindNotFound (error %v)", apperr.KindOf(err), err)
	}

	due, err := store.Due(ctx, 100, time.Now())
	if err != nil {
		t.Fatalf("reading due: %v", err)
	}
	// Nothing is announced for a checkout that did not happen: the whole
	// transaction rolls back together.
	if find(due, message.ID) != nil {
		t.Error("a failed checkout recorded an announcement")
	}
}

func TestTheSameMessageIsRecordedOnce(t *testing.T) {
	repo, pool := newRepositoryWithPool(t)
	store := postgres.NewOutboxStore(pool)
	ctx := context.Background()

	message := announcement("customer-3")

	for _, user := range []string{"double-1", "double-2"} {
		if err := repo.Store(ctx, cart(user, item("iPhone X", 1, 500))); err != nil {
			t.Fatalf("storing %s: %v", user, err)
		}
		if err := repo.Checkout(ctx, user, message); err != nil {
			t.Fatalf("checking out %s: %v", user, err)
		}
	}

	due, err := store.Due(ctx, 100, time.Now())
	if err != nil {
		t.Fatalf("reading due: %v", err)
	}

	// Two checkouts of the same basket derive the same id, so the second must
	// not add a second message - that would be two orders for one basket.
	seen := 0
	for _, candidate := range due {
		if candidate.Envelope.ID == message.ID {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("the message is recorded %d times, want once", seen)
	}
}

func TestAFailedPublishIsRetriedLaterAndThenGivenUpOn(t *testing.T) {
	repo, pool := newRepositoryWithPool(t)
	store := postgres.NewOutboxStore(pool)
	ctx := context.Background()

	if err := repo.Store(ctx, cart("retry-1", item("iPhone X", 1, 500))); err != nil {
		t.Fatalf("storing: %v", err)
	}
	message := announcement("customer-4")
	if err := repo.Checkout(ctx, "retry-1", message); err != nil {
		t.Fatalf("checking out: %v", err)
	}

	if err := store.Fail(ctx, message.ID, "the broker is unreachable", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("recording a failure: %v", err)
	}

	due, err := store.Due(ctx, 100, time.Now())
	if err != nil {
		t.Fatalf("reading due: %v", err)
	}
	// Held back until its next attempt is due, so a broken broker is not a
	// hot loop. The backoff survives a restart because it is in the row.
	if find(due, message.ID) != nil {
		t.Error("a message waiting for its next attempt should not be due yet")
	}

	later, err := store.Due(ctx, 100, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("reading due later: %v", err)
	}
	waiting := find(later, message.ID)
	if waiting == nil {
		t.Fatal("the message should be due once its next attempt has passed")
	}
	if waiting.Attempts != 1 {
		t.Errorf("attempts = %d, want the failure counted", waiting.Attempts)
	}

	if err := store.Poison(ctx, message.ID, "given up on"); err != nil {
		t.Fatalf("poisoning: %v", err)
	}
	evenLater, err := store.Due(ctx, 100, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("reading due after poisoning: %v", err)
	}
	// One message nobody can publish must not be retried forever in front of
	// the rest.
	if find(evenLater, message.ID) != nil {
		t.Error("a message that was given up on should not come back")
	}
}

func TestAPublishedMessageIsRemoved(t *testing.T) {
	repo, pool := newRepositoryWithPool(t)
	store := postgres.NewOutboxStore(pool)
	ctx := context.Background()

	if err := repo.Store(ctx, cart("done-1", item("iPhone X", 1, 500))); err != nil {
		t.Fatalf("storing: %v", err)
	}
	message := announcement("customer-5")
	if err := repo.Checkout(ctx, "done-1", message); err != nil {
		t.Fatalf("checking out: %v", err)
	}

	if err := store.Delete(ctx, message.ID); err != nil {
		t.Fatalf("removing: %v", err)
	}

	due, err := store.Due(ctx, 100, time.Now())
	if err != nil {
		t.Fatalf("reading due: %v", err)
	}
	if find(due, message.ID) != nil {
		t.Error("a published message should be gone from the outbox")
	}
}

func find(messages []outbox.Message, id uuid.UUID) *outbox.Message {
	for i := range messages {
		if messages[i].Envelope.ID == id {
			return &messages[i]
		}
	}

	return nil
}

// The repository is what the handlers depend on, transaction included.
var _ carts.Repository = (*postgres.BasketRepository)(nil)
