// Command gateway runs the API gateway.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/gateway"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

const defaultAddr = ":8080"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("gateway stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("GATEWAY_HTTP_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	routes, err := routesFromEnv()
	if err != nil {
		return err
	}

	limiter := gateway.NewFixedWindow(intFromEnv("GATEWAY_RATE_LIMIT", gateway.DefaultLimit),
		durationFromEnv("GATEWAY_RATE_WINDOW", gateway.DefaultWindow))

	// No dependency checks: the gateway is still doing its job when a service
	// behind it is down, and it says so per request with a 502.
	checks := health.NewRegistry()

	router, err := gateway.Router(routes, limiter, checks, log)
	if err != nil {
		return err
	}

	for _, route := range routes {
		log.Info("routing", "prefix", route.Prefix, "target", route.Target.String())
	}

	return httpx.Run(ctx, addr, router, log)
}

// routesFromEnv reads where each service lives.
//
// A service with no address configured is left unrouted rather than guessed
// at, and a gateway with nothing to route refuses to start.
func routesFromEnv() ([]gateway.Route, error) {
	wanted := []struct {
		prefix   string
		variable string
	}{
		{"/catalog-service", "GATEWAY_CATALOG_URL"},
		{"/basket-service", "GATEWAY_BASKET_URL"},
		{"/ordering-service", "GATEWAY_ORDERING_URL"},
	}

	var routes []gateway.Route
	for _, entry := range wanted {
		raw := os.Getenv(entry.variable)
		if raw == "" {
			continue
		}

		target, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s is not a url: %w", entry.variable, err)
		}
		if target.Scheme == "" || target.Host == "" {
			return nil, fmt.Errorf("%s must include a scheme and a host, got %q", entry.variable, raw)
		}

		routes = append(routes, gateway.Route{Prefix: entry.prefix, Target: target})
	}

	return routes, nil
}

func intFromEnv(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}

	return value
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(name))
	if err != nil {
		return fallback
	}

	return value
}
