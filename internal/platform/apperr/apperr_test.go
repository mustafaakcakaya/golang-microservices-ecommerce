package apperr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

func TestKindOfDefaultsToInternalForUnclassifiedErrors(t *testing.T) {
	t.Parallel()

	// A driver error or a bug must never be reported as a client error.
	if got := apperr.KindOf(errors.New("connection reset")); got != apperr.KindInternal {
		t.Errorf("kind = %v, want KindInternal", got)
	}
	if got := apperr.KindOf(nil); got != apperr.KindInternal {
		t.Errorf("kind of nil = %v, want KindInternal", got)
	}
}

func TestKindOfSeesThroughWrapping(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("loading product: %w", apperr.NotFound("Product", 7))

	if got := apperr.KindOf(wrapped); got != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound through wrapping", got)
	}
}

func TestInternalKeepsCauseReachable(t *testing.T) {
	t.Parallel()

	cause := errors.New("dial tcp: refused")
	err := apperr.Internal(cause, "saving product")

	if !errors.Is(err, cause) {
		t.Error("cause should stay reachable via errors.Is for logging and debugging")
	}
	if got := err.Error(); got != "saving product: dial tcp: refused" {
		t.Errorf("message = %q, want message and cause", got)
	}
}

func TestNotFoundMessageNamesEntityAndKey(t *testing.T) {
	t.Parallel()

	err := apperr.NotFound("Product", "abc")

	if got := err.Error(); got != "entity Product with key abc was not found" {
		t.Errorf("message = %q", got)
	}
}

func TestValidationCarriesFieldMessages(t *testing.T) {
	t.Parallel()

	err := apperr.Validation(map[string]string{"Name": "is required"})

	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatal("As should extract the *Error")
	}
	if appErr.Kind != apperr.KindValidation {
		t.Errorf("kind = %v, want KindValidation", appErr.Kind)
	}
	if appErr.Fields["Name"] != "is required" {
		t.Errorf("fields = %v, want the per-field message", appErr.Fields)
	}
}

func TestKindNamesMatchProblemTitles(t *testing.T) {
	t.Parallel()

	cases := map[apperr.Kind]string{
		apperr.KindInternal:   "InternalServerError",
		apperr.KindBadRequest: "BadRequest",
		apperr.KindNotFound:   "NotFound",
		apperr.KindConflict:   "Conflict",
		apperr.KindValidation: "ValidationError",
	}

	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}
