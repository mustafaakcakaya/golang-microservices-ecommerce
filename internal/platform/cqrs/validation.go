package cqrs

import (
	"context"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/validation"
)

// Validating rejects invalid input before the handler runs. Placing it in the
// pipeline rather than inside each handler means a new slice cannot forget to
// validate: it is applied where routes are registered.
func Validating[In, Out any]() Middleware[In, Out] {
	return func(next HandlerFunc[In, Out]) HandlerFunc[In, Out] {
		return func(ctx context.Context, in In) (Out, error) {
			if err := validation.Struct(in); err != nil {
				var zero Out
				return zero, err
			}

			return next(ctx, in)
		}
	}
}
