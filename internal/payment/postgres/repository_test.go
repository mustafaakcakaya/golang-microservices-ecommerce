package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/payment/cards"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/payment/postgres"
)

// newPool starts a throwaway PostgreSQL with the migrations applied.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("payment"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		// Both limits are raised: the deadline around the whole wait and the
		// strategy's own startup timeout each default to a minute, and
		// raising one leaves the other in place.
		testcontainers.WithWaitStrategyAndDeadline(2*time.Minute,
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

	return pool
}

func TestACardIsKeptUnderItsToken(t *testing.T) {
	pool := newPool(t)
	repo := postgres.NewCardRepository(pool)
	ctx := t.Context()

	card := cards.StoredCard{
		Token:      "pt_0123456789abcdef0123456789abcdef",
		CardNumber: "5555555555554444",
		Expiration: "12/28",
		CardName:   "Mustafa Akcakaya",
	}
	if err := repo.Store(ctx, card); err != nil {
		t.Fatalf("storing: %v", err)
	}

	var number, expiration, name string
	if err := pool.QueryRow(ctx,
		`SELECT card_number, expiration, card_name FROM cards WHERE token = $1`, card.Token,
	).Scan(&number, &expiration, &name); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if number != card.CardNumber || expiration != card.Expiration || name != card.CardName {
		t.Errorf("stored = %s/%s/%s, want the card", number, expiration, name)
	}

	// A token is issued once; a second card under it would make the token
	// refer to two cards.
	if err := repo.Store(ctx, card); err == nil {
		t.Error("storing a second card under the same token should fail")
	}
}

func TestThereIsNowhereToKeepASecurityCode(t *testing.T) {
	pool := newPool(t)

	rows, err := pool.Query(t.Context(),
		`SELECT column_name FROM information_schema.columns WHERE table_name = 'cards'`)
	if err != nil {
		t.Fatalf("listing columns: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("reading column: %v", err)
		}
		switch column {
		case "cvv", "cvc", "security_code":
			t.Errorf("the cards table has a %s column", column)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("listing columns: %v", err)
	}
}
