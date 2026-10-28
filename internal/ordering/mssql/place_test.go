package mssql_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
)

func newCustomer(t *testing.T) *domain.Customer {
	t.Helper()

	id, err := domain.NewCustomerID(uuid.New())
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}
	customer, err := domain.NewCustomer(id, "Mustafa Akcakaya", "mustafa@example.com")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}

	return customer
}

func newProduct(t *testing.T, name string, price int64) *domain.Product {
	t.Helper()

	id, err := domain.NewProductID(uuid.New())
	if err != nil {
		t.Fatalf("product id: %v", err)
	}
	product, err := domain.NewProduct(id, name, decimal.NewFromInt(price))
	if err != nil {
		t.Fatalf("product: %v", err)
	}

	return product
}

// storedProduct reads a product row back, reporting whether there is one.
func storedProduct(t *testing.T, id domain.ProductID) (string, decimal.Decimal, bool) {
	t.Helper()

	var (
		name  string
		price string
	)
	err := sharedDB.QueryRowContext(t.Context(),
		`SELECT Name, CONVERT(NVARCHAR(40), Price) FROM Products WHERE Id = @p1`, id.UUID()).Scan(&name, &price)
	if errors.Is(err, sql.ErrNoRows) {
		return "", decimal.Zero, false
	}
	if err != nil {
		t.Fatalf("reading product %s: %v", id, err)
	}

	return name, decimal.RequireFromString(price), true
}

func customerExists(t *testing.T, id domain.CustomerID) bool {
	t.Helper()

	var count int
	if err := sharedDB.QueryRowContext(t.Context(),
		`SELECT count(*) FROM Customers WHERE Id = @p1`, id.UUID()).Scan(&count); err != nil {
		t.Fatalf("counting customers: %v", err)
	}

	return count == 1
}

func TestPlacingAnOrderAddsTheProductsThisServiceDidNotKnow(t *testing.T) {
	repo := newRepository(t)

	customer := newCustomer(t)
	unknown := newProduct(t, "Pixel 9", 700)

	order := newOrder(t, customer.ID)
	// Twice on purpose: two lines of one product are still one product.
	if err := order.Add(unknown.ID, 1, unknown.Price); err != nil {
		t.Fatalf("adding line: %v", err)
	}
	if err := order.Add(unknown.ID, 2, unknown.Price); err != nil {
		t.Fatalf("adding line: %v", err)
	}

	if err := repo.Place(t.Context(), order, customer, []*domain.Product{unknown, unknown}); err != nil {
		t.Fatalf("placing: %v", err)
	}

	// Without the product row the line's foreign key would have refused the
	// order; with it, the order is there and readable.
	name, price, found := storedProduct(t, unknown.ID)
	if !found || name != "Pixel 9" || !price.Equal(decimal.NewFromInt(700)) {
		t.Errorf("product = %q/%s (found %v), want Pixel 9 at 700", name, price, found)
	}
	if !customerExists(t, customer.ID) {
		t.Error("the customer was not recorded")
	}
	if got, err := repo.ByID(t.Context(), order.ID()); err != nil || len(got.Items()) != 2 {
		t.Errorf("order = %v (error %v), want it stored with both lines", got, err)
	}
}

func TestPlacingAnOrderKeepsWhatIsKnownAboutAProduct(t *testing.T) {
	repo := newRepository(t)

	known := seedProduct(t, sharedDB) // "test product" at 100.00
	customer := newCustomer(t)

	// The basket saw it under another name and price.
	renamed, err := domain.NewProduct(known, "renamed in the basket", decimal.NewFromInt(90))
	if err != nil {
		t.Fatalf("product: %v", err)
	}

	order := newOrder(t, customer.ID)
	if err := order.Add(known, 1, renamed.Price); err != nil {
		t.Fatalf("adding line: %v", err)
	}

	if err := repo.Place(t.Context(), order, customer, []*domain.Product{renamed}); err != nil {
		t.Fatalf("placing: %v", err)
	}

	// The line records what was charged; the product row stays this
	// service's own reference data rather than whatever the last basket said.
	name, price, _ := storedProduct(t, known)
	if name != "test product" || !price.Equal(decimal.NewFromInt(100)) {
		t.Errorf("product = %q at %s, want it unchanged as test product at 100", name, price)
	}
}

func TestAFailedPlacementLeavesNothingBehind(t *testing.T) {
	repo := newRepository(t)

	customer := newCustomer(t)
	announced := newProduct(t, "announced", 100)
	missing := newProduct(t, "never recorded", 100)

	order := newOrder(t, customer.ID)
	// The line points at a product the placement does not add, so the
	// order's foreign key refuses it - after the customer and the other
	// product have been written in the same transaction.
	if err := order.Add(missing.ID, 1, missing.Price); err != nil {
		t.Fatalf("adding line: %v", err)
	}

	if err := repo.Place(t.Context(), order, customer, []*domain.Product{announced}); err == nil {
		t.Fatal("placing an order for an unknown product should fail")
	}

	if _, _, found := storedProduct(t, announced.ID); found {
		t.Error("a product was added for an order that was never stored")
	}
	if customerExists(t, customer.ID) {
		t.Error("a customer was recorded for an order that was never stored")
	}
	if _, err := repo.ByID(t.Context(), order.ID()); err == nil {
		t.Error("the order was stored despite the failure")
	}
}

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
