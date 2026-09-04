// Command catalog runs the Catalog service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/api"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/postgres"
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

	databaseURL := os.Getenv("CATALOG_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("CATALOG_DATABASE_URL is required")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("creating database pool: %w", err)
	}
	defer pool.Close()

	// Marten builds its schema on first use; here migrations run at startup
	// so the binary owns its schema the same way.
	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}

	checks := health.NewRegistry()
	checks.Register("postgres", pool.Ping)

	repo := postgres.NewProductRepository(pool)

	return httpx.Run(ctx, addr, api.Router(repo, checks, log), log)
}
