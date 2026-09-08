package mssql_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// captureTimeout bounds the wait for the capture job. It is generous on
// purpose: the job runs on its own schedule, so the only thing worth asserting
// is that changes do arrive, not how quickly.
const captureTimeout = 90 * time.Second

// countCapturedFor reads the change table for one aggregate.
//
// Errors are returned rather than failed on: immediately after capture is
// enabled the change table's range can be empty, and the function rejects a
// range it cannot serve. That is a "not yet", not a fault.
func countCapturedFor(t *testing.T, aggregateID string) (int, error) {
	t.Helper()

	const query = `
		SELECT count(*)
		FROM cdc.fn_cdc_get_all_changes_dbo_OutboxMessages(
			sys.fn_cdc_get_min_lsn(N'dbo_OutboxMessages'),
			sys.fn_cdc_get_max_lsn(),
			N'all')
		WHERE [__$operation] = 2 AND AggregateId = @p1`

	var count int
	err := sharedDB.QueryRowContext(t.Context(), query, aggregateID).Scan(&count)

	return count, err
}

func TestOutboxInsertsReachTheChangeTable(t *testing.T) {
	repo := newRepository(t)

	customerID := seedCustomer(t, sharedDB)
	productID := seedProduct(t, sharedDB)

	order := newOrder(t, customerID, "ORD_K")
	if err := order.Add(productID, 1, decimal.NewFromInt(75)); err != nil {
		t.Fatalf("adding line: %v", err)
	}
	if err := repo.Save(t.Context(), order); err != nil {
		t.Fatalf("saving: %v", err)
	}

	// Nothing in the application writes to the change table; the capture job
	// reads the transaction log and fills it. This is what the migration buys:
	// the outbox is readable as a stream without anybody polling the table and
	// competing with the writers.
	deadline := time.Now().Add(captureTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		count, err := countCapturedFor(t, order.ID.String())
		lastErr = err

		if err == nil && count == 1 {
			return
		}

		time.Sleep(time.Second)
	}

	t.Fatalf("the outbox insert never reached the change table (last error: %v)", lastErr)
}
