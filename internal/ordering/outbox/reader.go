package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	mssqldriver "github.com/microsoft/go-mssqldb"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// CaptureInstance is the name the migration gave the capture instance. It is a
// fixed identifier baked into the function names below, never user input.
const CaptureInstance = "dbo_OutboxMessages"

// Group is every outbox message written by one source transaction.
//
// The checkpoint only ever moves to a group boundary, so a transaction is
// either published in full or retried in full. Half of a transaction reaching
// other services is worse than none of it: they would see an order created
// without the update that immediately corrected it.
type Group struct {
	LSN      LSN
	Messages []messaging.Envelope
}

// Batch is what one read returned.
type Batch struct {
	Groups []Group
}

// MessageCount is the number of messages across every group.
func (b Batch) MessageCount() int {
	count := 0
	for _, group := range b.Groups {
		count += len(group.Messages)
	}

	return count
}

// Reader reads committed outbox inserts from the change table.
type Reader struct {
	db *sql.DB
}

// NewReader wires the reader.
func NewReader(db *sql.DB) *Reader {
	return &Reader{db: db}
}

// changeColumns are the outbox columns as the change table exposes them,
// alongside the LSN of the transaction that wrote the row.
const changeColumns = `[__$start_lsn], [Id], [EventType], [SchemaVersion], ` +
	`[AggregateId], [CorrelationId], [OccurredOnUtc], [Payload]`

// Read returns committed outbox inserts newer than after, oldest first.
//
// Passing no position starts from the oldest change still retained, not from
// the beginning of time: capture keeps a window, and pretending otherwise would
// silently skip whatever fell out of it.
func (r *Reader) Read(ctx context.Context, after LSN, batchSize int) (Batch, error) {
	if batchSize <= 0 {
		return Batch{}, fmt.Errorf("batch size must be positive, got %d", batchSize)
	}

	minLSN, maxLSN, next, err := r.bounds(ctx, after)
	if err != nil {
		return Batch{}, err
	}

	if maxLSN.IsZero() {
		// Enabling capture is not the same as capture having run: the server
		// has no position to report until its capture job has processed the
		// log at least once.
		return Batch{}, &NotReadyError{
			Reason: "the server reports no maximum LSN yet; either capture is not enabled on this " +
				"database or its capture job has not recorded a position (is SQL Server Agent running?)",
		}
	}
	if minLSN.IsZero() {
		return Batch{}, &NotReadyError{
			Reason: fmt.Sprintf("capture instance %s has no minimum LSN; it does not exist, "+
				"or its capture job has never run (is SQL Server Agent running?)", CaptureInstance),
		}
	}

	if !after.IsZero() && next.Compare(minLSN) < 0 {
		return Batch{}, &CheckpointExpiredError{Checkpoint: after, MinRetained: minLSN}
	}

	from := next
	if after.IsZero() {
		from = minLSN
	}
	if from.Compare(maxLSN) > 0 {
		return Batch{}, nil
	}

	rows, err := r.changes(ctx, from, maxLSN, batchSize+1)
	if err != nil {
		return Batch{}, err
	}
	if len(rows) == 0 {
		return Batch{}, nil
	}

	// One row over the limit was read so a group cut in half by the limit can
	// be recognised. It is dropped rather than published in part.
	if len(rows) > batchSize {
		lastLSN := rows[len(rows)-1].lsn

		kept := rows[:0:0]
		for _, row := range rows {
			if row.lsn.Compare(lastLSN) != 0 {
				kept = append(kept, row)
			}
		}
		rows = kept

		if len(rows) == 0 {
			// One transaction wrote more messages than a batch holds. It is
			// fetched whole rather than split, because the checkpoint has
			// nowhere to stop inside it.
			rows, err = r.changes(ctx, lastLSN, lastLSN, 0)
			if err != nil {
				return Batch{}, err
			}
		}
	}

	return Batch{Groups: groupByLSN(rows)}, nil
}

// bounds asks the server what it still retains and what the next position after
// the checkpoint would be.
func (r *Reader) bounds(ctx context.Context, after LSN) (minLSN, maxLSN, next LSN, err error) {
	const query = `
		SELECT
			sys.fn_cdc_get_min_lsn(@p1),
			sys.fn_cdc_get_max_lsn(),
			sys.fn_cdc_increment_lsn(CAST(@p2 AS BINARY(10)))`

	var checkpoint any
	if !after.IsZero() {
		checkpoint = []byte(after)
	}

	// Scanned as plain byte slices: any of the three is NULL when capture is
	// not running yet, and database/sql will not store a NULL into a named
	// slice type.
	var minBytes, maxBytes, nextBytes []byte

	err = r.db.QueryRowContext(ctx, query, CaptureInstance, checkpoint).
		Scan(&minBytes, &maxBytes, &nextBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading capture bounds: %w", err)
	}

	return minBytes, maxBytes, nextBytes, nil
}

// changeRow is one captured insert.
type changeRow struct {
	lsn     LSN
	message messaging.Envelope
}

// changes reads the change table between two positions. A limit of zero reads
// everything in the range.
func (r *Reader) changes(ctx context.Context, from, to LSN, limit int) ([]changeRow, error) {
	top := ""
	args := []any{[]byte(from), []byte(to)}
	if limit > 0 {
		top = "TOP (@p3) "
		args = append(args, limit)
	}

	// The capture instance is a constant, so the only interpolation here is a
	// name this package owns; the positions are bound parameters.
	query := fmt.Sprintf(`
		SELECT %s%s
		FROM cdc.fn_cdc_get_all_changes_%s(CAST(@p1 AS BINARY(10)), CAST(@p2 AS BINARY(10)), N'all')
		WHERE [__$operation] = 2
		ORDER BY [__$start_lsn], [__$seqval]`, top, changeColumns, CaptureInstance)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading captured outbox changes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var captured []changeRow
	for rows.Next() {
		row, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		captured = append(captured, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading captured outbox changes: %w", err)
	}

	return captured, nil
}

func scanChange(rows *sql.Rows) (changeRow, error) {
	var (
		lsn []byte
		// The driver's own type converts SQL Server's mixed-endian
		// UNIQUEIDENTIFIER; scanning straight into a uuid.UUID silently
		// reverses the first three groups.
		id            mssqldriver.UniqueIdentifier
		eventType     string
		version       int
		aggregateID   string
		correlationID *string
		occurredOn    time.Time
		payload       string
	)

	if err := rows.Scan(&lsn, &id, &eventType, &version, &aggregateID, &correlationID, &occurredOn, &payload); err != nil {
		return changeRow{}, fmt.Errorf("reading a captured outbox change: %w", err)
	}

	correlation := ""
	if correlationID != nil {
		correlation = *correlationID
	}

	return changeRow{
		lsn: lsn,
		message: messaging.Envelope{
			ID:            uuid.UUID(id),
			Contract:      messaging.Contract{EventType: eventType, Version: version},
			AggregateID:   aggregateID,
			CorrelationID: correlation,
			OccurredOnUTC: occurredOn.UTC(),
			Payload:       []byte(payload),
		},
	}, nil
}

// groupByLSN collects consecutive rows that share a transaction.
func groupByLSN(rows []changeRow) []Group {
	var groups []Group

	for _, row := range rows {
		last := len(groups) - 1
		if last >= 0 && groups[last].LSN.Compare(row.lsn) == 0 {
			groups[last].Messages = append(groups[last].Messages, row.message)
			continue
		}

		groups = append(groups, Group{LSN: row.lsn, Messages: []messaging.Envelope{row.message}})
	}

	return groups
}
