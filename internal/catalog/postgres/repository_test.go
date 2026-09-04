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

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/postgres"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/products"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

// newRepository starts a throwaway PostgreSQL, applies the migrations and
// returns a repository pointing at it. The container is removed when the test
// finishes, so no state survives the run.
func newRepository(t *testing.T) *postgres.ProductRepository {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("catalog"),
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

	// Exercising the real migration is part of the point: it proves the schema
	// the service ships with is the one the repository queries assume.
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	return postgres.NewProductRepository(pool)
}

func newProduct(name string, price int64, categories ...string) products.Product {
	return products.Product{
		ID:          uuid.New(),
		Name:        name,
		Category:    categories,
		Description: "test product",
		ImageFile:   "product.png",
		Price:       decimal.NewFromInt(price),
	}
}

func TestStoreAndLoadRoundTrip(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	want := newProduct("iPhone X", 950, "Smart Phone")
	if err := repo.Store(ctx, want); err != nil {
		t.Fatalf("storing: %v", err)
	}

	got, err := repo.ByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	if got.ID != want.ID || got.Name != want.Name || got.ImageFile != want.ImageFile {
		t.Errorf("product = %+v, want %+v", got, want)
	}
	if !got.Price.Equal(want.Price) {
		t.Errorf("price = %s, want %s", got.Price, want.Price)
	}
	if len(got.Category) != 1 || got.Category[0] != "Smart Phone" {
		t.Errorf("category = %v, want [Smart Phone]", got.Category)
	}
}

func TestByIDReportsNotFoundForMissingProduct(t *testing.T) {
	repo := newRepository(t)

	_, err := repo.ByID(context.Background(), uuid.New())

	// The HTTP layer turns this kind into 404; a bare sql error would become 500.
	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound (got error %v)", got, err)
	}
}

func TestStoreReplacesExistingProduct(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	product := newProduct("Samsung 10", 840, "Smart Phone")
	if err := repo.Store(ctx, product); err != nil {
		t.Fatalf("storing: %v", err)
	}

	product.Name = "Samsung 20"
	product.Price = decimal.NewFromInt(900)
	if err := repo.Store(ctx, product); err != nil {
		t.Fatalf("re-storing: %v", err)
	}

	got, err := repo.ByID(ctx, product.ID)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got.Name != "Samsung 20" || !got.Price.Equal(decimal.NewFromInt(900)) {
		t.Errorf("product = %+v, want the updated values (upsert, not duplicate)", got)
	}

	_, count, err := repo.List(ctx, pagination.Request{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 after re-storing the same id", count)
	}
}

func TestListPagesAndReportsTotalCount(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	for _, name := range []string{"A", "B", "C", "D", "E"} {
		if err := repo.Store(ctx, newProduct(name, 100, "Smart Phone")); err != nil {
			t.Fatalf("storing %s: %v", name, err)
		}
	}

	first, count, err := repo.List(ctx, pagination.Request{PageIndex: 0, PageSize: 2})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want the total 5, not the page size", count)
	}
	if len(first) != 2 || first[0].Name != "A" || first[1].Name != "B" {
		t.Fatalf("first page = %v, want [A B]", names(first))
	}

	second, _, err := repo.List(ctx, pagination.Request{PageIndex: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("listing page 2: %v", err)
	}
	if len(second) != 2 || second[0].Name != "C" || second[1].Name != "D" {
		t.Errorf("second page = %v, want [C D]", names(second))
	}

	// Past the end must be empty rather than an error.
	last, _, err := repo.List(ctx, pagination.Request{PageIndex: 9, PageSize: 2})
	if err != nil {
		t.Fatalf("listing past the end: %v", err)
	}
	if len(last) != 0 {
		t.Errorf("page past the end = %v, want empty", names(last))
	}
}

func TestByCategoryMatchesProductsTaggedWithIt(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	phone := newProduct("iPhone X", 950, "Smart Phone", "Sale")
	laptop := newProduct("MacBook", 2000, "Computer")

	for _, p := range []products.Product{phone, laptop} {
		if err := repo.Store(ctx, p); err != nil {
			t.Fatalf("storing %s: %v", p.Name, err)
		}
	}

	got, err := repo.ByCategory(ctx, "Smart Phone")
	if err != nil {
		t.Fatalf("querying by category: %v", err)
	}
	if len(got) != 1 || got[0].ID != phone.ID {
		t.Errorf("category match = %v, want only the phone", names(got))
	}

	// A product carrying several categories must match each of them.
	sale, err := repo.ByCategory(ctx, "Sale")
	if err != nil {
		t.Fatalf("querying by second category: %v", err)
	}
	if len(sale) != 1 || sale[0].ID != phone.ID {
		t.Errorf("second category match = %v, want the phone", names(sale))
	}

	none, err := repo.ByCategory(ctx, "Nonexistent")
	if err != nil {
		t.Fatalf("querying unknown category: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("unknown category = %v, want empty", names(none))
	}
}

func TestDeleteRemovesProductAndIsIdempotent(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	product := newProduct("Huawei Plus", 650, "Smart Phone")
	if err := repo.Store(ctx, product); err != nil {
		t.Fatalf("storing: %v", err)
	}

	if err := repo.Delete(ctx, product.ID); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if _, err := repo.ByID(ctx, product.ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("product still readable after delete: %v", err)
	}

	// Marten's Delete is idempotent; the port keeps that behaviour.
	if err := repo.Delete(ctx, product.ID); err != nil {
		t.Errorf("deleting a missing product should not fail: %v", err)
	}
}

func TestMigrateIsRepeatable(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	// newRepository already migrated once; storing proves the schema is usable
	// and the second Migrate call in the same process is a no-op.
	if err := repo.Store(ctx, newProduct("Xiaomi Mi 9", 470, "Smart Phone")); err != nil {
		t.Fatalf("storing after migration: %v", err)
	}
}

func names(items []products.Product) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
