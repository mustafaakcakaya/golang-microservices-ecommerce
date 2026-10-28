package messaging_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	kafkago "github.com/segmentio/kafka-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	tcrabbitmq "github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/broker"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/kafka"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/orderingv2"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/rabbitmq"
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
		createTopic(t, addresses, config.Kafka.Topic((orderingv2.OrderCreated{}).Contract().EventType))

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
			orderingv2.Register(registry)

			contracts := []messaging.Contract{(orderingv2.OrderCreated{}).Contract()}
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
	orderingv2.Register(registry)

	contracts := []messaging.Contract{(orderingv2.OrderCreated{}).Contract()}
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

func TestARejectedMessageIsSetAsideWithItsReason(t *testing.T) {
	for _, provider := range []broker.Provider{broker.ProviderRabbitMQ, broker.ProviderKafka} {
		t.Run(string(provider), func(t *testing.T) {
			config := brokerConfig(t, provider)

			const consumer = "reject-test"
			contract := (orderingv2.OrderCreated{}).Contract()

			if provider == broker.ProviderKafka {
				createTopic(t, config.Kafka.Brokers, kafka.RejectedTopic(config.Kafka.Topic(contract.EventType)))
			}

			registry := messaging.NewRegistry()
			orderingv2.Register(registry)
			log := slog.New(slog.DiscardHandler)

			subscriber, err := broker.NewSubscriber(config, consumer, []messaging.Contract{contract}, log)
			if err != nil {
				t.Fatalf("building subscriber: %v", err)
			}
			t.Cleanup(func() { _ = subscriber.Close() })

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			go func() {
				_ = subscriber.Subscribe(ctx, func(context.Context, messaging.Envelope) error {
					return messaging.Reject(errors.New("the total does not match the lines"))
				})
			}()

			publisher, err := broker.New(config, registry)
			if err != nil {
				t.Fatalf("building publisher: %v", err)
			}
			t.Cleanup(func() { _ = publisher.Close() })

			sent := sampleEnvelope(t)

			var id, reason string
			switch provider {
			case broker.ProviderRabbitMQ:
				id, reason = firstFromErrorQueue(t, ctx, config.RabbitMQ.URL, rabbitmq.ErrorQueue(consumer), publisher, sent)
			default:
				id, reason = firstFromRejectedTopic(t, ctx, config.Kafka.Brokers,
					kafka.RejectedTopic(config.Kafka.Topic(contract.EventType)), publisher, sent)
			}

			// The copy has to be recognisable as the message it replaces, and
			// say why it was given up on.
			if id != sent.ID.String() {
				t.Errorf("set-aside message id = %q, want %s", id, sent.ID)
			}
			if !strings.Contains(reason, "the total does not match the lines") {
				t.Errorf("rejection reason = %q, want the handler's error", reason)
			}
		})
	}
}

// firstFromErrorQueue publishes until a copy shows up in the error queue.
func firstFromErrorQueue(
	t *testing.T, ctx context.Context, url, queue string,
	publisher messaging.Publisher, sent messaging.Envelope,
) (string, string) {
	t.Helper()

	connection, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("connecting to rabbitmq: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	channel, err := connection.Channel()
	if err != nil {
		t.Fatalf("opening channel: %v", err)
	}

	deadline := time.After(60 * time.Second)
	for {
		if err := publisher.Publish(ctx, sent); err != nil {
			t.Fatalf("publishing: %v", err)
		}

		select {
		case <-deadline:
			t.Fatal("no rejected message reached the error queue")
		case <-time.After(2 * time.Second):
		}

		// The queue is declared by the subscriber, so it may not exist yet.
		delivery, ok, err := channel.Get(queue, true)
		if err != nil {
			if channel, err = connection.Channel(); err != nil {
				t.Fatalf("reopening channel: %v", err)
			}
			continue
		}
		if ok {
			reason, _ := delivery.Headers["rejection-reason"].(string)
			return delivery.MessageId, reason
		}
	}
}

// firstFromRejectedTopic publishes once and reads the copy back.
func firstFromRejectedTopic(
	t *testing.T, ctx context.Context, addresses []string, topic string,
	publisher messaging.Publisher, sent messaging.Envelope,
) (string, string) {
	t.Helper()

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     addresses,
		Topic:       topic,
		StartOffset: kafkago.FirstOffset,
	})
	t.Cleanup(func() { _ = reader.Close() })

	// The group starts from the first offset, so one publish is enough.
	if err := publisher.Publish(ctx, sent); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	readCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	message, err := reader.ReadMessage(readCtx)
	if err != nil {
		t.Fatalf("reading %s: %v", topic, err)
	}

	var id, reason string
	for _, header := range message.Headers {
		switch header.Key {
		case kafka.MessageIDHeader:
			id = string(header.Value)
		case kafka.RejectionReasonHeader:
			reason = string(header.Value)
		}
	}

	return id, reason
}
