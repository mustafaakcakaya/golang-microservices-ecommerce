// Command ordering runs the Ordering service.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/api"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/integration"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/orders"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/broker"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/inbox"
)

const defaultAddr = ":8080"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("ordering service stopped with error", "error", err)
		os.Exit(1)
	}
}

// fulfillOrders reads whether new orders should be announced to other
// services. Off unless explicitly enabled, so a deployment does not start
// broadcasting to consumers that are not ready for it.
func fulfillOrders() bool {
	return os.Getenv("ORDERING_FULFILL_ORDERS") == "true"
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

	repo := mssql.NewOrderRepository(db, integration.NewMapper(fulfillOrders()).Map)

	subscriber, deliver, err := checkoutSubscription(db, repo, log)
	if err != nil {
		return err
	}
	defer func() { _ = subscriber.Close() }()

	return serveAndSubscribe(ctx, addr, api.Router(repo, checks, log), subscriber, deliver, log)
}

// consumerName owns both the inbox records and the broker subscription, so a
// second consumer of the same messages cannot be mistaken for this one.
const consumerName = "ordering-checkout"

// checkoutSubscription builds the subscription that turns checkouts into
// orders.
func checkoutSubscription(
	db *sql.DB, repo *mssql.OrderRepository, log *slog.Logger,
) (messaging.Subscriber, messaging.Delivery, error) {
	brokerConfig, err := broker.ConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}

	contracts := []messaging.Contract{(basketv1.CheckoutRequested{}).Contract()}

	subscriber, err := broker.NewSubscriber(brokerConfig, consumerName, contracts, log)
	if err != nil {
		return nil, nil, err
	}

	handler := orders.NewCheckoutHandler(repo, mssql.NewSnapshotStore(db))

	// Delivery is at-least-once, so the same checkout can arrive more than
	// once; the inbox is what keeps one checkout from becoming two orders.
	deliver := inbox.Idempotent(mssql.NewInboxStore(db), consumerName, log, handler.Handle)

	log.Info("ordering configured", "broker", brokerConfig.Provider, "consumer", consumerName)

	return subscriber, deliver, nil
}

// serveAndSubscribe runs the API and the subscription together, stopping both
// as soon as one of them stops.
func serveAndSubscribe(
	ctx context.Context,
	addr string,
	router http.Handler,
	subscriber messaging.Subscriber,
	deliver messaging.Delivery,
	log *slog.Logger,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	subscribeErr := make(chan error, 1)
	go func() {
		subscribeErr <- subscriber.Subscribe(ctx, deliver)
		// The API alone cannot take checkouts, so it does not outlive them.
		cancel()
	}()

	serveErr := httpx.Run(ctx, addr, router, log)

	if err := <-subscribeErr; err != nil {
		return err
	}

	return serveErr
}
