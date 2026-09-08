package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// readinessRetryDelay is how long to wait between readiness checks.
const readinessRetryDelay = 3 * time.Second

// readinessQuery names what is missing rather than answering yes or no, so the
// log says which of the two setup steps has not happened.
const readinessQuery = `
	SELECT CASE
		WHEN (SELECT is_cdc_enabled FROM sys.databases WHERE name = DB_NAME()) = 0
			THEN 'the database does not have change data capture enabled'
		WHEN NOT EXISTS (SELECT 1 FROM cdc.change_tables WHERE capture_instance = N'dbo_OutboxMessages')
			THEN 'the capture instance for the outbox table does not exist'
		WHEN sys.fn_cdc_get_max_lsn() IS NULL
			THEN 'the capture job has not recorded a position yet (is SQL Server Agent running?)'
		ELSE 'ready'
	END`

// Readiness waits for change data capture to become usable.
//
// The worker creates no schema of its own: capture is enabled by the Ordering
// migrations. A worker can therefore start before those have run, and waiting
// with a clear reason is more useful than a crash loop that says only that a
// function is missing.
type Readiness struct {
	db  *sql.DB
	log *slog.Logger
}

// NewReadiness wires the check.
func NewReadiness(db *sql.DB, log *slog.Logger) *Readiness {
	return &Readiness{db: db, log: log}
}

// Check reports whether capture is usable right now.
func (r *Readiness) Check(ctx context.Context) error {
	var state string
	if err := r.db.QueryRowContext(ctx, readinessQuery).Scan(&state); err != nil {
		return fmt.Errorf("checking whether capture is ready: %w", err)
	}

	if state != "ready" {
		return &NotReadyError{Reason: state}
	}

	return nil
}

// Wait blocks until capture is usable or the context is cancelled.
func (r *Readiness) Wait(ctx context.Context) error {
	reported := ""

	for {
		err := r.Check(ctx)
		if err == nil {
			r.log.InfoContext(ctx, "change data capture is ready")
			return nil
		}

		// Repeated identical reasons are logged once: the state does not
		// change every three seconds, and a wall of the same line hides it.
		if reason := err.Error(); reason != reported {
			reported = reason
			r.log.WarnContext(ctx, "waiting for change data capture; "+
				"apply the Ordering migrations against this database", "reason", reason)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readinessRetryDelay):
		}
	}
}
