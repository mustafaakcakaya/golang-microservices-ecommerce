package outbox_test

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	tcmssql "github.com/testcontainers/testcontainers-go/modules/mssql"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/integration"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/mssql"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/outbox"
)

// captureTimeout bounds the wait for the capture job, which runs on its own
// schedule. Only that changes arrive is worth asserting, not how fast.
const captureTimeout = 90 * time.Second

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
		testcontainers.WithEnv(map[string]string{
			// Capture needs an edition that supports it and a running agent to
			// copy changes into the change table.
			"MSSQL_PID":           "Developer",
			"MSSQL_AGENT_ENABLED": "true",
		}),
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

func run(ctx context.Context, container *tcmssql.MSSQLServerContainer, m *testing.M) int {
	adminDSN, err := container.ConnectionString(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		return 1
	}

	admin, err := mssql.Open(adminDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "opening database: %v\n", err)
		return 1
	}
	if err := waitForLogin(ctx, admin); err != nil {
		_ = admin.Close()
		fmt.Fprintf(os.Stderr, "waiting for sql server: %v\n", err)
		return 1
	}
	_ = admin.Close()

	// Capture cannot be enabled on a system database, so the tests need one of
	// their own.
	dsn, err := databaseDSN(adminDSN, "Ordering")
	if err != nil {
		fmt.Fprintf(os.Stderr, "building connection string: %v\n", err)
		return 1
	}
	if err := mssql.EnsureDatabase(ctx, dsn); err != nil {
		fmt.Fprintf(os.Stderr, "creating database: %v\n", err)
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

// waitForLogin polls until the server accepts a connection: the readiness log
// line is printed before the sa password has been applied.
func waitForLogin(ctx context.Context, db *sql.DB) error {
	const attempts = 60

	var err error
	for range attempts {
		if err = db.PingContext(ctx); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}

	return err
}

func databaseDSN(dsn, name string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	query.Set("database", name)
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func skipWithoutDocker(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping test that needs Docker")
	}
}

// placeOrder stores a new order and returns it, along with the number of
// messages its save produced.
func placeOrder(t *testing.T, name string, alsoUpdate bool) *domain.Order {
	t.Helper()

	repo := mssql.NewOrderRepository(sharedDB, integration.Map)

	customerID := seedCustomer(t)
	productID := seedProduct(t)

	orderID, err := domain.NewOrderID(uuid.New())
	if err != nil {
		t.Fatalf("order id: %v", err)
	}
	orderName, err := domain.NewOrderName(name)
	if err != nil {
		t.Fatalf("order name: %v", err)
	}
	address, err := domain.NewAddress("Mustafa", "Akcakaya", "mustafa@example.com",
		"Test Address", "Turkey", "Istanbul", "34000")
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	payment, err := domain.NewPayment("Mustafa", "5555555555554444", "12/28", "355", 1)
	if err != nil {
		t.Fatalf("payment: %v", err)
	}

	order := domain.NewOrder(orderID, customerID, orderName, address, address, payment)
	if err := order.Add(productID, 2, decimal.NewFromInt(250)); err != nil {
		t.Fatalf("adding line: %v", err)
	}

	if alsoUpdate {
		// A second change before the save, so one transaction produces two
		// messages.
		if err := order.Update(orderName, address, address, payment, domain.OrderStatusCompleted); err != nil {
			t.Fatalf("updating: %v", err)
		}
	}

	if err := repo.Save(t.Context(), order); err != nil {
		t.Fatalf("saving: %v", err)
	}

	return order
}

func seedCustomer(t *testing.T) domain.CustomerID {
	t.Helper()

	id, err := domain.NewCustomerID(uuid.New())
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}
	if _, err := sharedDB.ExecContext(t.Context(),
		`INSERT INTO Customers (Id, Name, Email) VALUES (@p1, @p2, @p3)`,
		id.UUID(), "test customer", "test@example.com"); err != nil {
		t.Fatalf("seeding customer: %v", err)
	}

	return id
}

func seedProduct(t *testing.T) domain.ProductID {
	t.Helper()

	id, err := domain.NewProductID(uuid.New())
	if err != nil {
		t.Fatalf("product id: %v", err)
	}
	if _, err := sharedDB.ExecContext(t.Context(),
		`INSERT INTO Products (Id, Name, Price) VALUES (@p1, @p2, @p3)`,
		id.UUID(), "test product", "100.00"); err != nil {
		t.Fatalf("seeding product: %v", err)
	}

	return id
}

// waitForGroup polls until the aggregate's messages have been captured, and
// returns the group holding them.
func waitForGroup(t *testing.T, reader *outbox.Reader, after outbox.LSN, aggregateID string, want int) outbox.Group {
	t.Helper()

	deadline := time.Now().Add(captureTimeout)
	var lastErr error

	for time.Now().Before(deadline) {
		batch, err := reader.Read(t.Context(), after, 100)
		lastErr = err

		// Waiting is only reasonable while capture is still starting up.
		// Anything else is a fault, and polling it for a minute and a half
		// only hides what went wrong.
		var notReady *outbox.NotReadyError
		if err != nil && !errors.As(err, &notReady) {
			t.Fatalf("reading captured changes: %v", err)
		}

		if err == nil {
			for _, group := range batch.Groups {
				if group.Messages[0].AggregateID == aggregateID && len(group.Messages) == want {
					return group
				}
			}
		}

		time.Sleep(time.Second)
	}

	t.Fatalf("the messages of %s were never captured (last error: %v)", aggregateID, lastErr)

	return outbox.Group{}
}
