// Command basket runs the Basket service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/api"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/postgres"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/rediscache"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
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

	checks := health.NewRegistry()
	checks.Register("postgres", pool.Ping)

	var repo carts.Repository = postgres.NewBasketRepository(pool)

	// Redis is optional: without it the service still works, just uncached.
	// The .NET service always requires it, but making it optional keeps local
	// runs and tests simple.
	if redisURL := os.Getenv("BASKET_REDIS_URL"); redisURL != "" {
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
		log.Info("basket cache enabled")
	}

	return httpx.Run(ctx, addr, api.Router(repo, checks, log), log)
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
