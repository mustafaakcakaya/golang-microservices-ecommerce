package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	mssql "github.com/microsoft/go-mssqldb"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/inbox"
)

// SQL Server's error numbers for a row that is already there.
const (
	uniqueIndexViolation = 2601
	primaryKeyViolation  = 2627
)

// InboxStore records which messages a consumer has handled.
//
// It lives with the rest of this database's code because the table it reads is
// created by the Ordering migrations. What a consumer depends on is the
// inbox.Store interface, not this type.
type InboxStore struct {
	db *sql.DB
}

// NewInboxStore wires the store.
func NewInboxStore(db *sql.DB) *InboxStore {
	return &InboxStore{db: db}
}

// Begin claims a message for a consumer.
//
// The claim is an insert, so two deliveries racing each other cannot both win:
// the primary key decides, and the loser then asks whether the winner has
// finished.
func (s *InboxStore) Begin(ctx context.Context, messageID uuid.UUID, consumer string) (bool, error) {
	const insert = `
		INSERT INTO InboxMessages (MessageId, ConsumerName, ReceivedOnUtc)
		VALUES (@p1, @p2, SYSUTCDATETIME())`

	_, err := s.db.ExecContext(ctx, insert, asGUID(messageID), consumer)
	if err == nil {
		return true, nil
	}
	if !isDuplicateKey(err) {
		return false, fmt.Errorf("claiming message %s for %s: %w", messageID, consumer, err)
	}

	const state = `
		SELECT ProcessedOnUtc FROM InboxMessages
		WHERE MessageId = @p1 AND ConsumerName = @p2`

	var processed *string
	if err := s.db.QueryRowContext(ctx, state, asGUID(messageID), consumer).Scan(&processed); err != nil {
		return false, fmt.Errorf("reading the state of message %s for %s: %w", messageID, consumer, err)
	}

	// Finished before: a duplicate delivery, skip it. Claimed but unfinished:
	// a previous attempt died part way, so run it again. Repeating work is
	// recoverable; skipping it is not.
	return processed == nil, nil
}

// Complete marks the message as handled.
func (s *InboxStore) Complete(ctx context.Context, messageID uuid.UUID, consumer string) error {
	const query = `
		UPDATE InboxMessages SET ProcessedOnUtc = SYSUTCDATETIME()
		WHERE MessageId = @p1 AND ConsumerName = @p2`

	if _, err := s.db.ExecContext(ctx, query, asGUID(messageID), consumer); err != nil {
		return fmt.Errorf("completing message %s for %s: %w", messageID, consumer, err)
	}

	return nil
}

// isDuplicateKey reports whether the row was already there, which is the
// ordinary outcome of a repeated delivery rather than a fault.
func isDuplicateKey(err error) bool {
	var mssqlErr mssql.Error
	if errors.As(err, &mssqlErr) {
		return mssqlErr.Number == uniqueIndexViolation || mssqlErr.Number == primaryKeyViolation
	}

	return false
}

// compile-time check that the store satisfies what consumers depend on.
var _ inbox.Store = (*InboxStore)(nil)
