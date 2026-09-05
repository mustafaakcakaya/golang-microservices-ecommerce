package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/postgres"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// newRepository starts a throwaway PostgreSQL, applies the migrations and
// returns a repository pointing at it. The container is removed when the test
// finishes, so nothing survives the run.
func newRepository(t *testing.T) *postgres.BasketRepository {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("basket"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminating postgres: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	return postgres.NewBasketRepository(pool)
}

func cart(userName string, items ...carts.ShoppingCartItem) carts.ShoppingCart {
	return carts.ShoppingCart{UserName: userName, Items: items}
}

func item(name string, quantity int, price int64) carts.ShoppingCartItem {
	return carts.ShoppingCartItem{
		Quantity:    quantity,
		Color:       "Red",
		Price:       decimal.NewFromInt(price),
		ProductID:   uuid.New(),
		ProductName: name,
	}
}

func TestStoreAndLoadRoundTrip(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	want := cart("mustafa", item("iPhone X", 2, 500), item("Samsung 10", 1, 400))
	if err := repo.Store(ctx, want); err != nil {
		t.Fatalf("storing: %v", err)
	}

	got, err := repo.GetByUserName(ctx, "mustafa")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	if got.UserName != want.UserName || len(got.Items) != 2 {
		t.Fatalf("basket = %+v, want two items for mustafa", got)
	}
	if got.Items[0].ProductName != "iPhone X" || got.Items[0].Quantity != 2 {
		t.Errorf("first item = %+v, want the stored line in order", got.Items[0])
	}
	// Prices are decimals; a float round trip would drift here.
	if !got.Items[0].Price.Equal(decimal.NewFromInt(500)) {
		t.Errorf("price = %s, want 500", got.Items[0].Price)
	}
	if !got.TotalPrice().Equal(decimal.NewFromInt(1400)) {
		t.Errorf("total = %s, want 1400", got.TotalPrice())
	}
}

func TestGetReportsNotFoundForUnknownUser(t *testing.T) {
	repo := newRepository(t)

	_, err := repo.GetByUserName(context.Background(), "nobody")

	// The HTTP layer turns this kind into 404; a bare sql error would be a 500.
	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound (error %v)", got, err)
	}
}

func TestStoreReplacesRatherThanDuplicating(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	if err := repo.Store(ctx, cart("mustafa", item("iPhone X", 2, 500))); err != nil {
		t.Fatalf("storing: %v", err)
	}
	if err := repo.Store(ctx, cart("mustafa", item("Pen", 1, 10))); err != nil {
		t.Fatalf("re-storing: %v", err)
	}

	got, err := repo.GetByUserName(ctx, "mustafa")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	// One basket per user: the second store must overwrite, not append.
	if len(got.Items) != 1 || got.Items[0].ProductName != "Pen" {
		t.Errorf("items = %+v, want only the replacement line", got.Items)
	}
}

func TestStoreEmptyBasketIsReadBackAsEmptyNotNull(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	if err := repo.Store(ctx, cart("empty")); err != nil {
		t.Fatalf("storing: %v", err)
	}

	got, err := repo.GetByUserName(ctx, "empty")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got.Items == nil {
		t.Error("items should decode as an empty slice so JSON emits [] rather than null")
	}
	if len(got.Items) != 0 {
		t.Errorf("items = %+v, want empty", got.Items)
	}
}

func TestDeleteRemovesBasketAndIsIdempotent(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	if err := repo.Store(ctx, cart("mustafa", item("iPhone X", 1, 500))); err != nil {
		t.Fatalf("storing: %v", err)
	}

	if err := repo.Delete(ctx, "mustafa"); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	// Deleting again must not fail: the outcome is the same either way.
	if err := repo.Delete(ctx, "mustafa"); err != nil {
		t.Fatalf("second delete: %v", err)
	}

	if _, err := repo.GetByUserName(ctx, "mustafa"); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("basket still readable after delete (error %v)", err)
	}
}

func TestBasketsAreIsolatedPerUser(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	if err := repo.Store(ctx, cart("mustafa", item("iPhone X", 1, 500))); err != nil {
		t.Fatalf("storing mustafa: %v", err)
	}
	if err := repo.Store(ctx, cart("john", item("Samsung 10", 3, 400))); err != nil {
		t.Fatalf("storing john: %v", err)
	}

	if err := repo.Delete(ctx, "mustafa"); err != nil {
		t.Fatalf("deleting mustafa: %v", err)
	}

	john, err := repo.GetByUserName(ctx, "john")
	if err != nil {
		t.Fatalf("john's basket disappeared: %v", err)
	}
	if len(john.Items) != 1 || john.Items[0].Quantity != 3 {
		t.Errorf("john's basket = %+v, want it untouched", john.Items)
	}
}
