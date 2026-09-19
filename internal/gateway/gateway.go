// Package gateway routes public traffic to the services behind it.
//
// It is the one address a client needs: the services keep their own ports for
// local work, but nothing outside has to know them, and cross-cutting concerns
// that belong at the edge - rate limiting here - are applied in one place
// rather than repeated in every service.
package gateway

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// Route sends everything under a prefix to one service.
type Route struct {
	// Prefix is the public path, for example "/catalog-service".
	Prefix string

	// Target is where the service actually listens.
	Target *url.URL
}

// Router builds the gateway's handler.
func Router(routes []Route, limiter *FixedWindow, checks *health.Registry, log *slog.Logger) (http.Handler, error) {
	if len(routes) == 0 {
		return nil, errors.New("the gateway needs at least one route")
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	// Outside the rate limit on purpose. A health probe that gets turned away
	// while the gateway is busy would take it out of the load balancer for
	// being under load, which is when it is needed most. It also reports on
	// the gateway itself, not on what it routes to, so one failing service
	// does not take the gateway down with it.
	r.Get("/health", checks.Handler())

	var routeErr error

	r.Group(func(r chi.Router) {
		// Only proxied traffic is limited, and it is turned away at the edge
		// rather than forwarded to a service first.
		r.Use(limiter.Middleware(log))

		for _, route := range routes {
			if route.Prefix == "" || route.Target == nil {
				routeErr = fmt.Errorf("route %q has no prefix or no target", route.Prefix)
				return
			}

			proxy := proxyFor(route, log)

			// Both forms, so "/catalog-service" and "/catalog-service/products"
			// reach the same service.
			r.Handle(route.Prefix, proxy)
			r.Handle(route.Prefix+"/*", proxy)
		}
	})

	if routeErr != nil {
		return nil, routeErr
	}

	return r, nil
}

// proxyFor forwards a route's traffic to its service.
func proxyFor(route Route, log *slog.Logger) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(route.Target)

			// The prefix is the gateway's own addressing and means nothing to
			// the service: /catalog-service/products is /products to Catalog.
			pr.Out.URL.Path = stripPrefix(pr.In.URL.Path, route.Prefix)
			pr.Out.URL.RawPath = ""

			// Records the caller's address for the service, and drops any
			// forwarding headers the caller sent themselves.
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// An unreachable service is not the caller's mistake, and the
			// reason belongs in the logs rather than in the response.
			log.ErrorContext(r.Context(), "a service behind the gateway could not be reached",
				"prefix", route.Prefix, "target", route.Target.String(), "error", err)

			problem(w, r, http.StatusBadGateway, "BadGateway",
				"the service behind this route is unavailable")
		},
	}
}

// stripPrefix removes the gateway's prefix, always leaving an absolute path.
func stripPrefix(path, prefix string) string {
	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}

	return trimmed
}

// problem writes the same error shape the services use, so a client sees one
// format whether the gateway or a service answered.
func problem(w http.ResponseWriter, r *http.Request, status int, title, detail string) {
	body := httpx.Problem{
		Title:   title,
		Detail:  detail,
		Status:  status,
		TraceID: middleware.GetReqID(r.Context()),
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = writeJSON(w, body)
}
