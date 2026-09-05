// Package api wires the Catalog service's HTTP routes.
//
// Routes are registered feature by feature rather than in one central table,
// each slice owns its
// endpoint next to its handler.
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/products"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
)

// Router builds the service's HTTP handler.
func Router(repo products.Repository, checks *health.Registry, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/health", checks.Handler())

	products.RegisterRoutes(r, repo, log)

	return r
}
