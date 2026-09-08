package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	mssqldriver "github.com/microsoft/go-mssqldb"
)

// maxRecordedError bounds what is stored, matching the column.
const maxRecordedError = 2000

// Failure is what is known about a message that would not publish.
type Failure struct {
	Attempts int
	Poisoned bool
}

// FailureStore records publish attempts that failed.
//
// It exists so one bad message cannot stop every message behind it. A broker
// that keeps refusing the same message would otherwise hold the checkpoint in
// place forever, and every order placed afterwards would go unannounced because
// of one that cannot be.
type FailureStore struct {
	db *sql.DB
}

// NewFailureStore wires the store.
func NewFailureStore(db *sql.DB) *FailureStore {
	return &FailureStore{db: db}
}

// Get returns what is known about a message, or a zero Failure if it has never
// failed.
func (s *FailureStore) Get(ctx context.Context, messageID uuid.UUID) (Failure, error) {
	const query = `
		SELECT Attempts, CASE WHEN PoisonedOnUtc IS NULL THEN 0 ELSE 1 END
		FROM OutboxPublishFailures WHERE OutboxMessageId = @p1`

	var failure Failure
	err := s.db.QueryRowContext(ctx, query, guid(messageID)).Scan(&failure.Attempts, &failure.Poisoned)
	if errors.Is(err, sql.ErrNoRows) {
		return Failure{}, nil
	}
	if err != nil {
		return Failure{}, fmt.Errorf("reading publish failures of %s: %w", messageID, err)
	}

	return failure, nil
}

// RecordFailure counts one failed attempt and returns the new total.
func (s *FailureStore) RecordFailure(ctx context.Context, messageID uuid.UUID, reason string) (int, error) {
	if len(reason) > maxRecordedError {
		reason = reason[:maxRecordedError]
	}

	const query = `
		MERGE OutboxPublishFailures WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS OutboxMessageId) AS source
			ON target.OutboxMessageId = source.OutboxMessageId
		WHEN MATCHED THEN UPDATE SET
			Attempts = target.Attempts + 1, LastError = @p2, LastAttemptOnUtc = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN
			INSERT (OutboxMessageId, Attempts, LastError, LastAttemptOnUtc)
			VALUES (@p1, 1, @p2, SYSUTCDATETIME())
		OUTPUT inserted.Attempts;`

	var attempts int
	if err := s.db.QueryRowContext(ctx, query, guid(messageID), reason).Scan(&attempts); err != nil {
		return 0, fmt.Errorf("recording a publish failure of %s: %w", messageID, err)
	}

	return attempts, nil
}

// MarkPoisoned gives up on a message.
//
// The row is the record of what was skipped and why, so giving up is visible
// afterwards rather than being a gap nobody can explain.
func (s *FailureStore) MarkPoisoned(ctx context.Context, messageID uuid.UUID) error {
	const query = `
		UPDATE OutboxPublishFailures SET PoisonedOnUtc = SYSUTCDATETIME()
		WHERE OutboxMessageId = @p1 AND PoisonedOnUtc IS NULL`

	if _, err := s.db.ExecContext(ctx, query, guid(messageID)); err != nil {
		return fmt.Errorf("marking %s poisoned: %w", messageID, err)
	}

	return nil
}

// guid converts an id for SQL Server, whose UNIQUEIDENTIFIER stores the first
// three groups in little-endian order. Passing a raw uuid.UUID writes different
// bytes than it reads back.
func guid(id uuid.UUID) mssqldriver.UniqueIdentifier {
	return mssqldriver.UniqueIdentifier(id)
}
