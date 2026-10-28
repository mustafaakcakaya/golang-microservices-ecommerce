package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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

	// rejects is a second channel, in confirm mode, for moving rejected
	// messages aside. The consuming channel cannot be put in confirm mode
	// without every acknowledgement on it waiting behind publishes.
	rejects *amqp.Channel
}

// ErrorQueue is where a consumer's rejected messages are kept.
//
// The suffix is the one operators already look for: a queue named after the
// consumer with _error on the end holds what that consumer gave up on.
func ErrorQueue(queue string) string {
	return queue + "_error"
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
	s.channel, s.rejects, s.connection = nil, nil, nil

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
		if messaging.IsRejected(err) {
			s.setAside(ctx, delivery, err)
			return
		}

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

// setAside moves a rejected message to the consumer's error queue.
//
// The copy is confirmed by the broker before the original is acknowledged, so
// a crash in between leaves the message in one queue or both, never in
// neither. A copy in both is harmless: the error queue is read by people, not
// by this consumer.
func (s *Subscriber) setAside(ctx context.Context, delivery amqp.Delivery, reason error) {
	errorQueue := ErrorQueue(s.config.Queue)

	if err := s.publishRejected(ctx, errorQueue, delivery, reason); err != nil {
		s.log.ErrorContext(ctx, "could not set a rejected message aside; it will be delivered again",
			"messageId", delivery.MessageId, "queue", errorQueue, "error", err)

		time.Sleep(redeliveryDelay)
		_ = delivery.Nack(false, true)

		return
	}

	s.log.ErrorContext(ctx, "rejected a message; it was moved to the error queue",
		"messageId", delivery.MessageId, "type", delivery.Type, "queue", errorQueue, "error", reason)

	if err := delivery.Ack(false); err != nil {
		s.log.WarnContext(ctx, "could not acknowledge a rejected message",
			"messageId", delivery.MessageId, "error", err)
	}
}

// publishRejected copies a delivery to the error queue and waits for the
// broker to confirm it.
func (s *Subscriber) publishRejected(
	ctx context.Context, errorQueue string, delivery amqp.Delivery, reason error,
) error {
	headers := amqp.Table{}
	for key, value := range delivery.Headers {
		headers[key] = value
	}
	// Why it was given up on, next to the message itself, so whoever reads
	// the error queue does not have to search the logs for it.
	headers["rejection-reason"] = reason.Error()

	confirmation, err := s.rejects.PublishWithDeferredConfirmWithContext(
		ctx, "", errorQueue, false, false,
		amqp.Publishing{
			MessageId:     delivery.MessageId,
			CorrelationId: delivery.CorrelationId,
			Type:          delivery.Type,
			Timestamp:     delivery.Timestamp,
			ContentType:   delivery.ContentType,
			DeliveryMode:  amqp.Persistent,
			Headers:       headers,
			Body:          delivery.Body,
		},
	)
	if err != nil {
		return fmt.Errorf("publishing to %s: %w", errorQueue, err)
	}

	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("waiting for %s to confirm: %w", errorQueue, err)
	}
	if !acked {
		return fmt.Errorf("rabbitmq refused the copy for %s", errorQueue)
	}

	return nil
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

	version, ok := intHeader(delivery.Headers["schema-version"])
	if !ok {
		return messaging.Envelope{}, errors.New("the message carries no schema version")
	}

	aggregateID, _ := delivery.Headers["aggregate-id"].(string)

	return messaging.Envelope{
		ID:            id,
		Contract:      messaging.Contract{EventType: delivery.Type, Version: version},
		AggregateID:   aggregateID,
		CorrelationID: delivery.CorrelationId,
		OccurredOnUTC: delivery.Timestamp.UTC(),
		Payload:       delivery.Body,
	}, nil
}

// intHeader reads a whole number from an AMQP header.
//
// The wire format has several integer types and a publisher picks whichever
// its library prefers - the same value arrives as a 32-bit integer from one
// client and a 64-bit one from another. Refusing every encoding but our own
// would reject messages that are perfectly well formed.
func intHeader(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(typed)
		return parsed, err == nil
	default:
		return 0, false
	}
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

	// Bound to nothing: only this consumer writes to it, when it gives up on
	// a message, and only a person reads from it.
	if _, err := channel.QueueDeclare(ErrorQueue(s.config.Queue), true, false, false, false, nil); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("declaring queue %s: %w", ErrorQueue(s.config.Queue), err)
	}

	rejects, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("opening rabbitmq channel for rejected messages: %w", err)
	}
	if err := rejects.Confirm(false); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("enabling confirms for rejected messages: %w", err)
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

	s.connection, s.channel, s.rejects = connection, channel, rejects

	return deliveries, nil
}

// compile-time check that the subscriber satisfies the seam.
var _ messaging.Subscriber = (*Subscriber)(nil)
