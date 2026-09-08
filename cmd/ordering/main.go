// Command ordering runs the Ordering service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/api"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

const defaultAddr = ":8080"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("ordering service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// SIGINT/SIGTERM cancel the context, which drains the HTTP server.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("ORDERING_HTTP_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	// The connection string carries the password, so it comes from the
	// environment; nothing about it is compiled into the binary.
	databaseURL := os.Getenv("ORDERING_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("ORDERING_DATABASE_URL is required")
	}

	// A fresh server has only its system databases, so the service creates its
	// own before migrating into it.
	if err := mssql.EnsureDatabase(ctx, databaseURL); err != nil {
		return err
	}

	db, err := mssql.Open(databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Migrations run at startup so the binary owns its schema.
	if err := mssql.Migrate(ctx, db); err != nil {
		return err
	}

	// Seeding is an explicit opt-in so nothing depends on guessing where the
	// binary runs.
	if os.Getenv("ORDERING_SEED") == "true" {
		seeded, err := mssql.Seed(ctx, db)
		if err != nil {
			return err
		}
		log.Info("ordering seeding finished", "inserted", seeded)
	}

	checks := health.NewRegistry()
	checks.Register("sqlserver", db.PingContext)

	repo := mssql.NewOrderRepository(db)

	return httpx.Run(ctx, addr, api.Router(repo, checks, log), log)
}
