package messaging_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/broker"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/kafka"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/orderingv2"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/rabbitmq"
)

func sampleEvent() *orderingv2.OrderCreated {
	return &orderingv2.OrderCreated{
		Header:     messaging.NewHeader("trace-1"),
		OrderID:    uuid.New(),
		CustomerID: uuid.New(),
		Status:     "Pending",
		TotalPrice: messaging.NewMoney(decimal.RequireFromString("1000.50")),
		Items: []orderingv2.Line{
			{ProductID: uuid.New(), Quantity: 2, Price: messaging.NewMoney(decimal.RequireFromString("500.25"))},
		},
	}
}

// sampleEnvelope is a published-ready envelope for one sample event.
func sampleEnvelope(t *testing.T) messaging.Envelope {
	t.Helper()

	event := sampleEvent()

	envelope, err := messaging.NewEnvelope(event, event.OrderID.String())
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}

	return envelope
}

// rabbitConfig and kafkaConfig keep the broker packages out of the other tests
// in this package, which need no container at all.
func rabbitConfig(url string) rabbitmq.Config {
	return rabbitmq.Config{URL: url}
}

func kafkaConfig(addresses []string) broker.Config {
	return broker.Config{
		Provider: broker.ProviderKafka,
		Kafka:    kafka.Config{Brokers: addresses, TopicPrefix: "test."},
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
	if envelope.Contract != (messaging.Contract{EventType: "ordering.order-created", Version: 2}) {
		t.Errorf("contract = %v, want ordering.order-created v2", envelope.Contract)
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
	orderingv2.Register(registry)

	event := sampleEvent()
	envelope, err := messaging.NewEnvelope(event, event.OrderID.String())
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}

	decoded, err := registry.Decode(envelope)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}

	created, ok := decoded.(*orderingv2.OrderCreated)
	if !ok {
		t.Fatalf("decoded = %T, want *orderingv2.OrderCreated", decoded)
	}
	if created.OrderID != event.OrderID || !created.TotalPrice.Equal(event.TotalPrice.Decimal) {
		t.Errorf("decoded = %+v, want the event that was written", created)
	}
	if len(created.Items) != 1 || created.Items[0].Quantity != 2 {
		t.Errorf("items = %+v, want the single line", created.Items)
	}
}

func TestTheOrderEventsAreVersionTwoAndCarryNoName(t *testing.T) {
	t.Parallel()

	registry := messaging.NewRegistry()
	orderingv2.Register(registry)

	// Version 1 carried an order name. It was removed rather than kept beside
	// version 2, so a v1 message is refused like any contract this build does
	// not know.
	for _, eventType := range []string{"ordering.order-created", "ordering.order-updated"} {
		if _, err := registry.Decode(messaging.Envelope{
			Contract: messaging.Contract{EventType: eventType, Version: 1},
			Payload:  []byte(`{}`),
		}); err == nil {
			t.Errorf("%s v1 was decoded, want it refused", eventType)
		}
	}

	for _, event := range []messaging.Event{&orderingv2.OrderCreated{}, &orderingv2.OrderUpdated{}} {
		if version := event.Contract().Version; version != 2 {
			t.Errorf("%s is version %d, want 2", event.Contract().EventType, version)
		}

		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshalling: %v", err)
		}
		if strings.Contains(string(payload), "orderName") {
			t.Errorf("%s still carries the order name: %s", event.Contract().EventType, payload)
		}
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
	orderingv2.Register(registry)

	defer func() {
		if recover() == nil {
			t.Error("two types claiming one wire identity should fail loudly at startup")
		}
	}()

	orderingv2.Register(registry)
}

func TestARejectionIsRecognisedThroughWrapping(t *testing.T) {
	t.Parallel()

	cause := errors.New("the total does not match the lines")
	wrapped := fmt.Errorf("handling checkout: %w", messaging.Reject(cause))

	if !messaging.IsRejected(wrapped) {
		t.Error("a wrapped rejection should still be recognised")
	}
	if !errors.Is(wrapped, cause) {
		t.Error("the rejection should keep its cause reachable")
	}
	if messaging.IsRejected(cause) {
		t.Error("an ordinary error is not a rejection")
	}
	if messaging.Reject(nil) != nil {
		t.Error("rejecting nothing should be nothing")
	}
}
