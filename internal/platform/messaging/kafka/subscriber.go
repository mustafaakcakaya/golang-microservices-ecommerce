package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// SubscriberConfig describes what to consume and from where.
type SubscriberConfig struct {
	Config

	// Group is the consumer group. Instances sharing a group divide the
	// partitions between them; a second group gets its own copy of everything.
	Group string

	// Contracts are the event types to read, one topic each.
	Contracts []messaging.Contract
}

// Subscriber consumes integration events from Kafka.
type Subscriber struct {
	config SubscriberConfig
	log    *slog.Logger
	reader *kafkago.Reader
}

// NewSubscriber wires the subscriber.
func NewSubscriber(config SubscriberConfig, log *slog.Logger) *Subscriber {
	topics := make([]string, 0, len(config.Contracts))
	for _, contract := range config.Contracts {
		topics = append(topics, config.Topic(contract.EventType))
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     config.Brokers,
		GroupID:     config.Group,
		GroupTopics: topics,
		// From the beginning for a group that has never run: a producer that
		// published before this consumer existed is the normal case for an
		// event stream, and skipping that history would silently lose orders.
		StartOffset: kafkago.FirstOffset,
	})

	return &Subscriber{config: config, log: log, reader: reader}
}

// Subscribe consumes until the context is cancelled.
//
// Offsets are committed after the handler succeeds, never before. Committing
// first would mean a crash in the handler loses the message, which is the one
// failure this design refuses; committing after means it arrives again, which
// the inbox absorbs.
func (s *Subscriber) Subscribe(ctx context.Context, deliver messaging.Delivery) error {
	if s.config.Group == "" {
		return errors.New("a subscriber needs a consumer group")
	}
	if len(s.config.Contracts) == 0 {
		return errors.New("a subscriber needs at least one contract to read")
	}

	s.log.InfoContext(ctx, "listening for integration events",
		"group", s.config.Group, "brokers", s.config.Brokers)

	for {
		message, err := s.reader.FetchMessage(ctx)
		// Checked before the error: a cancelled fetch is a shutdown, which is
		// a clean stop rather than a failure.
		if stopped(ctx) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading from kafka: %w", err)
		}

		envelope, err := envelopeFrom(message)
		if err != nil {
			// Unreadable messages are committed past rather than retried:
			// nothing about them will improve, and a partition cannot move on
			// while one message is stuck at its head.
			s.log.ErrorContext(ctx, "skipping an unreadable message",
				"topic", message.Topic, "offset", message.Offset, "error", err)
		} else if err := deliver(ctx, envelope); err != nil {
			s.log.ErrorContext(ctx, "handling a message failed; it will be read again",
				"messageId", envelope.ID, "contract", envelope.Contract.String(), "error", err)

			// Not committed, so the group re-reads from here. The pause keeps a
			// permanently failing message from becoming a hot loop.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}

			continue
		}

		err = s.reader.CommitMessages(ctx, message)
		if stopped(ctx) {
			return nil
		}
		if err != nil {
			// The work is done but the offset was not recorded, so the message
			// arrives again. That is what the consumer's inbox is for.
			s.log.WarnContext(ctx, "could not commit an offset",
				"topic", message.Topic, "offset", message.Offset, "error", err)
		}
	}
}

// Close releases the reader.
func (s *Subscriber) Close() error {
	if err := s.reader.Close(); err != nil {
		return fmt.Errorf("closing kafka reader: %w", err)
	}

	return nil
}

// stopped reports whether the subscriber has been asked to shut down.
func stopped(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// envelopeFrom rebuilds the envelope the publisher sent. Kafka has no message
// metadata of its own, so everything travels in headers.
func envelopeFrom(message kafkago.Message) (messaging.Envelope, error) {
	header := func(key string) string {
		for _, h := range message.Headers {
			if h.Key == key {
				return string(h.Value)
			}
		}
		return ""
	}

	id, err := uuid.Parse(header(MessageIDHeader))
	if err != nil {
		return messaging.Envelope{}, fmt.Errorf("the %s header is not a uuid: %w", MessageIDHeader, err)
	}

	eventType := header(EventTypeHeader)
	if eventType == "" {
		return messaging.Envelope{}, fmt.Errorf("the message carries no %s header", EventTypeHeader)
	}

	version, err := strconv.Atoi(header(SchemaVersionHeader))
	if err != nil {
		return messaging.Envelope{}, fmt.Errorf("the %s header is not a number: %w", SchemaVersionHeader, err)
	}

	occurredOn, err := time.Parse(time.RFC3339Nano, header(OccurredOnHeader))
	if err != nil {
		return messaging.Envelope{}, fmt.Errorf("the %s header is not a timestamp: %w", OccurredOnHeader, err)
	}

	return messaging.Envelope{
		ID:            id,
		Contract:      messaging.Contract{EventType: eventType, Version: version},
		AggregateID:   string(message.Key),
		CorrelationID: header(CorrelationIDHeader),
		OccurredOnUTC: occurredOn.UTC(),
		Payload:       message.Value,
	}, nil
}

// compile-time check that the subscriber satisfies the seam.
var _ messaging.Subscriber = (*Subscriber)(nil)
