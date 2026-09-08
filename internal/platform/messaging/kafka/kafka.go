// Package kafka publishes integration events to Kafka.
package kafka

import (
	"context"
	"fmt"
	"strconv"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// Header names carried alongside the payload. Kafka has no message id of its
// own, so the envelope's metadata travels here.
const (
	MessageIDHeader     = "message-id"
	EventTypeHeader     = "event-type"
	SchemaVersionHeader = "schema-version"
	CorrelationIDHeader = "correlation-id"
	OccurredOnHeader    = "occurred-on-utc"
)

// Config describes how to reach the cluster.
type Config struct {
	// Brokers are the bootstrap addresses.
	Brokers []string

	// TopicPrefix is prepended to the event type to form the topic name, so one
	// cluster can carry several environments.
	TopicPrefix string

	// WriteTimeout bounds a single produce call.
	WriteTimeout time.Duration
}

// Topic is where an event type is published.
func (c Config) Topic(eventType string) string {
	return c.TopicPrefix + eventType
}

// Publisher sends envelopes to Kafka.
type Publisher struct {
	config   Config
	registry *messaging.Registry
	writer   *kafkago.Writer
}

// New wires the publisher.
func New(config Config, registry *messaging.Registry) *Publisher {
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 30 * time.Second
	}

	writer := &kafkago.Writer{
		Addr: kafkago.TCP(config.Brokers...),
		// The key decides the partition, so the balancer has to be one that
		// looks at it. The default spreads messages round-robin, which would
		// scatter one aggregate's events across partitions and lose their
		// order - the one ordering guarantee Kafka does give.
		Balancer: &kafkago.Hash{},
		// Every in-sync replica must have the message before the write is
		// acknowledged. Anything weaker can lose a write the leader already
		// confirmed, and the reader would have moved past it.
		RequiredAcks: kafkago.RequireAll,
		// Synchronous: WriteMessages must not return before the cluster has
		// the message, or the checkpoint would advance on a promise.
		Async:        false,
		WriteTimeout: config.WriteTimeout,
		// Convenient locally. A deployed cluster usually creates topics ahead
		// of time with a chosen partition count and retention.
		AllowAutoTopicCreation: true,
	}

	return &Publisher{config: config, registry: registry, writer: writer}
}

// Publish sends one envelope and waits for the cluster to acknowledge it.
func (p *Publisher) Publish(ctx context.Context, envelope messaging.Envelope) error {
	if !p.registry.Knows(envelope.Contract) {
		return fmt.Errorf("refusing to publish unknown contract %s", envelope.Contract)
	}

	headers := []kafkago.Header{
		// What a consumer deduplicates on; delivery is at-least-once.
		{Key: MessageIDHeader, Value: []byte(envelope.ID.String())},
		{Key: EventTypeHeader, Value: []byte(envelope.Contract.EventType)},
		{Key: SchemaVersionHeader, Value: []byte(strconv.Itoa(envelope.Contract.Version))},
		{Key: OccurredOnHeader, Value: []byte(envelope.OccurredOnUTC.Format(time.RFC3339Nano))},
	}
	if envelope.CorrelationID != "" {
		headers = append(headers, kafkago.Header{
			Key: CorrelationIDHeader, Value: []byte(envelope.CorrelationID),
		})
	}

	message := kafkago.Message{
		Topic: p.config.Topic(envelope.Contract.EventType),
		// One aggregate's events land on one partition and stay in order.
		Key: []byte(envelope.AggregateID),
		// Forwarded exactly as stored, like every other publisher.
		Value:   envelope.Payload,
		Headers: headers,
	}

	if err := p.writer.WriteMessages(ctx, message); err != nil {
		return fmt.Errorf("publishing %s to kafka: %w", envelope.ID, err)
	}

	return nil
}

// Close releases the writer.
func (p *Publisher) Close() error {
	if err := p.writer.Close(); err != nil {
		return fmt.Errorf("closing kafka writer: %w", err)
	}

	return nil
}

// compile-time check that the publisher satisfies the seam.
var _ messaging.Publisher = (*Publisher)(nil)
