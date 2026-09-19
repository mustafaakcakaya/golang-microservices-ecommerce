package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// OutboxStore is the recorded checkouts waiting to be published.
type OutboxStore struct {
	pool *pgxpool.Pool
}

// NewOutboxStore wires the store.
func NewOutboxStore(pool *pgxpool.Pool) *OutboxStore {
	return &OutboxStore{pool: pool}
}

// Due returns messages ready to publish, oldest first.
//
// Oldest first so checkouts leave in the order they happened, and a message
// that has been given up on is skipped rather than retried forever.
func (s *OutboxStore) Due(ctx context.Context, limit int, now time.Time) ([]outbox.Message, error) {
	const query = `
		SELECT id, event_type, schema_version, aggregate_id, correlation_id,
		       occurred_on_utc, payload, attempts
		FROM basket_outbox_messages
		WHERE poisoned_on_utc IS NULL AND next_attempt_on_utc <= $1
		ORDER BY occurred_on_utc, id
		LIMIT $2`

	rows, err := s.pool.Query(ctx, query, now, limit)
	if err != nil {
		return nil, fmt.Errorf("reading due checkouts: %w", err)
	}
	defer rows.Close()

	var messages []outbox.Message
	for rows.Next() {
		var (
			message       outbox.Message
			id            uuid.UUID
			eventType     string
			version       int
			aggregateID   string
			correlationID *string
			occurredOn    time.Time
			payload       string
		)

		if err := rows.Scan(&id, &eventType, &version, &aggregateID,
			&correlationID, &occurredOn, &payload, &message.Attempts); err != nil {
			return nil, fmt.Errorf("reading a due checkout: %w", err)
		}

		correlation := ""
		if correlationID != nil {
			correlation = *correlationID
		}

		message.Envelope = messaging.Envelope{
			ID:            id,
			Contract:      messaging.Contract{EventType: eventType, Version: version},
			AggregateID:   aggregateID,
			CorrelationID: correlation,
			OccurredOnUTC: occurredOn.UTC(),
			Payload:       []byte(payload),
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading due checkouts: %w", err)
	}

	return messages, nil
}

// Delete removes a message that has been published.
//
// Deleted rather than marked as sent: the outbox is a queue of work, and the
// record of what was ordered lives in Ordering, which is the service whose job
// that is.
func (s *OutboxStore) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM basket_outbox_messages WHERE id = $1`, id); err != nil {
		return fmt.Errorf("removing published checkout %s: %w", id, err)
	}

	return nil
}

// Fail records a failed attempt and when to try again.
func (s *OutboxStore) Fail(ctx context.Context, id uuid.UUID, reason string, nextAttempt time.Time) error {
	const query = `
		UPDATE basket_outbox_messages
		SET attempts = attempts + 1, last_error = $2, next_attempt_on_utc = $3
		WHERE id = $1`

	if _, err := s.pool.Exec(ctx, query, id, truncate(reason), nextAttempt); err != nil {
		return fmt.Errorf("recording a failed checkout publish %s: %w", id, err)
	}

	return nil
}

// Poison gives up on a message, leaving it as the record of what was skipped.
func (s *OutboxStore) Poison(ctx context.Context, id uuid.UUID, reason string) error {
	const query = `
		UPDATE basket_outbox_messages
		SET attempts = attempts + 1, last_error = $2, poisoned_on_utc = now()
		WHERE id = $1 AND poisoned_on_utc IS NULL`

	if _, err := s.pool.Exec(ctx, query, id, truncate(reason)); err != nil {
		return fmt.Errorf("marking checkout %s poisoned: %w", id, err)
	}

	return nil
}

// truncate bounds what is stored: a driver can produce a very long message,
// and losing the attempt count over it would be worse than losing the tail.
func truncate(reason string) string {
	const maxReason = 2000

	if len(reason) > maxReason {
		return reason[:maxReason]
	}

	return reason
}

// Checkout removes the basket and records the announcement in one transaction.
//
// This is the whole point of the outbox: the basket is emptied and the message
// is certain to go out, or neither happens. Publishing here instead would be
// two writes to two systems - a crash between them either announces a checkout
// whose basket survives, so the customer checks out twice, or clears a basket
// nobody is told about.
func (r *BasketRepository) Checkout(
	ctx context.Context, userName string, announcement messaging.Envelope,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning checkout of %q: %w", userName, err)
	}
	// The rollback error is dropped deliberately: the original failure is what
	// the caller needs.
	defer func() { _ = tx.Rollback(ctx) }()

	deleted, err := tx.Exec(ctx, `DELETE FROM baskets WHERE user_name = $1`, userName)
	if err != nil {
		return fmt.Errorf("clearing basket %q: %w", userName, err)
	}
	if deleted.RowsAffected() == 0 {
		// Someone else checked this basket out first. Their transaction
		// recorded the message, so there is nothing left to do here.
		return apperr.NotFound("Basket", userName)
	}

	if err := recordCheckout(ctx, tx, announcement); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing checkout of %q: %w", userName, err)
	}

	return nil
}

// recordCheckout writes the message on the caller's transaction.
func recordCheckout(ctx context.Context, tx pgx.Tx, announcement messaging.Envelope) error {
	const query = `
		INSERT INTO basket_outbox_messages (
			id, event_type, schema_version, aggregate_id, correlation_id, occurred_on_utc, payload
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		-- Two checkouts of the same basket derive the same id, so the second
		-- one finds the message already recorded rather than adding another.
		ON CONFLICT (id) DO NOTHING`

	_, err := tx.Exec(ctx, query,
		announcement.ID,
		announcement.Contract.EventType,
		announcement.Contract.Version,
		announcement.AggregateID,
		nullable(announcement.CorrelationID),
		announcement.OccurredOnUTC,
		string(announcement.Payload),
	)
	if err != nil {
		return fmt.Errorf("recording checkout %s: %w", announcement.ID, err)
	}

	return nil
}

// nullable stores an absent optional value as absent rather than as an empty
// string.
func nullable(value string) any {
	if value == "" {
		return nil
	}

	return value
}
