package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// Problem is an RFC 7807 problem details body, the same shape the .NET
// CustomExceptionHandler produces so clients see one error format across
// both implementations.
type Problem struct {
	Title   string            `json:"title"`
	Detail  string            `json:"detail"`
	Status  int               `json:"status"`
	TraceID string            `json:"traceId,omitempty"`
	Errors  map[string]string `json:"validationErrors,omitempty"`
}

// JSON writes v with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error maps a classified error to a problem response. Internal errors are
// logged with their cause but reported to the caller without it, so driver
// messages and stack details never leak to clients.
func Error(ctx context.Context, w http.ResponseWriter, log *slog.Logger, err error) {
	kind := apperr.KindOf(err)
	status := statusFor(kind)

	problem := Problem{
		Title:   kind.String(),
		Status:  status,
		TraceID: middleware.GetReqID(ctx),
	}

	if appErr, ok := apperr.As(err); ok && kind != apperr.KindInternal {
		problem.Detail = appErr.Message
		problem.Errors = appErr.Fields
	} else {
		problem.Detail = "an unexpected error occurred"
		log.ErrorContext(ctx, "request failed", "error", err, "traceId", problem.TraceID)
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}

func statusFor(kind apperr.Kind) int {
	switch kind {
	case apperr.KindBadRequest, apperr.KindValidation:
		return http.StatusBadRequest
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
