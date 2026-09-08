// Command ordering-worker publishes the Ordering service's outbox.
//
// It is a separate process from the API on purpose. Publishing is a background
// activity whose pace has nothing to do with a request, and keeping it out of
// the API means a broker outage slows down publishing rather than order taking.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/integration"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/broker"
)

const defaultAddr = ":8080"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("ordering worker stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("ORDERING_WORKER_HTTP_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	// Both the connection string and the broker address carry credentials, so
	// both come from the environment.
	databaseURL := os.Getenv("ORDERING_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("ORDERING_DATABASE_URL is required")
	}

	brokerConfig, err := broker.ConfigFromEnv()
	if err != nil {
		return err
	}

	options, err := outbox.OptionsFromEnv()
	if err != nil {
		return err
	}

	db, err := mssql.Open(databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// The worker owns no schema: capture and its tables are created by the
	// Ordering migrations, and it waits for them rather than creating them.
	publisher, err := broker.New(brokerConfig, integration.Registry())
	if err != nil {
		return err
	}
	defer func() { _ = publisher.Close() }()

	readiness := outbox.NewReadiness(db, log)
	processor := outbox.NewProcessor(
		outbox.NewReader(db),
		outbox.NewCheckpointStore(db, options.ConsumerName),
		outbox.NewFailureStore(db),
		publisher,
		options,
		log,
	)
	worker := outbox.NewWorker(processor, readiness, options, log)

	checks := health.NewRegistry()
	checks.Register("sqlserver", db.PingContext)
	checks.Register("capture", readiness.Check)
	// A worker that is running but publishing nothing is exactly what a
	// liveness check misses, so it reports on its own progress too.
	checks.Register("publishing", worker.Health)

	log.Info("ordering worker configured", "broker", brokerConfig.Provider, "consumer", options.ConsumerName)

	return runUntilEitherStops(ctx, addr, worker, checks, log)
}

// runUntilEitherStops serves the health endpoint and the polling loop together,
// stopping both as soon as one of them stops.
func runUntilEitherStops(
	ctx context.Context, addr string, worker *outbox.Worker, checks *health.Registry, log *slog.Logger,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerErr := make(chan error, 1)
	go func() {
		workerErr <- worker.Run(ctx)
		// The health endpoint has nothing to report once the loop is gone.
		cancel()
	}()

	serveErr := httpx.Run(ctx, addr, healthRouter(checks), log)

	if err := <-workerErr; err != nil {
		return err
	}

	return serveErr
}

func healthRouter(checks *health.Registry) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/health", checks.Handler())

	return r
}
