// Package cqrs provides the command/query seam shared by the services.
//
// The .NET project expresses this with MediatR: requests implement ICommand or
// IQuery and a reflection-based dispatcher finds the handler. That indirection
// is not ported. In Go a handler is an ordinary value wired explicitly in main,
// so the call graph stays readable and nothing is resolved at runtime. What is
// kept is the useful half of MediatR - the pipeline - as ordinary middleware.
package cqrs

import "context"

// Handler executes one command or query.
//
// Commands and queries share a shape here; the distinction lives in the naming
// of the concrete types (CreateProductCommand, GetProductsQuery), exactly as it
// does in the .NET code where ICommand and IQuery are both IRequest.
type Handler[In, Out any] interface {
	Handle(ctx context.Context, in In) (Out, error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc[In, Out any] func(ctx context.Context, in In) (Out, error)

// Handle implements Handler.
func (f HandlerFunc[In, Out]) Handle(ctx context.Context, in In) (Out, error) {
	return f(ctx, in)
}

// Middleware wraps a handler with a cross-cutting concern; the Go counterpart
// of a MediatR IPipelineBehavior.
type Middleware[In, Out any] func(HandlerFunc[In, Out]) HandlerFunc[In, Out]

// Chain wraps h so that the first middleware given is the outermost one,
// matching the order behaviours are registered in the .NET pipeline.
func Chain[In, Out any](h Handler[In, Out], middlewares ...Middleware[In, Out]) HandlerFunc[In, Out] {
	next := h.Handle

	for i := len(middlewares) - 1; i >= 0; i-- {
		next = middlewares[i](next)
	}

	return next
}
