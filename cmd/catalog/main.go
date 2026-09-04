// Command catalog runs the Catalog service.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/api"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

const defaultAddr = ":8080"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("catalog service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// SIGINT/SIGTERM cancel the context, which drains the HTTP server.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("CATALOG_HTTP_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	// No dependencies registered yet, so /health is a liveness probe until
	// persistence arrives.
	checks := health.NewRegistry()

	return httpx.Run(ctx, addr, api.Router(checks), log)
}
