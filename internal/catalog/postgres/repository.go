package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/products"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

// ProductRepository stores products as JSONB documents.
type ProductRepository struct {
	pool *pgxpool.Pool
}

// NewProductRepository wires the repository to a connection pool.
func NewProductRepository(pool *pgxpool.Pool) *ProductRepository {
	return &ProductRepository{pool: pool}
}

// List pages through products ordered by name, with id as a tie-breaker so
// pages are stable when names repeat.
func (r *ProductRepository) List(ctx context.Context, page pagination.Request) ([]products.Product, int64, error) {
	const countSQL = `SELECT count(*) FROM products`
	const pageSQL = `
		SELECT data FROM products
		ORDER BY data ->> 'name', id
		LIMIT $1 OFFSET $2`

	var count int64
	if err := r.pool.QueryRow(ctx, countSQL).Scan(&count); err != nil {
		return nil, 0, fmt.Errorf("counting products: %w", err)
	}

	rows, err := r.pool.Query(ctx, pageSQL, page.Limit(), page.Offset())
	if err != nil {
		return nil, 0, fmt.Errorf("listing products: %w", err)
	}
	defer rows.Close()

	items, err := collect(rows)
	if err != nil {
		return nil, 0, err
	}

	return items, count, nil
}

// ByID loads one product or reports apperr.NotFound.
func (r *ProductRepository) ByID(ctx context.Context, id uuid.UUID) (products.Product, error) {
	const query = `SELECT data FROM products WHERE id = $1`

	var product products.Product
	err := r.pool.QueryRow(ctx, query, id).Scan(&product)
	if errors.Is(err, pgx.ErrNoRows) {
		return products.Product{}, apperr.NotFound("Product", id)
	}
	if err != nil {
		return products.Product{}, fmt.Errorf("loading product %s: %w", id, err)
	}

	return product, nil
}

// ByCategory matches products whose category array contains the value; the
// jsonb ? operator uses the GIN index created by the migration.
func (r *ProductRepository) ByCategory(ctx context.Context, category string) ([]products.Product, error) {
	const query = `
		SELECT data FROM products
		WHERE data -> 'category' ? $1
		ORDER BY data ->> 'name', id`

	rows, err := r.pool.Query(ctx, query, category)
	if err != nil {
		return nil, fmt.Errorf("listing products by category: %w", err)
	}
	defer rows.Close()

	return collect(rows)
}

// Store upserts the document, matching Marten's Store semantics.
func (r *ProductRepository) Store(ctx context.Context, product products.Product) error {
	const query = `
		INSERT INTO products (id, data) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data`

	data, err := json.Marshal(product)
	if err != nil {
		return fmt.Errorf("encoding product %s: %w", product.ID, err)
	}

	if _, err := r.pool.Exec(ctx, query, product.ID, data); err != nil {
		return fmt.Errorf("storing product %s: %w", product.ID, err)
	}

	return nil
}

// Delete removes the document; a missing id is not an error.
func (r *ProductRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `DELETE FROM products WHERE id = $1`

	if _, err := r.pool.Exec(ctx, query, id); err != nil {
		return fmt.Errorf("deleting product %s: %w", id, err)
	}

	return nil
}

// collect decodes each jsonb row into a Product.
func collect(rows pgx.Rows) ([]products.Product, error) {
	items := []products.Product{}

	for rows.Next() {
		var product products.Product
		if err := rows.Scan(&product); err != nil {
			return nil, fmt.Errorf("decoding product row: %w", err)
		}
		items = append(items, product)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading product rows: %w", err)
	}

	return items, nil
}
