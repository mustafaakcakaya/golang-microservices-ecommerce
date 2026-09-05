package rediscache_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/rediscache"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// countingRepository records how often the inner repository is reached, which
// is how a cache hit is observed from the outside.
type countingRepository struct {
	items  map[string]carts.ShoppingCart
	reads  int
	writes int
	failOn error
}

func newCountingRepository(seed ...carts.ShoppingCart) *countingRepository {
	repo := &countingRepository{items: make(map[string]carts.ShoppingCart)}
	for _, cart := range seed {
		repo.items[cart.UserName] = cart
	}
	return repo
}

func (r *countingRepository) GetByUserName(_ context.Context, userName string) (carts.ShoppingCart, error) {
	r.reads++
	if r.failOn != nil {
		return carts.ShoppingCart{}, r.failOn
	}
	cart, ok := r.items[userName]
	if !ok {
		return carts.ShoppingCart{}, apperr.NotFound("Basket", userName)
	}
	return cart, nil
}

func (r *countingRepository) Store(_ context.Context, cart carts.ShoppingCart) error {
	r.writes++
	if r.failOn != nil {
		return r.failOn
	}
	r.items[cart.UserName] = cart
	return nil
}

func (r *countingRepository) Delete(_ context.Context, userName string) error {
	r.writes++
	if r.failOn != nil {
		return r.failOn
	}
	delete(r.items, userName)
	return nil
}

func sampleCart(userName string) carts.ShoppingCart {
	return carts.ShoppingCart{
		UserName: userName,
		Items: []carts.ShoppingCartItem{
			{Quantity: 2, Color: "Red", Price: decimal.NewFromInt(500), ProductName: "iPhone X"},
		},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newRedis starts a throwaway Redis and returns a client for it.
func newRedis(t *testing.T) redis.UniversalClient {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("starting redis: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminating redis: %v", err)
		}
	})

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	options, err := redis.ParseURL(endpoint)
	if err != nil {
		t.Fatalf("parsing redis url: %v", err)
	}

	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func TestSecondReadIsServedFromCache(t *testing.T) {
	client := newRedis(t)
	inner := newCountingRepository(sampleCart("mustafa"))
	repo := rediscache.NewCachedRepository(inner, client, time.Minute, discardLogger())
	ctx := context.Background()

	first, err := repo.GetByUserName(ctx, "mustafa")
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	second, err := repo.GetByUserName(ctx, "mustafa")
	if err != nil {
		t.Fatalf("second read: %v", err)
	}

	if inner.reads != 1 {
		t.Errorf("inner reads = %d, want 1: the second read should hit the cache", inner.reads)
	}
	if !first.TotalPrice().Equal(second.TotalPrice()) || second.UserName != "mustafa" {
		t.Errorf("cached basket = %+v, want the same as %+v", second, first)
	}
	if len(second.Items) != 1 || !second.Items[0].Price.Equal(decimal.NewFromInt(500)) {
		t.Errorf("cached items = %+v, want the price to survive the round trip", second.Items)
	}
}

func TestStoreRefreshesTheCache(t *testing.T) {
	client := newRedis(t)
	inner := newCountingRepository(sampleCart("mustafa"))
	repo := rediscache.NewCachedRepository(inner, client, time.Minute, discardLogger())
	ctx := context.Background()

	if _, err := repo.GetByUserName(ctx, "mustafa"); err != nil {
		t.Fatalf("priming the cache: %v", err)
	}

	updated := carts.ShoppingCart{
		UserName: "mustafa",
		Items:    []carts.ShoppingCartItem{{Quantity: 1, Price: decimal.NewFromInt(10), ProductName: "Pen"}},
	}
	if err := repo.Store(ctx, updated); err != nil {
		t.Fatalf("storing: %v", err)
	}

	got, err := repo.GetByUserName(ctx, "mustafa")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	// A write-through cache must not keep serving the previous basket.
	if len(got.Items) != 1 || got.Items[0].ProductName != "Pen" {
		t.Errorf("basket after store = %+v, want the updated line", got.Items)
	}
	if inner.reads != 1 {
		t.Errorf("inner reads = %d, want 1: the read after a store should still be cached", inner.reads)
	}
}

func TestDeleteEvictsTheCache(t *testing.T) {
	client := newRedis(t)
	inner := newCountingRepository(sampleCart("mustafa"))
	repo := rediscache.NewCachedRepository(inner, client, time.Minute, discardLogger())
	ctx := context.Background()

	if _, err := repo.GetByUserName(ctx, "mustafa"); err != nil {
		t.Fatalf("priming the cache: %v", err)
	}
	if err := repo.Delete(ctx, "mustafa"); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	// Without eviction the cache would keep serving a basket that is gone.
	_, err := repo.GetByUserName(ctx, "mustafa")
	if apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("read after delete = %v, want not found", err)
	}
}

func TestEntriesExpire(t *testing.T) {
	client := newRedis(t)
	inner := newCountingRepository(sampleCart("mustafa"))
	repo := rediscache.NewCachedRepository(inner, client, 50*time.Millisecond, discardLogger())
	ctx := context.Background()

	if _, err := repo.GetByUserName(ctx, "mustafa"); err != nil {
		t.Fatalf("priming: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	if _, err := repo.GetByUserName(ctx, "mustafa"); err != nil {
		t.Fatalf("reading after expiry: %v", err)
	}
	// The .NET decorator sets no expiry; the TTL is what bounds staleness here.
	if inner.reads != 2 {
		t.Errorf("inner reads = %d, want 2: the entry should have expired", inner.reads)
	}
}

func TestReadsSurviveAnUnreachableCache(t *testing.T) {
	t.Parallel()

	// Pointing at a closed port stands in for a Redis outage.
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = client.Close() })

	inner := newCountingRepository(sampleCart("mustafa"))
	repo := rediscache.NewCachedRepository(inner, client, time.Minute, discardLogger())

	got, err := repo.GetByUserName(context.Background(), "mustafa")
	if err != nil {
		t.Fatalf("a cache outage must not fail reads: %v", err)
	}
	if got.UserName != "mustafa" {
		t.Errorf("basket = %+v, want it served from the database", got)
	}
	if inner.reads != 1 {
		t.Errorf("inner reads = %d, want the database to have been consulted", inner.reads)
	}
}

func TestWriteReportsCacheFailureRatherThanLeavingStaleData(t *testing.T) {
	t.Parallel()

	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = client.Close() })

	inner := newCountingRepository()
	repo := rediscache.NewCachedRepository(inner, client, time.Minute, discardLogger())
	ctx := context.Background()

	err := repo.Store(ctx, sampleCart("mustafa"))

	// The database write succeeded, so silently ignoring the cache failure
	// would serve the previous basket until the TTL expired.
	if err == nil {
		t.Fatal("store should report that the cache could not be refreshed")
	}
	if inner.writes != 1 {
		t.Errorf("inner writes = %d, want the database write to have happened", inner.writes)
	}

	if err := repo.Delete(ctx, "mustafa"); err == nil {
		t.Error("delete should report that the cache entry could not be removed")
	}
}

func TestDatabaseErrorsPassThrough(t *testing.T) {
	client := newRedis(t)
	failure := errors.New("database is down")
	inner := newCountingRepository()
	inner.failOn = failure
	repo := rediscache.NewCachedRepository(inner, client, time.Minute, discardLogger())

	if _, err := repo.GetByUserName(context.Background(), "mustafa"); !errors.Is(err, failure) {
		t.Errorf("error = %v, want the database error to surface unchanged", err)
	}
}
