package mssql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
)

// seedNamespace derives stable ids for the sample data, so a reset database
// produces the same ids and fixtures referring to them keep working.
var seedNamespace = uuid.MustParse("2f5b7d10-6c3a-4f5e-9c2b-7a1d4e8f0b62")

// SeedCustomer is one of the sample buyers.
type seedEntry struct {
	name  string
	email string
}

// Seed inserts sample customers, products and orders when the database is
// empty. It is a no-op otherwise, so running it on every start is safe.
func Seed(ctx context.Context, db *sql.DB) (int, error) {
	var existing int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM Customers`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("counting customers before seeding: %w", err)
	}
	if existing > 0 {
		return 0, nil
	}

	customers := sampleCustomers()
	products := sampleProducts()

	if err := insertCustomers(ctx, db, customers); err != nil {
		return 0, err
	}
	if err := insertProducts(ctx, db, products); err != nil {
		return 0, err
	}

	orders, err := sampleOrders(customers, products)
	if err != nil {
		return 0, err
	}

	// Seeding maps no events: see DropEvents.
	repo := NewOrderRepository(db, DropEvents)
	for _, order := range orders {
		if err := repo.Save(ctx, order); err != nil {
			return 0, fmt.Errorf("seeding order %s: %w", order.OrderName, err)
		}
	}

	return len(customers) + len(products) + len(orders), nil
}

func sampleCustomers() []*domain.Customer {
	entries := []seedEntry{
		{"mehmet", "mehmet@gmail.com"},
		{"john", "john@gmail.com"},
	}

	customers := make([]*domain.Customer, 0, len(entries))
	for _, entry := range entries {
		id, _ := domain.NewCustomerID(uuid.NewSHA1(seedNamespace, []byte("customer:"+entry.name)))
		customer, _ := domain.NewCustomer(id, entry.name, entry.email)
		customers = append(customers, customer)
	}

	return customers
}

func sampleProducts() []*domain.Product {
	entries := []struct {
		name  string
		price int64
	}{
		{"IPhone X", 500},
		{"Samsung 10", 400},
		{"Huawei Plus", 650},
		{"Xiaomi Mi", 450},
	}

	products := make([]*domain.Product, 0, len(entries))
	for _, entry := range entries {
		id, _ := domain.NewProductID(uuid.NewSHA1(seedNamespace, []byte("product:"+entry.name)))
		product, _ := domain.NewProduct(id, entry.name, decimal.NewFromInt(entry.price))
		products = append(products, product)
	}

	return products
}

func sampleOrders(customers []*domain.Customer, products []*domain.Product) ([]*domain.Order, error) {
	address, err := domain.NewAddress("mehmet", "ozkaya", "mehmet@gmail.com", "Bahcelievler No:4", "Turkey", "Istanbul", "38050")
	if err != nil {
		return nil, err
	}
	payment, err := domain.NewPayment("mehmet", "5555555555554444", "12/28", "355", 1)
	if err != nil {
		return nil, err
	}
	orderName, err := domain.NewOrderName("ORD_1")
	if err != nil {
		return nil, err
	}
	orderID, err := domain.NewOrderID(uuid.NewSHA1(seedNamespace, []byte("order:ORD_1")))
	if err != nil {
		return nil, err
	}

	order := domain.NewOrder(orderID, customers[0].ID, orderName, address, address, payment)
	if err := order.Add(products[0].ID, 2, products[0].Price); err != nil {
		return nil, err
	}
	if err := order.Add(products[1].ID, 1, products[1].Price); err != nil {
		return nil, err
	}

	return []*domain.Order{order}, nil
}

func insertCustomers(ctx context.Context, db *sql.DB, customers []*domain.Customer) error {
	const query = `
		INSERT INTO Customers (Id, Name, Email, CreatedAt)
		VALUES (@p1, @p2, @p3, SYSUTCDATETIME())`

	for _, customer := range customers {
		if _, err := db.ExecContext(ctx, query, customer.ID.UUID(), customer.Name, customer.Email); err != nil {
			return fmt.Errorf("seeding customer %s: %w", customer.Name, err)
		}
	}

	return nil
}

func insertProducts(ctx context.Context, db *sql.DB, products []*domain.Product) error {
	const query = `
		INSERT INTO Products (Id, Name, Price, CreatedAt)
		VALUES (@p1, @p2, @p3, SYSUTCDATETIME())`

	for _, product := range products {
		if _, err := db.ExecContext(ctx, query, product.ID.UUID(), product.Name, product.Price.String()); err != nil {
			return fmt.Errorf("seeding product %s: %w", product.Name, err)
		}
	}

	return nil
}
