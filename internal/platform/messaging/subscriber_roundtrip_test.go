package messaging_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	tcrabbitmq "github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/broker"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/orderingv1"
)

// Publishing and subscribing are tested together and against both brokers,
// because the thing worth proving is that what one side sends is exactly what
// the other side receives - including the metadata a consumer deduplicates on.

func brokerConfig(t *testing.T, provider broker.Provider) broker.Config {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := context.Background()

	switch provider {
	case broker.ProviderRabbitMQ:
		container, err := tcrabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine")
		if err != nil {
			t.Fatalf("starting rabbitmq: %v", err)
		}
		t.Cleanup(func() { _ = container.Terminate(context.Background()) })

		url, err := container.AmqpURL(ctx)
		if err != nil {
			t.Fatalf("broker url: %v", err)
		}

		return broker.Config{Provider: provider, RabbitMQ: rabbitConfig(url)}

	default:
		container, err := tckafka.Run(ctx, "confluentinc/confluent-local:7.6.1")
		if err != nil {
			t.Fatalf("starting kafka: %v", err)
		}
		t.Cleanup(func() { _ = container.Terminate(context.Background()) })

		addresses, err := container.Brokers(ctx)
		if err != nil {
			t.Fatalf("broker addresses: %v", err)
		}

		config := kafkaConfig(addresses)
		// Created up front, as an operator would: deployed clusters usually
		// disable auto-creation, and where it is on it happens asynchronously,
		// so the first publish would race it.
		createTopic(t, addresses, config.Kafka.Topic((orderingv1.OrderCreated{}).Contract().EventType))

		return config
	}
}

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

func TestAMessageArrivesAsItWasSent(t *testing.T) {
	for _, provider := range []broker.Provider{broker.ProviderRabbitMQ, broker.ProviderKafka} {
		t.Run(string(provider), func(t *testing.T) {
			config := brokerConfig(t, provider)

			registry := messaging.NewRegistry()
			orderingv1.Register(registry)

			contracts := []messaging.Contract{(orderingv1.OrderCreated{}).Contract()}
			log := slog.New(slog.DiscardHandler)

			subscriber, err := broker.NewSubscriber(config, "checkout-test", contracts, log)
			if err != nil {
				t.Fatalf("building subscriber: %v", err)
			}
			t.Cleanup(func() { _ = subscriber.Close() })

			received := make(chan messaging.Envelope, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			subscribed := make(chan error, 1)
			go func() {
				subscribed <- subscriber.Subscribe(ctx, func(_ context.Context, envelope messaging.Envelope) error {
					received <- envelope
					return nil
				})
			}()

			publisher, err := broker.New(config, registry)
			if err != nil {
				t.Fatalf("building publisher: %v", err)
			}
			t.Cleanup(func() { _ = publisher.Close() })

			sent := sampleEnvelope(t)
			publishUntilDelivered(t, ctx, publisher, sent, received)

			cancel()
			if err := <-subscribed; err != nil {
				t.Errorf("subscribing ended with %v", err)
			}
		})
	}
}

// publishUntilDelivered publishes and waits. A consumer that has just
// subscribed may not have its binding or its group assignment yet, so an early
// message can legitimately be missed; republishing is how the test stays about
// the round trip rather than about startup timing.
func publishUntilDelivered(
	t *testing.T,
	ctx context.Context,
	publisher messaging.Publisher,
	sent messaging.Envelope,
	received <-chan messaging.Envelope,
) {
	t.Helper()

	deadline := time.After(60 * time.Second)

	for {
		if err := publisher.Publish(ctx, sent); err != nil {
			t.Fatalf("publishing: %v", err)
		}

		select {
		case got := <-received:
			assertSameMessage(t, sent, got)
			return
		case <-time.After(2 * time.Second):
		case <-deadline:
			t.Fatal("the message never arrived")
		}
	}
}

func assertSameMessage(t *testing.T, sent, got messaging.Envelope) {
	t.Helper()

	// The id is what a consumer deduplicates on, so it has to survive the trip
	// unchanged; delivery is at-least-once on both brokers.
	if got.ID != sent.ID {
		t.Errorf("message id = %s, want %s", got.ID, sent.ID)
	}
	if got.Contract != sent.Contract {
		t.Errorf("contract = %v, want %v", got.Contract, sent.Contract)
	}
	if got.AggregateID != sent.AggregateID {
		t.Errorf("aggregate = %q, want %q", got.AggregateID, sent.AggregateID)
	}
	if got.CorrelationID != sent.CorrelationID {
		t.Errorf("correlation id = %q, want %q", got.CorrelationID, sent.CorrelationID)
	}
	if string(got.Payload) != string(sent.Payload) {
		t.Errorf("payload = %s, want %s", got.Payload, sent.Payload)
	}
}

func TestAFailedHandlerLeavesTheMessageWithTheBroker(t *testing.T) {
	config := brokerConfig(t, broker.ProviderRabbitMQ)

	registry := messaging.NewRegistry()
	orderingv1.Register(registry)

	contracts := []messaging.Contract{(orderingv1.OrderCreated{}).Contract()}
	log := slog.New(slog.DiscardHandler)

	subscriber, err := broker.NewSubscriber(config, "retry-test", contracts, log)
	if err != nil {
		t.Fatalf("building subscriber: %v", err)
	}
	t.Cleanup(func() { _ = subscriber.Close() })

	attempts := make(chan uuid.UUID, 4)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = subscriber.Subscribe(ctx, func(_ context.Context, envelope messaging.Envelope) error {
			attempts <- envelope.ID
			// Never succeeds, so the broker should keep handing it back.
			return errors.New("the downstream call failed")
		})
	}()

	publisher, err := broker.New(config, registry)
	if err != nil {
		t.Fatalf("building publisher: %v", err)
	}
	t.Cleanup(func() { _ = publisher.Close() })

	sent := sampleEnvelope(t)
	deadline := time.After(60 * time.Second)
	seen := 0

	for seen < 2 {
		if err := publisher.Publish(ctx, sent); err != nil {
			t.Fatalf("publishing: %v", err)
		}

		select {
		case <-attempts:
			seen++
		case <-time.After(3 * time.Second):
		case <-deadline:
			t.Fatalf("the message was delivered %d times, want it retried after failing", seen)
		}
	}
}
