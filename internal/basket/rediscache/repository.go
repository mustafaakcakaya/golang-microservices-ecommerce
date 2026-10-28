// Package rediscache decorates the basket repository with a Redis cache.
package rediscache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
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
		var entry storedCart
		// An entry without a version was written before baskets had one.
		// Serving it would hand a checkout an empty version, so it is read
		// from the database instead, which always has one.
		if err := json.Unmarshal(cached, &entry); err == nil && entry.Version != uuid.Nil {
			r.log.DebugContext(ctx, "basket cache hit", "userName", userName)
			return entry.cart(), nil
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
	// The version is chosen here, before the write, so the database and the
	// cache hold the same one. Left to the database, the cache would be
	// refreshed with whatever the caller passed - no version at all - and a
	// checkout served from it would derive its id from nothing.
	cart.Version = uuid.New()

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

// Checkout clears the basket and its cache entry.
//
// The transaction belongs to the database underneath; the cache is refreshed
// after it commits. A failure to evict is reported rather than swallowed: a
// surviving entry would keep serving a basket that has already been checked
// out, and the customer would check it out again.
func (r *CachedRepository) Checkout(
	ctx context.Context, userName string, announcement messaging.Envelope,
) error {
	if err := r.inner.Checkout(ctx, userName, announcement); err != nil {
		return err
	}

	if err := r.client.Del(ctx, key(userName)).Err(); err != nil {
		return apperr.Internal(err, "the basket was checked out but its cache entry could not be removed")
	}

	return nil
}

func (r *CachedRepository) set(ctx context.Context, cart carts.ShoppingCart) error {
	encoded, err := json.Marshal(entryOf(cart))
	if err != nil {
		return fmt.Errorf("encoding basket for cache: %w", err)
	}

	return r.client.Set(ctx, key(cart.UserName), encoded, r.ttl).Err()
}

func key(userName string) string { return keyPrefix + userName }

// storedCart is how a basket is kept in the cache.
//
// The cart's own JSON form is the wire format, and that deliberately hides the
// version, so caching the cart as a client sees it would lose the version on
// every cache hit - and a checkout served from cache would derive its message
// id from nothing.
type storedCart struct {
	UserName string                   `json:"userName"`
	Version  uuid.UUID                `json:"version"`
	Items    []carts.ShoppingCartItem `json:"items"`
}

func entryOf(cart carts.ShoppingCart) storedCart {
	return storedCart{UserName: cart.UserName, Version: cart.Version, Items: cart.Items}
}

func (s storedCart) cart() carts.ShoppingCart {
	return carts.ShoppingCart{UserName: s.UserName, Items: s.Items, Version: s.Version}
}
