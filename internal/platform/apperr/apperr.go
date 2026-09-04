// Package apperr classifies errors so the transport layer can map them to a
// status code in one place.
//
// The .NET project throws NotFoundException, BadRequestException and
// InternalServerException, and CustomExceptionHandler turns them into
// ProblemDetails. Go has no exceptions, so the classification travels in the
// error value itself: handlers return an *Error, and the HTTP layer inspects
// its Kind. Everything else - a driver error, a bug - is treated as internal.
package apperr

import (
	"errors"
	"fmt"
)

// Kind is the category an error belongs to.
type Kind int

const (
	// KindInternal is the zero value so an unclassified error is never
	// mistaken for a client error.
	KindInternal Kind = iota
	KindBadRequest
	KindNotFound
	KindConflict
	KindValidation
)

// String names the kind; used as the problem-details title.
func (k Kind) String() string {
	switch k {
	case KindBadRequest:
		return "BadRequest"
	case KindNotFound:
		return "NotFound"
	case KindConflict:
		return "Conflict"
	case KindValidation:
		return "ValidationError"
	default:
		return "InternalServerError"
	}
}

// Error carries a message meant for the caller plus the kind that decides the
// status code. Fields holds per-field messages for validation failures.
type Error struct {
	Kind    Kind
	Message string
	Fields  map[string]string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.cause)
	}
	return e.Message
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// NotFound reports a missing entity. Mirrors the .NET NotFoundException
// constructor that formats the entity name and key.
func NotFound(entity string, key any) *Error {
	return &Error{
		Kind:    KindNotFound,
		Message: fmt.Sprintf("entity %s with key %v was not found", entity, key),
	}
}

// BadRequest reports input the caller can correct.
func BadRequest(format string, args ...any) *Error {
	return &Error{Kind: KindBadRequest, Message: fmt.Sprintf(format, args...)}
}

// Conflict reports a request that clashes with current state.
func Conflict(format string, args ...any) *Error {
	return &Error{Kind: KindConflict, Message: fmt.Sprintf(format, args...)}
}

// Validation reports field-level failures.
func Validation(fields map[string]string) *Error {
	return &Error{
		Kind:    KindValidation,
		Message: "one or more validation errors occurred",
		Fields:  fields,
	}
}

// Internal wraps an unexpected failure. The cause is kept for logs; the
// message is what may be shown to the caller.
func Internal(cause error, format string, args ...any) *Error {
	return &Error{
		Kind:    KindInternal,
		Message: fmt.Sprintf(format, args...),
		cause:   cause,
	}
}

// KindOf reports the kind of err, defaulting to KindInternal for errors that
// were never classified.
func KindOf(err error) Kind {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Kind
	}
	return KindInternal
}

// As extracts the *Error from err, if there is one.
func As(err error) (*Error, bool) {
	var appErr *Error
	ok := errors.As(err, &appErr)
	return appErr, ok
}
