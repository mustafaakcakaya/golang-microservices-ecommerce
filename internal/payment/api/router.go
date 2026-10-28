// Package api wires the Payment service's HTTP routes.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/payment/cards"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
)

// Router builds the service's HTTP handler.
func Router(repo cards.Repository, now func() time.Time, checks *health.Registry, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/health", checks.Handler())

	cards.RegisterRoutes(r, repo, now, log)

	return r
}
