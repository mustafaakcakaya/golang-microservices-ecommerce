package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/payment/cards"
)

// CardRepository keeps cards in PostgreSQL.
type CardRepository struct {
	pool *pgxpool.Pool
}

// NewCardRepository wires the repository.
func NewCardRepository(pool *pgxpool.Pool) *CardRepository {
	return &CardRepository{pool: pool}
}

// Store saves a card under its token.
//
// A failure names the token, never the card, because the error is logged. The
// driver's own message does not quote values - a PostgreSQL error keeps those
// in its detail field, which its Error method leaves out.
func (r *CardRepository) Store(ctx context.Context, card cards.StoredCard) error {
	const query = `
		INSERT INTO cards (token, card_number, expiration, card_name)
		VALUES ($1, $2, $3, $4)`

	if _, err := r.pool.Exec(ctx, query, card.Token, card.CardNumber, card.Expiration, card.CardName); err != nil {
		return fmt.Errorf("storing the card for token %s: %w", card.Token, err)
	}

	return nil
}
