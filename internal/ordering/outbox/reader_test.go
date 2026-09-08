package outbox_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/integration"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/orderingv1"
)

func TestReadingRebuildsTheStoredMessage(t *testing.T) {
	skipWithoutDocker(t)

	reader := outbox.NewReader(sharedDB)
	order := placeOrder(t, "ORD_R", false)

	group := waitForGroup(t, reader, nil, order.ID.String(), 1)

	message := group.Messages[0]
	if message.Contract != (orderingv1.OrderCreated{}).Contract() {
		t.Errorf("contract = %v, want ordering.order-created v1", message.Contract)
	}
	if message.AggregateID != order.ID.String() {
		t.Errorf("aggregate = %q, want the order id", message.AggregateID)
	}

	// The payload survives the trip through the change table unchanged, which
	// is what lets a publisher forward it without re-encoding.
	decoded, err := integration.Registry().Decode(message)
	if err != nil {
		t.Fatalf("decoding the captured payload: %v", err)
	}
	created, ok := decoded.(*orderingv1.OrderCreated)
	if !ok {
		t.Fatalf("decoded = %T, want *orderingv1.OrderCreated", decoded)
	}
	if created.OrderName != "ORD_R" || !created.TotalPrice.Equal(message500()) {
		t.Errorf("decoded = %+v, want the order that was saved", created)
	}
	if strings.Contains(string(message.Payload), "5555555555554444") {
		t.Error("the captured payload carries card details")
	}
}

func TestOneTransactionIsOneGroup(t *testing.T) {
	skipWithoutDocker(t)

	reader := outbox.NewReader(sharedDB)

	// Two changes to the aggregate before a single save, so both messages are
	// written by one transaction.
	order := placeOrder(t, "ORD_T", true)

	group := waitForGroup(t, reader, nil, order.ID.String(), 2)

	// Grouping is what lets the checkpoint stop only at transaction
	// boundaries: publishing half of a transaction would tell other services
	// an order was created without the update that immediately corrected it.
	if group.Messages[0].Contract.EventType != "ordering.order-created" ||
		group.Messages[1].Contract.EventType != "ordering.order-updated" {
		t.Errorf("group = %v, want the create followed by the update",
			[]string{group.Messages[0].Contract.EventType, group.Messages[1].Contract.EventType})
	}
}

func TestABatchIsNeverCutInsideATransaction(t *testing.T) {
	skipWithoutDocker(t)

	reader := outbox.NewReader(sharedDB)
	order := placeOrder(t, "ORD_S", true)

	// Wait for capture first, then read with a batch smaller than the
	// transaction that produced the messages.
	group := waitForGroup(t, reader, nil, order.ID.String(), 2)

	batch, err := reader.Read(t.Context(), previous(t, group.LSN), 1)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if len(batch.Groups) == 0 {
		t.Fatal("the batch was empty")
	}
	// A transaction bigger than the batch is fetched whole rather than split:
	// the checkpoint has nowhere to stop inside it.
	if got := len(batch.Groups[0].Messages); got != 2 {
		t.Errorf("first group has %d messages, want the whole transaction (2)", got)
	}
}

func TestReadingResumesAfterTheCheckpoint(t *testing.T) {
	skipWithoutDocker(t)

	reader := outbox.NewReader(sharedDB)

	first := placeOrder(t, "ORD_1", false)
	firstGroup := waitForGroup(t, reader, nil, first.ID.String(), 1)

	second := placeOrder(t, "ORD_2", false)
	secondGroup := waitForGroup(t, reader, firstGroup.LSN, second.ID.String(), 1)

	// Reading from a position returns what came after it and nothing else,
	// which is what makes a restart resume rather than replay.
	batch, err := reader.Read(t.Context(), firstGroup.LSN, 100)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	for _, group := range batch.Groups {
		if group.LSN.Compare(firstGroup.LSN) <= 0 {
			t.Errorf("a group at %s came back after checkpoint %s", group.LSN, firstGroup.LSN)
		}
	}
	if secondGroup.LSN.Compare(firstGroup.LSN) <= 0 {
		t.Error("the later transaction should have a later position")
	}
}

func TestACheckpointOlderThanRetentionIsReported(t *testing.T) {
	skipWithoutDocker(t)

	reader := outbox.NewReader(sharedDB)
	placeOrder(t, "ORD_E", false)

	// A position from long before anything this server retains.
	ancient := outbox.LSN{0, 0, 0, 0, 0, 0, 0, 0, 0, 1}

	_, err := reader.Read(t.Context(), ancient, 100)

	// Never a silent skip: changes between the checkpoint and the retention
	// floor may have been cleaned up, so a person has to decide what happens.
	var expired *outbox.CheckpointExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("error = %v, want a CheckpointExpiredError", err)
	}
	if !strings.Contains(expired.Error(), "Reconcile against OutboxMessages") {
		t.Errorf("error = %v, want it to say what an operator should do", expired)
	}
}

func TestReadingRejectsAnEmptyBatchSize(t *testing.T) {
	skipWithoutDocker(t)

	if _, err := outbox.NewReader(sharedDB).Read(t.Context(), nil, 0); err == nil {
		t.Fatal("a batch size of zero should be refused")
	}
}
