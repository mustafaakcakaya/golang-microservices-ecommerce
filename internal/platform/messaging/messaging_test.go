package messaging_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/orderingv1"
)

func sampleEvent() *orderingv1.OrderCreated {
	return &orderingv1.OrderCreated{
		Header:     messaging.NewHeader("trace-1"),
		OrderID:    uuid.New(),
		CustomerID: uuid.New(),
		OrderName:  "ORD_1",
		Status:     "Pending",
		TotalPrice: messaging.NewMoney(decimal.RequireFromString("1000.50")),
		Items: []orderingv1.Line{
			{ProductID: uuid.New(), Quantity: 2, Price: messaging.NewMoney(decimal.RequireFromString("500.25"))},
		},
	}
}

func TestAnEnvelopeCarriesTheContractAndTheEncodedEvent(t *testing.T) {
	t.Parallel()

	event := sampleEvent()

	envelope, err := messaging.NewEnvelope(event, event.OrderID.String())
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}

	if envelope.ID != event.ID {
		t.Error("the envelope must carry the event id; it becomes the broker message id")
	}
	if envelope.Contract != (messaging.Contract{EventType: "ordering.order-created", Version: 1}) {
		t.Errorf("contract = %v, want ordering.order-created v1", envelope.Contract)
	}
	if envelope.AggregateID != event.OrderID.String() {
		t.Errorf("aggregate = %q, want the order id", envelope.AggregateID)
	}
}

func TestAnEnvelopeWithoutAnAggregateIsRefused(t *testing.T) {
	t.Parallel()

	// Without it a partitioning broker cannot keep one aggregate's events in
	// order, so an empty value is a defect rather than a default.
	if _, err := messaging.NewEnvelope(sampleEvent(), ""); err == nil {
		t.Fatal("an envelope with no aggregate id should be refused")
	}
}

func TestMoneyAlwaysCrossesTheWireAsAString(t *testing.T) {
	t.Parallel()

	// A package elsewhere in the process can flip how decimal encodes itself.
	// A contract must not change shape because of that.
	original := decimal.MarshalJSONWithoutQuotes
	decimal.MarshalJSONWithoutQuotes = true
	t.Cleanup(func() { decimal.MarshalJSONWithoutQuotes = original })

	payload, err := messaging.Marshal(sampleEvent())
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	if !strings.Contains(string(payload), `"totalPrice":"1000.5"`) {
		t.Errorf("payload = %s, want a quoted total", payload)
	}
}

func TestMoneyReadsBackWhateverItWasWrittenAs(t *testing.T) {
	t.Parallel()

	for _, written := range []string{`{"totalPrice":"12.34"}`, `{"totalPrice":12.34}`} {
		var decoded struct {
			TotalPrice messaging.Money `json:"totalPrice"`
		}
		if err := json.Unmarshal([]byte(written), &decoded); err != nil {
			t.Fatalf("decoding %s: %v", written, err)
		}
		if !decoded.TotalPrice.Equal(decimal.RequireFromString("12.34")) {
			t.Errorf("%s decoded to %s", written, decoded.TotalPrice)
		}
	}
}

func TestAPayloadRoundTripsThroughTheRegistry(t *testing.T) {
	t.Parallel()

	registry := messaging.NewRegistry()
	orderingv1.Register(registry)

	event := sampleEvent()
	envelope, err := messaging.NewEnvelope(event, event.OrderID.String())
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}

	decoded, err := registry.Decode(envelope)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}

	created, ok := decoded.(*orderingv1.OrderCreated)
	if !ok {
		t.Fatalf("decoded = %T, want *orderingv1.OrderCreated", decoded)
	}
	if created.OrderName != event.OrderName || !created.TotalPrice.Equal(event.TotalPrice.Decimal) {
		t.Errorf("decoded = %+v, want the event that was written", created)
	}
	if len(created.Items) != 1 || created.Items[0].Quantity != 2 {
		t.Errorf("items = %+v, want the single line", created.Items)
	}
}

func TestAnUnknownContractIsRefusedRatherThanForwarded(t *testing.T) {
	t.Parallel()

	registry := messaging.NewRegistry()

	_, err := registry.Decode(messaging.Envelope{
		Contract: messaging.Contract{EventType: "ordering.order-created", Version: 99},
		Payload:  []byte(`{}`),
	})

	// Forwarding a message this build cannot read would push the problem
	// downstream, where it is much harder to trace.
	if err == nil {
		t.Fatal("an unregistered contract should be refused")
	}
	if !strings.Contains(err.Error(), "v99") {
		t.Errorf("error = %v, want it to name the version that is missing", err)
	}
}

func TestRegisteringOneContractTwiceIsAWiringMistake(t *testing.T) {
	t.Parallel()

	registry := messaging.NewRegistry()
	orderingv1.Register(registry)

	defer func() {
		if recover() == nil {
			t.Error("two types claiming one wire identity should fail loudly at startup")
		}
	}()

	orderingv1.Register(registry)
}
