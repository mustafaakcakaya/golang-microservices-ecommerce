package mssql_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
)

func TestExistingCardsBecomeLegacyTokens(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := t.Context()

	// A database of its own, migrated only as far as the schema that still
	// held card numbers.
	dsn, err := databaseDSN(sharedDSN, "OrderingCardMigration")
	if err != nil {
		t.Fatalf("building connection string: %v", err)
	}
	if err := mssql.EnsureDatabase(ctx, dsn); err != nil {
		t.Fatalf("creating database: %v", err)
	}
	db, err := mssql.Open(dsn)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := mssql.MigrateTo(ctx, db, 5); err != nil {
		t.Fatalf("migrating to the card schema: %v", err)
	}

	customerID := uuid.New()
	paid, unpaid := uuid.New(), uuid.New()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO Customers (Id, Name, Email) VALUES (@p1, 'legacy', 'legacy@example.com')`,
		customerID); err != nil {
		t.Fatalf("seeding customer: %v", err)
	}

	const insertOrder = `
		INSERT INTO Orders (
			Id, CustomerId, OrderName, Status,
			ShippingAddress_FirstName, ShippingAddress_LastName, ShippingAddress_EmailAddress,
			ShippingAddress_AddressLine, ShippingAddress_Country, ShippingAddress_State, ShippingAddress_ZipCode,
			BillingAddress_FirstName, BillingAddress_LastName, BillingAddress_EmailAddress,
			BillingAddress_AddressLine, BillingAddress_Country, BillingAddress_State, BillingAddress_ZipCode,
			Payment_CardName, Payment_CardNumber, Payment_Expiration, Payment_CVV, Payment_PaymentMethod
		) VALUES (
			@p1, @p2, @p3, 'Pending',
			'a', 'b', 'legacy@example.com', 'line', 'TR', 'IST', '34000',
			'a', 'b', 'legacy@example.com', 'line', 'TR', 'IST', '34000',
			@p4, @p5, @p6, @p7, @p8
		)`

	// A card stored with spaces, as people type it.
	if _, err := db.ExecContext(ctx, insertOrder,
		paid, customerID, "ORD_L", "legacy", "5555 5555 5555 4444", "12/28", "zqx", 2); err != nil {
		t.Fatalf("seeding a paid order: %v", err)
	}
	if _, err := db.ExecContext(ctx, insertOrder,
		unpaid, customerID, "ORD_U", nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("seeding an unpaid order: %v", err)
	}

	if err := mssql.Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	repo := mssql.NewOrderRepository(db, mssql.DropEvents)

	paidID, _ := domain.NewOrderID(paid)
	order, err := repo.ByID(ctx, paidID)
	if err != nil {
		t.Fatalf("loading the paid order: %v", err)
	}
	payment := order.Payment()
	if payment.CardLast4() != "4444" || payment.PaymentMethod() != 2 {
		t.Errorf("payment = last4 %q, method %d; want the card's last four and its method",
			payment.CardLast4(), payment.PaymentMethod())
	}
	if want := "legacy-" + paid.String(); payment.PaymentToken() != want {
		t.Errorf("token = %q, want %q", payment.PaymentToken(), want)
	}

	unpaidID, _ := domain.NewOrderID(unpaid)
	order, err = repo.ByID(ctx, unpaidID)
	if err != nil {
		t.Fatalf("loading the unpaid order: %v", err)
	}
	// An order that had no card does not suddenly get a token.
	if !order.Payment().IsZero() {
		t.Errorf("payment = %v, want none", order.Payment())
	}

	// The card itself is gone, not just hidden.
	var leftover int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM sys.columns
		WHERE object_id = OBJECT_ID('dbo.Orders')
		  AND name IN ('Payment_CardName', 'Payment_CardNumber', 'Payment_Expiration', 'Payment_CVV')`,
	).Scan(&leftover); err != nil {
		t.Fatalf("listing columns: %v", err)
	}
	if leftover != 0 {
		t.Errorf("%d card columns survived the migration", leftover)
	}
}
