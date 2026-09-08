// Package rabbitmq publishes integration events to RabbitMQ.
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// DefaultExchange is where events are published unless configured otherwise.
const DefaultExchange = "integration-events"

// Config describes how to reach the broker.
type Config struct {
	// URL is an amqp:// address. It carries credentials, so it comes from the
	// environment rather than the code.
	URL string

	// Exchange is a durable topic exchange; the event type is the routing key,
	// so a consumer binds to the contracts it cares about and nothing else.
	Exchange string
}

// Publisher sends envelopes to a RabbitMQ exchange.
//
// The connection is opened on first use and reopened after a failure. A broker
// that is briefly unreachable is normal, and the caller already knows what to
// do with a failed publish: leave the checkpoint where it is and try again.
type Publisher struct {
	config   Config
	registry *messaging.Registry

	mu         sync.Mutex
	connection *amqp.Connection
	channel    *amqp.Channel
}

// New wires the publisher. Nothing is dialled yet.
func New(config Config, registry *messaging.Registry) *Publisher {
	if config.Exchange == "" {
		config.Exchange = DefaultExchange
	}

	return &Publisher{config: config, registry: registry}
}

// Publish sends one envelope and waits for the broker to confirm it.
func (p *Publisher) Publish(ctx context.Context, envelope messaging.Envelope) error {
	// Refused before it is sent: forwarding a contract this build does not know
	// would put a message on the bus that nothing here can explain later.
	if !p.registry.Knows(envelope.Contract) {
		return fmt.Errorf("refusing to publish unknown contract %s", envelope.Contract)
	}

	channel, err := p.openChannel()
	if err != nil {
		return err
	}

	confirmation, err := channel.PublishWithDeferredConfirmWithContext(
		ctx,
		p.config.Exchange,
		envelope.Contract.EventType,
		// Not mandatory: an event nobody has bound a queue to is dropped by
		// the broker, on purpose. A publisher that failed because no consumer
		// existed yet would make deploying a producer before its consumers
		// impossible, and would stall the whole outbox stream behind it.
		false,
		false,
		message(envelope),
	)
	if err != nil {
		p.discardConnection()
		return fmt.Errorf("publishing %s to rabbitmq: %w", envelope.ID, err)
	}

	// Without waiting for the confirm, Publish returns once the bytes reach the
	// socket. The broker could still lose the message, and the reader would
	// already have moved past it.
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		p.discardConnection()
		return fmt.Errorf("waiting for rabbitmq to confirm %s: %w", envelope.ID, err)
	}
	if !acked {
		return fmt.Errorf("rabbitmq rejected message %s", envelope.ID)
	}

	return nil
}

// Close releases the connection.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.connection == nil {
		return nil
	}

	err := p.connection.Close()
	p.channel, p.connection = nil, nil

	if err != nil && !errors.Is(err, amqp.ErrClosed) {
		return fmt.Errorf("closing rabbitmq connection: %w", err)
	}

	return nil
}

// message builds the AMQP message for an envelope.
func message(envelope messaging.Envelope) amqp.Publishing {
	return amqp.Publishing{
		// The id a consumer deduplicates on; delivery is at-least-once.
		MessageId:     envelope.ID.String(),
		CorrelationId: envelope.CorrelationID,
		Type:          envelope.Contract.EventType,
		Timestamp:     envelope.OccurredOnUTC,
		ContentType:   "application/json",
		// Persistent, or a broker restart would drop everything still queued -
		// which would undo the durability the outbox was built for.
		DeliveryMode: amqp.Persistent,
		Headers: amqp.Table{
			"schema-version": envelope.Contract.Version,
			"aggregate-id":   envelope.AggregateID,
		},
		// Forwarded exactly as it was stored: re-encoding it here would be a
		// chance to lose a field this build no longer knows about.
		Body: envelope.Payload,
	}
}

// openChannel returns a usable channel, dialling if there is none.
func (p *Publisher) openChannel() (*amqp.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.channel != nil && !p.channel.IsClosed() {
		return p.channel, nil
	}

	if p.connection != nil {
		_ = p.connection.Close()
		p.channel, p.connection = nil, nil
	}

	connection, err := amqp.Dial(p.config.URL)
	if err != nil {
		return nil, fmt.Errorf("connecting to rabbitmq: %w", err)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("opening rabbitmq channel: %w", err)
	}

	// Durable, so the exchange survives a broker restart along with the
	// messages sitting behind it.
	if err := channel.ExchangeDeclare(
		p.config.Exchange, amqp.ExchangeTopic, true, false, false, false, nil,
	); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("declaring exchange %s: %w", p.config.Exchange, err)
	}

	if err := channel.Confirm(false); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("enabling publisher confirms: %w", err)
	}

	p.connection, p.channel = connection, channel

	return channel, nil
}

// discardConnection drops the connection so the next publish redials rather
// than reusing something already broken.
func (p *Publisher) discardConnection() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.connection != nil {
		_ = p.connection.Close()
	}
	p.channel, p.connection = nil, nil
}

// compile-time check that the publisher satisfies the seam.
var _ messaging.Publisher = (*Publisher)(nil)
