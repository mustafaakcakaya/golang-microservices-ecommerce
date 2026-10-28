package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// BasketRepository stores baskets in PostgreSQL.
//
// The items are a JSONB column keyed by user name rather than rows in a
// separate table: nothing queries a line on its own, so normalising would add
// joins without buying anything.
type BasketRepository struct {
	pool *pgxpool.Pool
}

// NewBasketRepository wires the repository.
func NewBasketRepository(pool *pgxpool.Pool) *BasketRepository {
	return &BasketRepository{pool: pool}
}

// GetByUserName loads a basket, reporting not found when the user has none.
func (r *BasketRepository) GetByUserName(ctx context.Context, userName string) (carts.ShoppingCart, error) {
	const query = `SELECT items, version FROM baskets WHERE user_name = $1`

	var (
		rawItems []byte
		version  uuid.UUID
	)
	err := r.pool.QueryRow(ctx, query, userName).Scan(&rawItems, &version)
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

	return carts.ShoppingCart{UserName: userName, Items: items, Version: version}, nil
}

// Store writes the basket, replacing any existing one for that user.
//
// The cart's version is used when it has one. A caller passing a version it
// read earlier would make two different baskets look alike, so only a writer
// that has just chosen a fresh one should set it.
func (r *BasketRepository) Store(ctx context.Context, cart carts.ShoppingCart) error {
	// A new version on every write, so two requests reading the same basket
	// see the same value and a later write is recognisably a different basket.
	const query = `
		INSERT INTO baskets (user_name, items, version, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (user_name) DO UPDATE
		SET items = EXCLUDED.items, version = EXCLUDED.version, updated_at = now()`

	items := cart.Items
	if items == nil {
		items = []carts.ShoppingCartItem{}
	}

	encoded, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("encoding basket %q: %w", cart.UserName, err)
	}

	// A decorator in front may have chosen the version already, so that what
	// it caches matches what is stored; otherwise every write gets a new one.
	version := cart.Version
	if version == uuid.Nil {
		version = uuid.New()
	}

	if _, err := r.pool.Exec(ctx, query, cart.UserName, encoded, version); err != nil {
		return fmt.Errorf("storing basket %q: %w", cart.UserName, err)
	}

	return nil
}

// Delete removes a basket. Removing one that does not exist is not an error.
func (r *BasketRepository) Delete(ctx context.Context, userName string) error {
	const query = `DELETE FROM baskets WHERE user_name = $1`

	if _, err := r.pool.Exec(ctx, query, userName); err != nil {
		return fmt.Errorf("deleting basket %q: %w", userName, err)
	}

	return nil
}
