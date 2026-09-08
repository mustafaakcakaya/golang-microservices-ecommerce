package kafka_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/shopspring/decimal"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/kafka"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/orderingv1"
)

const cardNumber = "5555555555554444"

// brokers starts a cluster for the test and returns its addresses.
func brokers(t *testing.T) []string {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := context.Background()

	container, err := tckafka.Run(ctx, "confluentinc/confluent-local:7.6.1")
	if err != nil {
		t.Fatalf("starting kafka: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminating kafka: %v", err)
		}
	})

	addresses, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("broker addresses: %v", err)
	}

	return addresses
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

// createTopic makes the topic up front.
//
// Auto-creation is a broker setting that deployed clusters usually turn off,
// so the test does what an operator would: create the topic with the partition
// count it should have.
func createTopic(t *testing.T, addresses []string, topic string) {
	t.Helper()

	client := &kafkago.Client{Addr: kafkago.TCP(addresses...)}

	response, err := client.CreateTopics(t.Context(), &kafkago.CreateTopicsRequest{
		Topics: []kafkago.TopicConfig{{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}},
	})
	if err != nil {
		t.Fatalf("creating topic %s: %v", topic, err)
	}
	for name, topicErr := range response.Errors {
		if topicErr != nil {
			t.Fatalf("creating topic %s: %v", name, topicErr)
		}
	}
}

func headerValue(message kafkago.Message, key string) string {
	for _, header := range message.Headers {
		if header.Key == key {
			return string(header.Value)
		}
	}

	return ""
}

func TestPublishingDeliversThePayloadKeyedByAggregate(t *testing.T) {
	addresses := brokers(t)

	registry := messaging.NewRegistry()
	orderingv1.Register(registry)

	config := kafka.Config{Brokers: addresses, TopicPrefix: "test."}
	publisher := kafka.New(config, registry)
	t.Cleanup(func() { _ = publisher.Close() })

	envelope := sampleEnvelope(t)
	createTopic(t, addresses, config.Topic(envelope.Contract.EventType))

	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: addresses,
		Topic:   config.Topic(envelope.Contract.EventType),
		// From the beginning: the message was produced before this reader
		// existed, which is the normal case for an event stream.
		StartOffset: kafkago.FirstOffset,
		GroupID:     "",
		Partition:   0,
	})
	t.Cleanup(func() { _ = reader.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	message, err := reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	// The key is what keeps one aggregate's events on one partition, and so in
	// order relative to each other.
	if string(message.Key) != envelope.AggregateID {
		t.Errorf("key = %q, want the aggregate id %s", message.Key, envelope.AggregateID)
	}
	// Kafka has no message id of its own, so the id a consumer deduplicates on
	// has to travel in a header.
	if got := headerValue(message, kafka.MessageIDHeader); got != envelope.ID.String() {
		t.Errorf("message-id header = %q, want %s", got, envelope.ID)
	}
	if got := headerValue(message, kafka.SchemaVersionHeader); got != "1" {
		t.Errorf("schema-version header = %q, want 1", got)
	}
	if got := headerValue(message, kafka.CorrelationIDHeader); got != "trace-1" {
		t.Errorf("correlation-id header = %q, want it carried through", got)
	}
	if string(message.Value) != string(envelope.Payload) {
		t.Errorf("value = %s, want the stored payload %s", message.Value, envelope.Payload)
	}
	if strings.Contains(string(message.Value), cardNumber) {
		t.Error("the broker received card details")
	}
}

func TestPublishingAnUnknownContractIsRefused(t *testing.T) {
	addresses := brokers(t)

	publisher := kafka.New(kafka.Config{Brokers: addresses}, messaging.NewRegistry())
	t.Cleanup(func() { _ = publisher.Close() })

	err := publisher.Publish(t.Context(), sampleEnvelope(t))

	if err == nil {
		t.Fatal("a contract this build does not know should not reach the cluster")
	}
	if !strings.Contains(err.Error(), "ordering.order-created") {
		t.Errorf("error = %v, want it to name the contract", err)
	}
}
