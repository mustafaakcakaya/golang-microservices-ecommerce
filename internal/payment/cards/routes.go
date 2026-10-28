package cards

import (
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
)

// routeLogger is the logger routes use for unexpected errors.
type routeLogger = *slog.Logger

// RegisterRoutes mounts every card slice on r. now is the clock card expiry is
// judged against; nil means the system clock.
func RegisterRoutes(r chi.Router, repo Repository, now func() time.Time, log *slog.Logger) {
	// The logging middleware records the operation and never the request,
	// which matters more here than anywhere: the request is a card.
	tokenizeCard := cqrs.Chain[TokenizeCardCommand, TokenizeCardResult](
		NewTokenizeCardHandler(repo, now),
		cqrs.Logging[TokenizeCardCommand, TokenizeCardResult](log, "TokenizeCard"),
		cqrs.Validating[TokenizeCardCommand, TokenizeCardResult](),
	)

	r.Route("/payment", func(r chi.Router) {
		r.Post("/tokens", TokenizeCardRoute(tokenizeCard, log))
	})
}
