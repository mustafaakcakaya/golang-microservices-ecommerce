package postgres_test

import (
	"context"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/postgres"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

func TestSeedPopulatesEmptyCatalogOnce(t *testing.T) {
	repo, pool := newRepositoryWithPool(t)
	ctx := context.Background()

	inserted, err := postgres.Seed(ctx, pool)
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if inserted != 4 {
		t.Fatalf("inserted = %d, want the four sample products", inserted)
	}

	items, count, err := repo.List(ctx, pagination.Request{PageSize: 50})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if count != 4 {
		t.Fatalf("count = %d, want 4", count)
	}

	// Re-running must not duplicate: seeding returns early when any product
	// exists, and ids are derived rather than random so a repeat run would
	// upsert the same rows anyway.
	again, err := postgres.Seed(ctx, pool)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if again != 0 {
		t.Errorf("second run inserted %d, want 0 on a populated catalog", again)
	}

	_, count, err = repo.List(ctx, pagination.Request{PageSize: 50})
	if err != nil {
		t.Fatalf("listing after second seed: %v", err)
	}
	if count != 4 {
		t.Errorf("count = %d after re-seeding, want 4", count)
	}

	if len(items) != 4 {
		t.Fatalf("page returned %d products, want 4", len(items))
	}
	for _, product := range items {
		if product.Price.IsZero() {
			t.Errorf("%s has no price", product.Name)
		}
		if len(product.Category) == 0 {
			t.Errorf("%s has no category", product.Name)
		}
	}
}

func TestSeedLeavesExistingCatalogUntouched(t *testing.T) {
	repo, pool := newRepositoryWithPool(t)
	ctx := context.Background()

	existing := newProduct("Custom", 1, "Sale")
	if err := repo.Store(ctx, existing); err != nil {
		t.Fatalf("storing: %v", err)
	}

	inserted, err := postgres.Seed(ctx, pool)
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if inserted != 0 {
		t.Errorf("inserted = %d, want 0 when the catalog already has data", inserted)
	}

	_, count, err := repo.List(ctx, pagination.Request{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want the single pre-existing product", count)
	}
}
