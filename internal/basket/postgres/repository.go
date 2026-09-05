package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// BasketRepository stores baskets in PostgreSQL.
//
// Marten keeps documents as JSONB; the same shape is used here - the items are
// a JSONB column keyed by user name - so the storage model matches the .NET
// service rather than normalising the basket into rows it never queries.
type BasketRepository struct {
	pool *pgxpool.Pool
}

// NewBasketRepository wires the repository.
func NewBasketRepository(pool *pgxpool.Pool) *BasketRepository {
	return &BasketRepository{pool: pool}
}

// GetByUserName loads a basket, reporting not found when the user has none.
func (r *BasketRepository) GetByUserName(ctx context.Context, userName string) (carts.ShoppingCart, error) {
	const query = `SELECT items FROM baskets WHERE user_name = $1`

	var rawItems []byte
	err := r.pool.QueryRow(ctx, query, userName).Scan(&rawItems)
	if errors.Is(err, pgx.ErrNoRows) {
		return carts.ShoppingCart{}, apperr.NotFound("Basket", userName)
	}
	if err != nil {
		return carts.ShoppingCart{}, fmt.Errorf("loading basket %q: %w", userName, err)
	}

	var items []carts.ShoppingCartItem
	if err := json.Unmarshal(rawItems, &items); err != nil {
		return carts.ShoppingCart{}, fmt.Errorf("decoding basket %q: %w", userName, err)
	}

	return carts.ShoppingCart{UserName: userName, Items: items}, nil
}

// Store writes the basket, replacing any existing one for that user.
func (r *BasketRepository) Store(ctx context.Context, cart carts.ShoppingCart) error {
	const query = `
		INSERT INTO baskets (user_name, items, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (user_name) DO UPDATE
		SET items = EXCLUDED.items, updated_at = now()`

	items := cart.Items
	if items == nil {
		items = []carts.ShoppingCartItem{}
	}

	encoded, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("encoding basket %q: %w", cart.UserName, err)
	}

	if _, err := r.pool.Exec(ctx, query, cart.UserName, encoded); err != nil {
		return fmt.Errorf("storing basket %q: %w", cart.UserName, err)
	}

	return nil
}

// Delete removes a basket. Removing one that does not exist is not an error,
// matching Marten's behaviour in the .NET repository.
func (r *BasketRepository) Delete(ctx context.Context, userName string) error {
	const query = `DELETE FROM baskets WHERE user_name = $1`

	if _, err := r.pool.Exec(ctx, query, userName); err != nil {
		return fmt.Errorf("deleting basket %q: %w", userName, err)
	}

	return nil
}
