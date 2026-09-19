package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// prefetch bounds how many unacknowledged messages one consumer holds. Without
// it the broker pushes the whole queue at once, so a restart re-delivers
// everything in flight instead of a handful.
const prefetch = 16

// redeliveryDelay slows a message that keeps failing. Requeuing immediately
// turns a broken consumer into a hot loop against the broker.
const redeliveryDelay = time.Second

// SubscriberConfig describes what to consume and from where.
type SubscriberConfig struct {
	Config

	// Queue is this consumer's durable queue. Two instances sharing a name
	// share the work; two names each get their own copy of every message.
	Queue string

	// Contracts are the event types to bind the queue to.
	Contracts []messaging.Contract
}

// Subscriber consumes integration events from RabbitMQ.
type Subscriber struct {
	config SubscriberConfig
	log    *slog.Logger

	connection *amqp.Connection
	channel    *amqp.Channel
}

// NewSubscriber wires the subscriber. Nothing is dialled yet.
func NewSubscriber(config SubscriberConfig, log *slog.Logger) *Subscriber {
	if config.Exchange == "" {
		config.Exchange = DefaultExchange
	}

	return &Subscriber{config: config, log: log}
}

// Subscribe consumes until the context is cancelled.
func (s *Subscriber) Subscribe(ctx context.Context, deliver messaging.Delivery) error {
	deliveries, err := s.open()
	if err != nil {
		return err
	}

	s.log.InfoContext(ctx, "listening for integration events",
		"queue", s.config.Queue, "exchange", s.config.Exchange)

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				// The channel closed under us: the connection dropped, or the
				// broker cancelled the consumer.
				return errors.New("the rabbitmq consumer was closed by the broker")
			}

			s.handle(ctx, delivery, deliver)
		}
	}
}

// Close releases the connection.
func (s *Subscriber) Close() error {
	if s.connection == nil {
		return nil
	}

	err := s.connection.Close()
	s.channel, s.connection = nil, nil

	if err != nil && !errors.Is(err, amqp.ErrClosed) {
		return fmt.Errorf("closing rabbitmq connection: %w", err)
	}

	return nil
}

// handle runs one delivery and tells the broker what became of it.
func (s *Subscriber) handle(ctx context.Context, delivery amqp.Delivery, deliver messaging.Delivery) {
	envelope, err := envelopeFrom(delivery)
	if err != nil {
		// Nothing about this message will improve by retrying it, and keeping
		// it would block the queue behind it.
		s.log.ErrorContext(ctx, "discarding an unreadable message",
			"messageId", delivery.MessageId, "error", err)
		_ = delivery.Nack(false, false)

		return
	}

	if err := deliver(ctx, envelope); err != nil {
		s.log.ErrorContext(ctx, "handling a message failed; it will be delivered again",
			"messageId", envelope.ID, "contract", envelope.Contract.String(), "error", err)

		// Requeued after a pause. A message that never succeeds is retried
		// forever; a dead-letter policy is the next step for that.
		time.Sleep(redeliveryDelay)
		_ = delivery.Nack(false, true)

		return
	}

	if err := delivery.Ack(false); err != nil {
		// The work is done but the broker was not told, so it will arrive
		// again. That is what the consumer's inbox is for.
		s.log.WarnContext(ctx, "could not acknowledge a handled message",
			"messageId", envelope.ID, "error", err)
	}
}

// envelopeFrom rebuilds the envelope the publisher sent.
func envelopeFrom(delivery amqp.Delivery) (messaging.Envelope, error) {
	id, err := uuid.Parse(delivery.MessageId)
	if err != nil {
		return messaging.Envelope{}, fmt.Errorf("message id %q is not a uuid: %w", delivery.MessageId, err)
	}
	if delivery.Type == "" {
		return messaging.Envelope{}, errors.New("the message carries no event type")
	}

	version, _ := delivery.Headers["schema-version"].(int32)
	if version == 0 {
		return messaging.Envelope{}, errors.New("the message carries no schema version")
	}

	aggregateID, _ := delivery.Headers["aggregate-id"].(string)

	return messaging.Envelope{
		ID:            id,
		Contract:      messaging.Contract{EventType: delivery.Type, Version: int(version)},
		AggregateID:   aggregateID,
		CorrelationID: delivery.CorrelationId,
		OccurredOnUTC: delivery.Timestamp.UTC(),
		Payload:       delivery.Body,
	}, nil
}

// open declares the queue, binds it and starts consuming.
func (s *Subscriber) open() (<-chan amqp.Delivery, error) {
	if s.config.Queue == "" {
		return nil, errors.New("a subscriber needs a queue name")
	}
	if len(s.config.Contracts) == 0 {
		return nil, errors.New("a subscriber needs at least one contract to bind to")
	}

	connection, err := amqp.Dial(s.config.URL)
	if err != nil {
		return nil, fmt.Errorf("connecting to rabbitmq: %w", err)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("opening rabbitmq channel: %w", err)
	}

	if err := channel.ExchangeDeclare(
		s.config.Exchange, amqp.ExchangeTopic, true, false, false, false, nil,
	); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("declaring exchange %s: %w", s.config.Exchange, err)
	}

	// Durable and not auto-deleted: the queue keeps collecting messages while
	// the consumer is down, which is the whole point of subscribing to a
	// producer that does not wait for it.
	if _, err := channel.QueueDeclare(s.config.Queue, true, false, false, false, nil); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("declaring queue %s: %w", s.config.Queue, err)
	}

	for _, contract := range s.config.Contracts {
		if err := channel.QueueBind(s.config.Queue, contract.EventType, s.config.Exchange, false, nil); err != nil {
			_ = connection.Close()
			return nil, fmt.Errorf("binding queue %s to %s: %w", s.config.Queue, contract.EventType, err)
		}
	}

	if err := channel.Qos(prefetch, 0, false); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("setting prefetch: %w", err)
	}

	// Manual acknowledgement: the broker must not consider a message handled
	// just because it was delivered.
	deliveries, err := channel.Consume(s.config.Queue, "", false, false, false, false, nil)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("consuming from %s: %w", s.config.Queue, err)
	}

	s.connection, s.channel = connection, channel

	return deliveries, nil
}

// compile-time check that the subscriber satisfies the seam.
var _ messaging.Subscriber = (*Subscriber)(nil)
