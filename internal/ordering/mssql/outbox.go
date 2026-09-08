package mssql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// EventMapper turns a domain event into the message that leaves the service.
//
// It is a parameter rather than a fixed call so persistence stays unaware of
// which contracts exist: the mapping is a decision about what this service
// promises other services, and it is made where the service is wired.
//
// The bool is false for a domain event with no outward contract.
type EventMapper func(ctx context.Context, event domain.Event) (messaging.Envelope, bool, error)

// DropEvents maps nothing. Seeding uses it: filling a fresh database is not a
// business change, and announcing sample orders to other services would be.
func DropEvents(context.Context, domain.Event) (messaging.Envelope, bool, error) {
	return messaging.Envelope{}, false, nil
}

// stageEvents writes the outbox rows for the events an aggregate raised.
//
// It runs on the caller's transaction, which is the whole point: the row that
// says an order was placed and the order itself are committed together, so no
// crash can leave one without the other.
func (r *OrderRepository) stageEvents(ctx context.Context, tx *sql.Tx, events []domain.Event) error {
	for _, event := range events {
		envelope, mapped, err := r.mapEvent(ctx, event)
		if err != nil {
			return fmt.Errorf("mapping domain event: %w", err)
		}
		if !mapped {
			continue
		}

		if err := insertOutboxMessage(ctx, tx, envelope); err != nil {
			return err
		}
	}

	return nil
}

func insertOutboxMessage(ctx context.Context, tx *sql.Tx, envelope messaging.Envelope) error {
	const query = `
		INSERT INTO OutboxMessages (
			Id, EventType, SchemaVersion, AggregateId, CorrelationId, OccurredOnUtc, Payload
		) VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7)`

	_, err := tx.ExecContext(ctx, query,
		asGUID(envelope.ID),
		envelope.Contract.EventType,
		envelope.Contract.Version,
		envelope.AggregateID,
		nullable(envelope.CorrelationID),
		envelope.OccurredOnUTC,
		string(envelope.Payload),
	)
	if err != nil {
		return fmt.Errorf("staging outbox message %s: %w", envelope.ID, err)
	}

	return nil
}
