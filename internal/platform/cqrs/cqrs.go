// Package cqrs provides the command/query seam shared by the services.
//
// There is no dispatcher: a handler is an ordinary value wired explicitly in
// main, so the call graph is readable and nothing is resolved at runtime.
// Cross-cutting concerns are ordinary middleware around that handler.
package cqrs

import "context"

// Handler executes one command or query.
//
// Commands and queries share a shape here; the distinction lives in the naming
// of the concrete types (CreateProductCommand, GetProductsQuery) and in which
// middleware they are wrapped with.
type Handler[In, Out any] interface {
	Handle(ctx context.Context, in In) (Out, error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc[In, Out any] func(ctx context.Context, in In) (Out, error)

// Handle implements Handler.
func (f HandlerFunc[In, Out]) Handle(ctx context.Context, in In) (Out, error) {
	return f(ctx, in)
}

// Middleware wraps a handler with a cross-cutting concern such as logging or
// validation.
type Middleware[In, Out any] func(HandlerFunc[In, Out]) HandlerFunc[In, Out]

// Chain wraps h so that the first middleware given is the outermost one, which
// makes the written order match the execution order.
func Chain[In, Out any](h Handler[In, Out], middlewares ...Middleware[In, Out]) HandlerFunc[In, Out] {
	next := h.Handle

	for i := len(middlewares) - 1; i >= 0; i-- {
		next = middlewares[i](next)
	}

	return next
}
