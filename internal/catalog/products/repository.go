package products

import (
	"context"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

// Repository is the persistence seam the handlers depend on. The Postgres
// implementation lives in the postgres package; tests can substitute a fake.
type Repository interface {
	// List returns one page of products and the total count.
	List(ctx context.Context, page pagination.Request) ([]Product, int64, error)

	// ByID returns apperr.NotFound when no product has the id.
	ByID(ctx context.Context, id uuid.UUID) (Product, error)

	// ByCategory returns every product tagged with the category.
	ByCategory(ctx context.Context, category string) ([]Product, error)

	// Store inserts or replaces the product.
	Store(ctx context.Context, product Product) error

	// Delete removes the product. Deleting a missing product is not an error:
	// the caller's intent - the product is gone - holds either way.
	Delete(ctx context.Context, id uuid.UUID) error
}
