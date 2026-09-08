package outbox

import "fmt"

// CheckpointExpiredError says the stored position is older than what change
// data capture still keeps.
//
// This is the one failure that must never be handled by moving on. Changes
// between the checkpoint and the retention floor have been cleaned up, so
// messages may have been lost - and the only honest response is to stop
// advancing and let a person reconcile against the outbox table, which still
// holds every row.
type CheckpointExpiredError struct {
	Checkpoint  LSN
	MinRetained LSN
}

func (e *CheckpointExpiredError) Error() string {
	return fmt.Sprintf(
		"outbox checkpoint %s is older than the oldest retained change %s; "+
			"capture cleanup may have discarded unpublished messages. Reconcile against "+
			"OutboxMessages, republish what is missing, then reset the checkpoint",
		e.Checkpoint, e.MinRetained)
}

// NotReadyError says capture is not usable yet: either the database has no
// capture enabled or the capture instance has not appeared.
//
// It is expected right after a deployment, when the worker may start before the
// migration that enables capture has run, so it is worth waiting on rather than
// crashing over.
type NotReadyError struct {
	Reason string
}

func (e *NotReadyError) Error() string {
	return "outbox capture is not ready: " + e.Reason
}
