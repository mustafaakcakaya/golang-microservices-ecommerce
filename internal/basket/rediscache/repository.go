// Package rediscache decorates the basket repository with a Redis cache.
package rediscache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// DefaultTTL bounds how long a cached basket may live.
//
// Without an expiry an abandoned basket would stay in Redis forever, and an
// entry that drifts out of step would never heal. A TTL turns both into a
// bounded window.
const DefaultTTL = 30 * time.Minute

// keyPrefix namespaces the entries, so they cannot collide with another
// service sharing the same Redis.
const keyPrefix = "basket:"

// CachedRepository is a cache-aside decorator around another Repository, so the
// handlers stay unaware that a cache exists.
type CachedRepository struct {
	inner  carts.Repository
	client redis.UniversalClient
	ttl    time.Duration
	log    *slog.Logger
}

// NewCachedRepository wraps inner with a Redis cache. A non-positive ttl falls
// back to DefaultTTL.
func NewCachedRepository(
	inner carts.Repository,
	client redis.UniversalClient,
	ttl time.Duration,
	log *slog.Logger,
) *CachedRepository {
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	return &CachedRepository{inner: inner, client: client, ttl: ttl, log: log}
}

// GetByUserName serves from the cache when possible and populates it on a miss.
//
// Cache failures on this path are logged and ignored: the database still holds
// the answer, so a Redis outage must not turn reads into errors.
func (r *CachedRepository) GetByUserName(ctx context.Context, userName string) (carts.ShoppingCart, error) {
	cached, err := r.client.Get(ctx, key(userName)).Bytes()
	switch {
	case err == nil:
		var cart carts.ShoppingCart
		if err := json.Unmarshal(cached, &cart); err == nil {
			r.log.DebugContext(ctx, "basket cache hit", "userName", userName)
			return cart, nil
		}
		// A corrupt entry is not worth failing over; fall through to the database.
		r.log.WarnContext(ctx, "discarding unreadable cached basket", "userName", userName)
	case errors.Is(err, redis.Nil):
		r.log.DebugContext(ctx, "basket cache miss", "userName", userName)
	default:
		r.log.WarnContext(ctx, "basket cache read failed", "userName", userName, "error", err)
	}

	cart, err := r.inner.GetByUserName(ctx, userName)
	if err != nil {
		return carts.ShoppingCart{}, err
	}

	// Populating the cache is best effort for the same reason.
	if err := r.set(ctx, cart); err != nil {
		r.log.WarnContext(ctx, "basket cache populate failed", "userName", userName, "error", err)
	}

	return cart, nil
}

// Store writes through to the database and then refreshes the cache.
//
// Unlike the read path, a cache failure here is returned. The database has
// already changed, so leaving a stale entry behind would serve the previous
// basket until the TTL expires. Reporting the failure lets the caller retry -
// storing is an upsert, so a repeat is harmless.
func (r *CachedRepository) Store(ctx context.Context, cart carts.ShoppingCart) error {
	if err := r.inner.Store(ctx, cart); err != nil {
		return err
	}

	if err := r.set(ctx, cart); err != nil {
		return apperr.Internal(err, "basket was stored but its cache entry could not be refreshed")
	}

	return nil
}

// Delete removes the basket and its cache entry. As with Store, a cache failure
// is reported rather than swallowed: a surviving entry would keep serving a
// basket that no longer exists.
func (r *CachedRepository) Delete(ctx context.Context, userName string) error {
	if err := r.inner.Delete(ctx, userName); err != nil {
		return err
	}

	if err := r.client.Del(ctx, key(userName)).Err(); err != nil {
		return apperr.Internal(err, "basket was deleted but its cache entry could not be removed")
	}

	return nil
}

func (r *CachedRepository) set(ctx context.Context, cart carts.ShoppingCart) error {
	encoded, err := json.Marshal(cart)
	if err != nil {
		return fmt.Errorf("encoding basket for cache: %w", err)
	}

	return r.client.Set(ctx, key(cart.UserName), encoded, r.ttl).Err()
}

func key(userName string) string { return keyPrefix + userName }
