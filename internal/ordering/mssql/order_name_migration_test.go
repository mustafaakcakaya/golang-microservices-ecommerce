package mssql_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
)

func TestRemovingTheOrderNameKeepsTheOrders(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	ctx := t.Context()

	// A database of its own, migrated only as far as the schema that still
	// named orders.
	dsn, err := databaseDSN(sharedDSN, "OrderingNameMigration")
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

	if err := mssql.MigrateTo(ctx, db, 6); err != nil {
		t.Fatalf("migrating to the named schema: %v", err)
	}

	customerID, orderID := uuid.New(), uuid.New()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO Customers (Id, Name, Email) VALUES (@p1, 'named', 'named@example.com')`,
		customerID); err != nil {
		t.Fatalf("seeding customer: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO Orders (
			Id, CustomerId, OrderName, Status,
			ShippingAddress_FirstName, ShippingAddress_LastName, ShippingAddress_EmailAddress,
			ShippingAddress_AddressLine, ShippingAddress_Country, ShippingAddress_State, ShippingAddress_ZipCode,
			BillingAddress_FirstName, BillingAddress_LastName, BillingAddress_EmailAddress,
			BillingAddress_AddressLine, BillingAddress_Country, BillingAddress_State, BillingAddress_ZipCode,
			Payment_PaymentToken, Payment_CardLast4, Payment_PaymentMethod
		) VALUES (
			@p1, @p2, 'swn01', 'Pending',
			'a', 'b', 'named@example.com', 'line', 'TR', 'IST', '34000',
			'a', 'b', 'named@example.com', 'line', 'TR', 'IST', '34000',
			'pt_named', '4444', 1
		)`, orderID, customerID); err != nil {
		t.Fatalf("seeding a named order: %v", err)
	}

	if err := mssql.Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	// The order is still there and still reads as it did, minus the name.
	id, _ := domain.NewOrderID(orderID)
	order, err := mssql.NewOrderRepository(db, mssql.DropEvents).ByID(ctx, id)
	if err != nil {
		t.Fatalf("loading the order: %v", err)
	}
	if order.Status() != domain.OrderStatusPending || order.Payment().CardLast4() != "4444" {
		t.Errorf("order = %v, want it as it was stored", order)
	}

	var leftover int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM sys.columns
		WHERE object_id = OBJECT_ID('dbo.Orders') AND name = 'OrderName'`,
	).Scan(&leftover); err != nil {
		t.Fatalf("listing columns: %v", err)
	}
	if leftover != 0 {
		t.Error("the OrderName column survived the migration")
	}
}
