// Command basket runs the Basket service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/api"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/discountclient"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/outbox"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/postgres"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/rediscache"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/broker"
)

const defaultAddr = ":8080"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("basket service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("BASKET_HTTP_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	databaseURL := os.Getenv("BASKET_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("BASKET_DATABASE_URL is required")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("creating database pool: %w", err)
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}

	// Discount is required, unlike Redis above. A missing cache only costs
	// speed; a missing discount service would silently store undiscounted
	// prices, so the service refuses to start without it.
	discountTarget := os.Getenv("BASKET_DISCOUNT_GRPC_ADDR")
	if discountTarget == "" {
		return errors.New("BASKET_DISCOUNT_GRPC_ADDR is required")
	}

	discounts, discountConn, err := discountclient.Dial(discountTarget)
	if err != nil {
		return err
	}
	defer func() { _ = discountConn.Close() }()

	checks := health.NewRegistry()
	checks.Register("postgres", pool.Ping)
	checks.Register("discount", func(ctx context.Context) error {
		return grpcHealthy(ctx, discountConn)
	})

	var repo carts.Repository = postgres.NewBasketRepository(pool)

	// Redis is required. Starting without it would run silently uncached: every
	// read would hit PostgreSQL and the problem would only show up as latency
	// under load, long after the deploy.
	redisURL := os.Getenv("BASKET_REDIS_URL")
	if redisURL == "" {
		return errors.New("BASKET_REDIS_URL is required")
	}

	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return fmt.Errorf("parsing BASKET_REDIS_URL: %w", err)
	}

	client := redis.NewClient(options)
	defer func() { _ = client.Close() }()

	checks.Register("redis", func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	})

	repo = rediscache.NewCachedRepository(repo, client, cacheTTL(), log)

	// The broker is required too: checking out is the point of a basket, and a
	// service that accepted checkouts it could not announce would lose them
	// silently.
	brokerConfig, err := broker.ConfigFromEnv()
	if err != nil {
		return err
	}

	registry := messaging.NewRegistry()
	basketv1.Register(registry)

	publisher, err := broker.New(brokerConfig, registry)
	if err != nil {
		return err
	}
	defer func() { _ = publisher.Close() }()

	// Nothing publishes during a request: a checkout is recorded in the same
	// transaction that clears the basket, and this relay is what gets it to
	// the broker afterwards.
	relay := outbox.NewRelay(postgres.NewOutboxStore(pool), publisher, outbox.DefaultOptions(), log)

	log.Info("basket configured", "broker", brokerConfig.Provider)

	return serveAndRelay(ctx, addr, api.Router(repo, discounts, checks, log), relay, log)
}

// serveAndRelay runs the API and the outbox relay together, stopping both as
// soon as one of them stops.
func serveAndRelay(
	ctx context.Context, addr string, router http.Handler, relay *outbox.Relay, log *slog.Logger,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	relayErr := make(chan error, 1)
	go func() {
		relayErr <- relay.Run(ctx)
		// Accepting checkouts nothing will publish is worse than not
		// accepting them.
		cancel()
	}()

	serveErr := httpx.Run(ctx, addr, router, log)

	if err := <-relayErr; err != nil {
		return err
	}

	return serveErr
}

// grpcHealthy asks the Discount service's standard health service whether it is
// serving, so Basket's /health reflects the dependency it cannot work without.
func grpcHealthy(ctx context.Context, conn *grpc.ClientConn) error {
	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return err
	}

	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("discount service reports %s", response.GetStatus())
	}

	return nil
}

// cacheTTL reads BASKET_CACHE_TTL, falling back to the package default.
func cacheTTL() time.Duration {
	raw := os.Getenv("BASKET_CACHE_TTL")
	if raw == "" {
		return rediscache.DefaultTTL
	}

	ttl, err := time.ParseDuration(raw)
	if err != nil {
		return rediscache.DefaultTTL
	}

	return ttl
}
