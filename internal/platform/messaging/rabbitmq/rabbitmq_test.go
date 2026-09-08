package rabbitmq_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/shopspring/decimal"
	tcrabbitmq "github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/orderingv1"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/rabbitmq"
)

const cardNumber = "5555555555554444"

// brokerURL starts a broker for the test and returns its address.
func brokerURL(t *testing.T) string {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := context.Background()

	container, err := tcrabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine")
	if err != nil {
		t.Fatalf("starting rabbitmq: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminating rabbitmq: %v", err)
		}
	})

	url, err := container.AmqpURL(ctx)
	if err != nil {
		t.Fatalf("broker url: %v", err)
	}

	return url
}

func sampleEnvelope(t *testing.T) messaging.Envelope {
	t.Helper()

	orderID := uuid.New()
	event := &orderingv1.OrderCreated{
		Header:     messaging.NewHeader("trace-1"),
		OrderID:    orderID,
		CustomerID: uuid.New(),
		OrderName:  "ORD_1",
		Status:     "Pending",
		TotalPrice: messaging.NewMoney(decimal.NewFromInt(1000)),
		Items: []orderingv1.Line{
			{ProductID: uuid.New(), Quantity: 2, Price: messaging.NewMoney(decimal.NewFromInt(500))},
		},
	}

	envelope, err := messaging.NewEnvelope(event, orderID.String())
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}

	return envelope
}

// subscribe binds a queue to the exchange and returns its deliveries.
func subscribe(t *testing.T, url, exchange, routingKey string) <-chan amqp.Delivery {
	t.Helper()

	connection, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	channel, err := connection.Channel()
	if err != nil {
		t.Fatalf("opening channel: %v", err)
	}

	if err := channel.ExchangeDeclare(exchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		t.Fatalf("declaring exchange: %v", err)
	}
	queue, err := channel.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("declaring queue: %v", err)
	}
	if err := channel.QueueBind(queue.Name, routingKey, exchange, false, nil); err != nil {
		t.Fatalf("binding queue: %v", err)
	}

	deliveries, err := channel.Consume(queue.Name, "", true, false, false, false, nil)
	if err != nil {
		t.Fatalf("consuming: %v", err)
	}

	return deliveries
}

func TestPublishingDeliversTheStoredPayloadAndItsMetadata(t *testing.T) {
	url := brokerURL(t)

	registry := messaging.NewRegistry()
	orderingv1.Register(registry)

	publisher := rabbitmq.New(rabbitmq.Config{URL: url}, registry)
	t.Cleanup(func() { _ = publisher.Close() })

	envelope := sampleEnvelope(t)

	// The consumer binds first: an event nobody is bound to is dropped, which
	// is the intended behaviour and would make this test silently pass nothing.
	deliveries := subscribe(t, url, rabbitmq.DefaultExchange, envelope.Contract.EventType)

	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	select {
	case delivery := <-deliveries:
		// The message id is what a consumer deduplicates on, so it has to be
		// the event id rather than something the broker made up.
		if delivery.MessageId != envelope.ID.String() {
			t.Errorf("message id = %q, want the event id %s", delivery.MessageId, envelope.ID)
		}
		if delivery.Type != envelope.Contract.EventType {
			t.Errorf("type = %q, want %q", delivery.Type, envelope.Contract.EventType)
		}
		if delivery.CorrelationId != "trace-1" {
			t.Errorf("correlation id = %q, want it carried through", delivery.CorrelationId)
		}
		if version, _ := delivery.Headers["schema-version"].(int32); version != 1 {
			t.Errorf("schema-version header = %v, want 1", delivery.Headers["schema-version"])
		}
		if delivery.DeliveryMode != amqp.Persistent {
			t.Error("messages must be persistent, or a broker restart undoes the outbox")
		}
		// Forwarded byte for byte: the publisher does not re-encode what the
		// writer already encoded.
		if string(delivery.Body) != string(envelope.Payload) {
			t.Errorf("body = %s, want the stored payload %s", delivery.Body, envelope.Payload)
		}
		if strings.Contains(string(delivery.Body), cardNumber) {
			t.Error("the broker received card details")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the message never arrived")
	}
}

func TestPublishingAnUnknownContractIsRefused(t *testing.T) {
	url := brokerURL(t)

	// An empty registry stands in for a build deployed before the contract
	// existed.
	publisher := rabbitmq.New(rabbitmq.Config{URL: url}, messaging.NewRegistry())
	t.Cleanup(func() { _ = publisher.Close() })

	err := publisher.Publish(t.Context(), sampleEnvelope(t))

	if err == nil {
		t.Fatal("a contract this build does not know should not reach the bus")
	}
	if !strings.Contains(err.Error(), "ordering.order-created") {
		t.Errorf("error = %v, want it to name the contract", err)
	}
}
