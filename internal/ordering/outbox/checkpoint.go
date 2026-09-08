package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// DefaultConsumerName is the reader this service runs.
const DefaultConsumerName = "ordering-outbox-worker"

// CheckpointStore remembers how far a reader has got.
//
// The position is stored in the same database as the outbox, so a worker that
// is restarted, replaced or moved picks up exactly where the last one stopped
// rather than replaying everything or skipping ahead.
type CheckpointStore struct {
	db       *sql.DB
	consumer string
}

// NewCheckpointStore wires the store for one named reader.
func NewCheckpointStore(db *sql.DB, consumer string) *CheckpointStore {
	if consumer == "" {
		consumer = DefaultConsumerName
	}

	return &CheckpointStore{db: db, consumer: consumer}
}

// Load returns the stored position, or nil when this reader has never run.
func (s *CheckpointStore) Load(ctx context.Context) (LSN, error) {
	const query = `SELECT LastProcessedLsn FROM OutboxCdcCheckpoints WHERE ConsumerName = @p1`

	var lsn []byte
	err := s.db.QueryRowContext(ctx, query, s.consumer).Scan(&lsn)
	if errors.Is(err, sql.ErrNoRows) {
		// Not an error: a reader with no position starts from the oldest
		// change capture still retains.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading outbox checkpoint for %s: %w", s.consumer, err)
	}

	return lsn, nil
}

// Save records the position.
//
// It is called only after every message up to and including this position has
// been accepted by the broker. Saving earlier would turn a crash into lost
// messages; saving later, as this does, turns it into repeated ones - which
// consumers already handle.
func (s *CheckpointStore) Save(ctx context.Context, lsn LSN) error {
	if lsn.IsZero() {
		return errors.New("refusing to save an empty outbox checkpoint")
	}

	const query = `
		MERGE OutboxCdcCheckpoints WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS ConsumerName) AS source ON target.ConsumerName = source.ConsumerName
		WHEN MATCHED THEN UPDATE SET LastProcessedLsn = @p2, UpdatedOnUtc = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT (ConsumerName, LastProcessedLsn, UpdatedOnUtc)
			VALUES (@p1, @p2, SYSUTCDATETIME());`

	if _, err := s.db.ExecContext(ctx, query, s.consumer, []byte(lsn)); err != nil {
		return fmt.Errorf("saving outbox checkpoint for %s: %w", s.consumer, err)
	}

	return nil
}
