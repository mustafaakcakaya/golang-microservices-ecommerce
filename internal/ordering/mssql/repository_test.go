package mssql_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	tcmssql "github.com/testcontainers/testcontainers-go/modules/mssql"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

const (
	cardNumber = "5555555555554444"
	cvv        = "355"
)

// One SQL Server is shared by the whole package. It takes tens of seconds to
// become ready, so a container per test would dominate the run; tests therefore
// create their own rows and never assume an empty database.
//
// Setup lives in TestMain rather than a helper's t.Cleanup: cleanup registered
// on the first test's t runs when that test ends, which would tear the
// container down while the rest of the package still needs it.
var sharedDB *sql.DB

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	ctx := context.Background()

	container, err := tcmssql.Run(ctx, "mcr.microsoft.com/mssql/server:2022-latest",
		tcmssql.WithAcceptEULA(),
		tcmssql.WithPassword("Str0ng!Passw0rd"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("SQL Server is now ready for client connections").
				WithStartupTimeout(5*time.Minute),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting sql server: %v\n", err)
		os.Exit(1)
	}

	code := run(ctx, container, m)

	if err := testcontainers.TerminateContainer(container); err != nil {
		fmt.Fprintf(os.Stderr, "terminating sql server: %v\n", err)
	}

	os.Exit(code)
}

// run owns the database lifetime so its defers execute before os.Exit.
func run(ctx context.Context, container *tcmssql.MSSQLServerContainer, m *testing.M) int {
	dsn, err := container.ConnectionString(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		return 1
	}

	db, err := mssql.Open(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "opening database: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	if err := mssql.Migrate(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "migrating: %v\n", err)
		return 1
	}

	sharedDB = db

	return m.Run()
}

func newRepository(t *testing.T) *mssql.OrderRepository {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}

	return mssql.NewOrderRepository(sharedDB)
}

// seedCustomer inserts a customer so orders have something to reference.
func seedCustomer(t *testing.T, db *sql.DB) domain.CustomerID {
	t.Helper()

	id, err := domain.NewCustomerID(uuid.New())
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}

	_, err = db.ExecContext(t.Context(),
		`INSERT INTO Customers (Id, Name, Email) VALUES (@p1, @p2, @p3)`,
		id.UUID(), "test customer", "test@example.com")
	if err != nil {
		t.Fatalf("seeding customer: %v", err)
	}

	return id
}

func seedProduct(t *testing.T, db *sql.DB) domain.ProductID {
	t.Helper()

	id, err := domain.NewProductID(uuid.New())
	if err != nil {
		t.Fatalf("product id: %v", err)
	}

	_, err = db.ExecContext(t.Context(),
		`INSERT INTO Products (Id, Name, Price) VALUES (@p1, @p2, @p3)`,
		id.UUID(), "test product", "100.00")
	if err != nil {
		t.Fatalf("seeding product: %v", err)
	}

	return id
}

func newOrder(t *testing.T, customerID domain.CustomerID, name string) *domain.Order {
	t.Helper()

	orderID, err := domain.NewOrderID(uuid.New())
	if err != nil {
		t.Fatalf("order id: %v", err)
	}
	orderName, err := domain.NewOrderName(name)
	if err != nil {
		t.Fatalf("order name: %v", err)
	}
	address, err := domain.NewAddress("Mustafa", "Akcakaya", "mustafa@example.com", "Test Address", "Turkey", "Istanbul", "34000")
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	payment, err := domain.NewPayment("Mustafa Akcakaya", cardNumber, "12/28", cvv, 1)
	if err != nil {
		t.Fatalf("payment: %v", err)
	}

	return domain.NewOrder(orderID, customerID, orderName, address, address, payment)
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	repo := newRepository(t)
	db := sharedDB

	customerID := seedCustomer(t, db)
	productID := seedProduct(t, db)

	order := newOrder(t, customerID, "ORD_1")
	if err := order.Add(productID, 2, decimal.NewFromInt(500)); err != nil {
		t.Fatalf("adding line: %v", err)
	}

	if err := repo.Save(t.Context(), order); err != nil {
		t.Fatalf("saving: %v", err)
	}

	got, err := repo.ByID(t.Context(), order.ID)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	if got.OrderName.String() != "ORD_1" || got.Status != domain.OrderStatusPending {
		t.Errorf("order = %s/%v, want ORD_1/Pending", got.OrderName, got.Status)
	}
	if got.ShippingAddress.AddressLine != "Test Address" {
		t.Errorf("shipping address = %+v, want the stored one", got.ShippingAddress)
	}
	// Payment survives storage; it is redacted when rendered, not when stored.
	if got.Payment.CardNumber() != cardNumber || got.Payment.CVV() != cvv {
		t.Error("payment details did not round trip")
	}

	items := got.Items()
	if len(items) != 1 || items[0].Quantity != 2 {
		t.Fatalf("items = %+v, want the single stored line", items)
	}
	// Money must not drift: a float column would not survive this.
	if !items[0].Price.Equal(decimal.NewFromInt(500)) {
		t.Errorf("price = %s, want 500", items[0].Price)
	}
	if !got.TotalPrice().Equal(decimal.NewFromInt(1000)) {
		t.Errorf("total = %s, want 1000", got.TotalPrice())
	}
}

func TestLoadingAnUnknownOrderReportsNotFound(t *testing.T) {
	repo := newRepository(t)

	id, err := domain.NewOrderID(uuid.New())
	if err != nil {
		t.Fatalf("order id: %v", err)
	}

	_, err = repo.ByID(t.Context(), id)

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound (error %v)", got, err)
	}
}

func TestSavingReplacesLinesRatherThanAppending(t *testing.T) {
	repo := newRepository(t)
	db := sharedDB

	customerID := seedCustomer(t, db)
	first := seedProduct(t, db)
	second := seedProduct(t, db)

	order := newOrder(t, customerID, "ORD_2")
	if err := order.Add(first, 1, decimal.NewFromInt(100)); err != nil {
		t.Fatalf("adding: %v", err)
	}
	if err := repo.Save(t.Context(), order); err != nil {
		t.Fatalf("saving: %v", err)
	}

	order.Remove(first)
	if err := order.Add(second, 3, decimal.NewFromInt(50)); err != nil {
		t.Fatalf("adding: %v", err)
	}
	if err := repo.Save(t.Context(), order); err != nil {
		t.Fatalf("re-saving: %v", err)
	}

	got, err := repo.ByID(t.Context(), order.ID)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	// The aggregate owns its lines, so the stored set is whatever it holds now.
	items := got.Items()
	if len(items) != 1 || items[0].ProductID != second || items[0].Quantity != 3 {
		t.Errorf("items = %+v, want only the replacement line", items)
	}
}

func TestOrderAndLinesCommitTogether(t *testing.T) {
	repo := newRepository(t)
	db := sharedDB

	customerID := seedCustomer(t, db)

	order := newOrder(t, customerID, "ORD_3")
	// A product that does not exist trips the foreign key on the line insert,
	// after the order row has already been written inside the transaction.
	missingProduct, err := domain.NewProductID(uuid.New())
	if err != nil {
		t.Fatalf("product id: %v", err)
	}
	if err := order.Add(missingProduct, 1, decimal.NewFromInt(10)); err != nil {
		t.Fatalf("adding: %v", err)
	}

	if err := repo.Save(t.Context(), order); err == nil {
		t.Fatal("saving a line for a missing product should fail")
	}

	// The order must not survive its own failed save.
	if _, err := repo.ByID(t.Context(), order.ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("order was written despite the failure (error %v)", err)
	}
}

func TestSavingForAnUnknownCustomerIsARequestError(t *testing.T) {
	repo := newRepository(t)

	unknown, err := domain.NewCustomerID(uuid.New())
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}

	err = repo.Save(t.Context(), newOrder(t, unknown, "ORD_4"))

	// A missing customer is the caller's mistake, not a broken database.
	if got := apperr.KindOf(err); got != apperr.KindBadRequest {
		t.Errorf("kind = %v, want KindBadRequest (error %v)", got, err)
	}
}

func TestListPagesAndCountsAllOrders(t *testing.T) {
	repo := newRepository(t)
	db := sharedDB

	customerID := seedCustomer(t, db)
	for _, name := range []string{"ORD_A", "ORD_B", "ORD_C"} {
		if err := repo.Save(t.Context(), newOrder(t, customerID, name)); err != nil {
			t.Fatalf("saving %s: %v", name, err)
		}
	}

	page, total, err := repo.List(t.Context(), pagination.Request{PageIndex: 0, PageSize: 2})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	if len(page) != 2 {
		t.Errorf("page = %d orders, want 2", len(page))
	}
	// The count is the total, not the page size, and the database holds rows
	// from other tests too.
	if total < 3 {
		t.Errorf("total = %d, want at least the three just written", total)
	}
}

func TestByNameAndByCustomerFilter(t *testing.T) {
	repo := newRepository(t)
	db := sharedDB

	mine := seedCustomer(t, db)
	other := seedCustomer(t, db)

	if err := repo.Save(t.Context(), newOrder(t, mine, "ORD_M")); err != nil {
		t.Fatalf("saving: %v", err)
	}
	if err := repo.Save(t.Context(), newOrder(t, other, "ORD_O")); err != nil {
		t.Fatalf("saving: %v", err)
	}

	byName, err := repo.ByName(t.Context(), "ORD_M")
	if err != nil {
		t.Fatalf("by name: %v", err)
	}
	if len(byName) != 1 || byName[0].OrderName.String() != "ORD_M" {
		t.Errorf("by name returned %d orders, want the one named ORD_M", len(byName))
	}

	byCustomer, err := repo.ByCustomer(t.Context(), mine)
	if err != nil {
		t.Fatalf("by customer: %v", err)
	}
	if len(byCustomer) != 1 || byCustomer[0].CustomerID != mine {
		t.Errorf("by customer returned %d orders, want only this customer's", len(byCustomer))
	}
}

func TestDeleteRemovesTheOrderAndItsLines(t *testing.T) {
	repo := newRepository(t)
	db := sharedDB

	customerID := seedCustomer(t, db)
	productID := seedProduct(t, db)

	order := newOrder(t, customerID, "ORD_D")
	if err := order.Add(productID, 1, decimal.NewFromInt(10)); err != nil {
		t.Fatalf("adding: %v", err)
	}
	if err := repo.Save(t.Context(), order); err != nil {
		t.Fatalf("saving: %v", err)
	}

	if err := repo.Delete(t.Context(), order.ID); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	// Lines have no life without their order, so the cascade must take them.
	var remaining int
	if err := db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM OrderItems WHERE OrderId = @p1`, order.ID.UUID()).Scan(&remaining); err != nil {
		t.Fatalf("counting lines: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d lines survived the order", remaining)
	}

	// Deleting an order that is gone is an error here: the caller asked to
	// remove something specific that was not there.
	if err := repo.Delete(t.Context(), order.ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("second delete = %v, want NotFound", err)
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	repo := newRepository(t)
	db := sharedDB

	inserted, err := mssql.Seed(t.Context(), db)
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// Other tests may have written first; seeding only runs on an empty
	// database, so either it inserted the full sample or nothing at all.
	if inserted != 0 && inserted != 7 {
		t.Fatalf("inserted = %d, want 0 or the full sample", inserted)
	}

	again, err := mssql.Seed(t.Context(), db)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if again != 0 {
		t.Errorf("second run inserted %d, want 0", again)
	}

	if inserted > 0 {
		orders, err := repo.ByName(t.Context(), "ORD_1")
		if err != nil {
			t.Fatalf("loading seeded order: %v", err)
		}
		if len(orders) != 1 || !orders[0].TotalPrice().Equal(decimal.NewFromInt(1400)) {
			t.Errorf("seeded order total = %v, want 1400", orders)
		}
	}
}
