package outbox_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

func message500() decimal.Decimal {
	return decimal.NewFromInt(500)
}

// previous returns a position just before lsn, so a read starting there
// includes the group at lsn.
func previous(t *testing.T, lsn outbox.LSN) outbox.LSN {
	t.Helper()

	before := make(outbox.LSN, len(lsn))
	copy(before, lsn)

	for i := len(before) - 1; i >= 0; i-- {
		if before[i] > 0 {
			before[i]--
			return before
		}
		before[i] = 0xFF
	}

	t.Fatalf("cannot step back from %s", lsn)

	return nil
}

func TestAReaderWithNoCheckpointStartsFromTheOldestRetainedChange(t *testing.T) {
	skipWithoutDocker(t)

	store := outbox.NewCheckpointStore(sharedDB, "never-run")

	lsn, err := store.Load(t.Context())
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	// Absence is not a failure: it is how a reader says it has never run.
	if lsn != nil {
		t.Errorf("checkpoint = %s, want none", lsn)
	}
}

func TestACheckpointSurvivesForTheNextRun(t *testing.T) {
	skipWithoutDocker(t)

	store := outbox.NewCheckpointStore(sharedDB, "round-trip")
	want := outbox.LSN{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	if err := store.Save(t.Context(), want); err != nil {
		t.Fatalf("saving: %v", err)
	}

	got, err := store.Load(t.Context())
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got.Compare(want) != 0 {
		t.Errorf("checkpoint = %s, want %s", got, want)
	}

	// Saving again replaces the position rather than adding a second row: a
	// reader has one position, not a history of them.
	later := outbox.LSN{1, 2, 3, 4, 5, 6, 7, 8, 9, 11}
	if err := store.Save(t.Context(), later); err != nil {
		t.Fatalf("re-saving: %v", err)
	}
	got, err = store.Load(t.Context())
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got.Compare(later) != 0 {
		t.Errorf("checkpoint = %s, want %s", got, later)
	}
}

func TestReadersDoNotShareAPosition(t *testing.T) {
	skipWithoutDocker(t)

	mine := outbox.NewCheckpointStore(sharedDB, "reader-a")
	theirs := outbox.NewCheckpointStore(sharedDB, "reader-b")

	if err := mine.Save(t.Context(), outbox.LSN{9, 9, 9, 9, 9, 9, 9, 9, 9, 9}); err != nil {
		t.Fatalf("saving: %v", err)
	}

	// Named per reader, so a second independent reader could be added without
	// either of them moving the other's position.
	other, err := theirs.Load(t.Context())
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if other != nil {
		t.Errorf("reader-b sees %s, want its own empty position", other)
	}
}

func TestAnEmptyCheckpointIsRefused(t *testing.T) {
	skipWithoutDocker(t)

	store := outbox.NewCheckpointStore(sharedDB, "empty")

	// Storing a zero position would silently rewind the reader to the start of
	// the retained window on its next run.
	if err := store.Save(t.Context(), outbox.LSN{}); err == nil {
		t.Fatal("an empty checkpoint should be refused")
	}
}

func TestLSNOrderingMatchesTheServers(t *testing.T) {
	t.Parallel()

	low := outbox.LSN{0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	high := outbox.LSN{0, 0, 0, 0, 0, 0, 0, 0, 1, 0}

	if low.Compare(high) >= 0 {
		t.Error("a position with a higher byte earlier must sort later")
	}
	if !(outbox.LSN{}).IsZero() || !(outbox.LSN{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}).IsZero() {
		t.Error("an absent or all-zero position means no position")
	}
	if high.IsZero() {
		t.Error("a real position is not zero")
	}
	if got := high.String(); got != "0x00000000000000000100" {
		t.Errorf("String() = %q, want the hex the server prints", got)
	}
}

// compile-time reminder that the reader speaks in envelopes, not rows.
var _ = messaging.Envelope{}
