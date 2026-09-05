package cqrs

import (
	"context"
	"log/slog"
	"time"
)

// SlowThreshold is the duration past which a handled request is reported as
// slow.
const SlowThreshold = 3 * time.Second

// Logging records the start, outcome and duration of each handled request.
//
// The request body is deliberately never logged: request types carry user
// input and, in Ordering, payment fields. Only the operation name is.
func Logging[In, Out any](log *slog.Logger, operation string) Middleware[In, Out] {
	return func(next HandlerFunc[In, Out]) HandlerFunc[In, Out] {
		return func(ctx context.Context, in In) (Out, error) {
			log.DebugContext(ctx, "handling request", "operation", operation)

			start := time.Now()
			out, err := next(ctx, in)
			elapsed := time.Since(start)

			switch {
			case err != nil:
				log.ErrorContext(ctx, "request failed",
					"operation", operation, "duration", elapsed, "error", err)
			case elapsed > SlowThreshold:
				log.WarnContext(ctx, "slow request",
					"operation", operation, "duration", elapsed)
			default:
				log.DebugContext(ctx, "request handled",
					"operation", operation, "duration", elapsed)
			}

			return out, err
		}
	}
}
