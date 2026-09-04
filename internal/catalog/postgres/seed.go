package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/products"
)

// seedNamespace derives stable ids for the sample products. The .NET seeder
// calls Guid.NewGuid(), so a reset database gets different ids every time;
// deriving them from the product name instead makes seeding idempotent and
// gives tests and API examples ids that do not move.
var seedNamespace = uuid.MustParse("6f2c1e64-9d0f-4a1e-9c1a-2d1f2b8f4c31")

// Seed inserts the sample catalog when the table is empty.
//
// It is a no-op on a populated database, mirroring the .NET initial data class,
// which returns early if any product exists. Callers decide when to run it;
// the service only does so when explicitly asked.
func Seed(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var existing int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM products`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("counting products before seeding: %w", err)
	}
	if existing > 0 {
		return 0, nil
	}

	repo := NewProductRepository(pool)
	for _, product := range sampleCatalog() {
		if err := repo.Store(ctx, product); err != nil {
			return 0, fmt.Errorf("seeding product %s: %w", product.Name, err)
		}
	}

	return len(sampleCatalog()), nil
}

// sampleCatalog is the same four products the .NET service seeds.
func sampleCatalog() []products.Product {
	const description = "This phone is the company's biggest change to its flagship smartphone in years."

	entries := []struct {
		name  string
		image string
		price int64
	}{
		{"iPhone X", "product-1.png", 950},
		{"Samsung 10", "product-2.png", 840},
		{"Huawei Plus", "product-3.png", 650},
		{"Xiaomi Mi 9", "product-4.png", 470},
	}

	catalog := make([]products.Product, 0, len(entries))
	for _, entry := range entries {
		catalog = append(catalog, products.Product{
			ID:          uuid.NewSHA1(seedNamespace, []byte(entry.name)),
			Name:        entry.name,
			Category:    []string{"Smart Phone"},
			Description: description,
			ImageFile:   entry.image,
			Price:       decimal.NewFromInt(entry.price),
		})
	}

	return catalog
}
