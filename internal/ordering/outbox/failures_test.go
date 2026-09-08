package outbox_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/outbox"
)

func TestAMessageThatNeverFailedHasNoRecord(t *testing.T) {
	skipWithoutDocker(t)

	store := outbox.NewFailureStore(sharedDB)

	failure, err := store.Get(t.Context(), uuid.New())
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if failure.Attempts != 0 || failure.Poisoned {
		t.Errorf("failure = %+v, want a clean record", failure)
	}
}

func TestAttemptsAccumulateAcrossCycles(t *testing.T) {
	skipWithoutDocker(t)

	store := outbox.NewFailureStore(sharedDB)
	messageID := uuid.New()

	for want := 1; want <= 3; want++ {
		attempts, err := store.RecordFailure(t.Context(), messageID, "broker unreachable")
		if err != nil {
			t.Fatalf("recording: %v", err)
		}
		// The count has to survive a restart, or a worker that crashes while
		// retrying would try forever.
		if attempts != want {
			t.Errorf("attempts = %d, want %d", attempts, want)
		}
	}

	failure, err := store.Get(t.Context(), messageID)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if failure.Attempts != 3 || failure.Poisoned {
		t.Errorf("failure = %+v, want three attempts and no verdict yet", failure)
	}
}

func TestGivingUpIsRecordedOnce(t *testing.T) {
	skipWithoutDocker(t)

	store := outbox.NewFailureStore(sharedDB)
	messageID := uuid.New()

	if _, err := store.RecordFailure(t.Context(), messageID, "nope"); err != nil {
		t.Fatalf("recording: %v", err)
	}
	if err := store.MarkPoisoned(t.Context(), messageID); err != nil {
		t.Fatalf("marking poisoned: %v", err)
	}
	if err := store.MarkPoisoned(t.Context(), messageID); err != nil {
		t.Fatalf("marking poisoned again: %v", err)
	}

	failure, err := store.Get(t.Context(), messageID)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	// The row is the record of what was skipped, so it must not be rewritten
	// by a later pass and lose when it happened.
	if !failure.Poisoned {
		t.Error("the message should be recorded as given up on")
	}
}

func TestALongErrorIsTruncatedRatherThanRejected(t *testing.T) {
	skipWithoutDocker(t)

	store := outbox.NewFailureStore(sharedDB)

	// A driver can produce a very long message; losing the failure count over
	// it would be worse than losing the tail of the text.
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'x'
	}

	if _, err := store.RecordFailure(t.Context(), uuid.New(), string(long)); err != nil {
		t.Fatalf("recording a long error: %v", err)
	}
}
