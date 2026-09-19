package mssql

import (
	"errors"
	"fmt"
	"testing"
)

// The refusal as SQL Server actually returns it, captured from a failed
// migration: the agent's error sits inside the capture procedure's error.
const agentStartingRefusal = "mssql: Could not update the metadata that indicates table " +
	"[dbo].[OutboxMessages] is enabled for Change Data Capture. The failure occurred when " +
	"executing the command '[sys].[sp_cdc_add_job] @job_type = N'capture''. The error returned " +
	"was 22836: 'Could not update the metadata for database Ordering to indicate that a Change " +
	"Data Capture job has been added. The failure occurred when executing the command " +
	"'sp_add_jobstep_internal'. The error returned was 14258: 'Cannot perform this operation " +
	"while SQL Server Agent is starting. Try again later.'. (22832)"

func TestTheAgentStartingRefusalIsRecognised(t *testing.T) {
	t.Parallel()

	// Wrapped as goose wraps it, since that is how Migrate sees it.
	err := fmt.Errorf("ERROR 00004_enable_outbox_cdc.sql: failed to run SQL migration: %w",
		errors.New(agentStartingRefusal))

	if !agentStarting(err) {
		t.Error("the server said to try again later; that should be retried")
	}
}

func TestOtherMigrationFailuresAreNotRetried(t *testing.T) {
	t.Parallel()

	// A real fault in a migration must fail at once, not after a minute of
	// retries that cannot help.
	for _, message := range []string{
		"mssql: Invalid object name 'OutboxMessages'. (208)",
		"mssql: There is already an object named 'Customers' in the database. (2714)",
		"mssql: Login failed for user 'sa'. (18456)",
	} {
		if agentStarting(errors.New(message)) {
			t.Errorf("%q was treated as the agent still starting", message)
		}
	}
}
