package mssql_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
)

func TestAMessageIsClaimedOnceAndSkippedAfterwards(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	store := mssql.NewInboxStore(sharedDB)
	messageID := uuid.New()

	claimed, err := store.Begin(t.Context(), messageID, "billing")
	if err != nil {
		t.Fatalf("claiming: %v", err)
	}
	if !claimed {
		t.Fatal("a message never seen before should be claimed")
	}

	if err := store.Complete(t.Context(), messageID, "billing"); err != nil {
		t.Fatalf("completing: %v", err)
	}

	// The whole point: publishing is at-least-once, so this delivery is normal
	// and must do nothing.
	again, err := store.Begin(t.Context(), messageID, "billing")
	if err != nil {
		t.Fatalf("claiming again: %v", err)
	}
	if again {
		t.Error("a message this consumer finished should not be claimed again")
	}
}

func TestAClaimThatWasNeverFinishedIsRetried(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	store := mssql.NewInboxStore(sharedDB)
	messageID := uuid.New()

	if _, err := store.Begin(t.Context(), messageID, "billing"); err != nil {
		t.Fatalf("claiming: %v", err)
	}

	// The consumer died between claiming and finishing. Running the work twice
	// is recoverable; skipping it is not, so the message is claimed again.
	again, err := store.Begin(t.Context(), messageID, "billing")
	if err != nil {
		t.Fatalf("claiming again: %v", err)
	}
	if !again {
		t.Error("an unfinished claim should be retried, not treated as handled")
	}
}

func TestOneConsumerHandlingAMessageDoesNotSpeakForAnother(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	store := mssql.NewInboxStore(sharedDB)
	messageID := uuid.New()

	if _, err := store.Begin(t.Context(), messageID, "billing"); err != nil {
		t.Fatalf("claiming: %v", err)
	}
	if err := store.Complete(t.Context(), messageID, "billing"); err != nil {
		t.Fatalf("completing: %v", err)
	}

	claimed, err := store.Begin(t.Context(), messageID, "shipping")
	if err != nil {
		t.Fatalf("claiming for shipping: %v", err)
	}
	if !claimed {
		t.Error("each consumer gets its own turn at a message")
	}
}
