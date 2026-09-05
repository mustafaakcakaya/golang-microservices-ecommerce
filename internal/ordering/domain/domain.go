// Package domain holds the Ordering service's aggregates, value objects and
// domain events.
//
// Nothing here may import a database driver, an HTTP router or a message
// broker: the rules live above the machinery that stores or transports them.
// The linter enforces that boundary, so a stray import fails the build rather
// than quietly eroding it.
package domain

import (
	"fmt"
	"time"
)

// Error is a rule violation raised by the domain. The application layer maps it
// to a client-facing status; the domain itself has no opinion about transports.
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }

// invalidf builds a rule violation.
func invalidf(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// Audit records who touched an entity and when.
//
// Persistence fills these in; the domain only carries them so the fields travel
// with the entity they describe.
type Audit struct {
	CreatedAt      *time.Time
	CreatedBy      *string
	LastModifiedAt *time.Time
	LastModifiedBy *string
}
