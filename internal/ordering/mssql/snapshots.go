package mssql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
)

// SnapshotStore records the customers and products an order refers to.
//
// Ordering keeps its own copies rather than reaching into Catalog: an order has
// to stay readable years later, when the product may have been renamed,
// repriced or removed entirely. The rows are also what the order's foreign keys
// point at, so an order can only refer to something this service has recorded.
type SnapshotStore struct {
	db *sql.DB
}

// NewSnapshotStore wires the store.
func NewSnapshotStore(db *sql.DB) *SnapshotStore {
	return &SnapshotStore{db: db}
}

// SaveCustomer records the customer, or refreshes what is known about them.
func (s *SnapshotStore) SaveCustomer(
	ctx context.Context, id domain.CustomerID, name, email string,
) error {
	const query = `
		MERGE Customers WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS Id) AS source ON target.Id = source.Id
		WHEN MATCHED THEN UPDATE SET Name = @p2, Email = @p3, LastModifiedAt = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT (Id, Name, Email, CreatedAt)
			VALUES (@p1, @p2, @p3, SYSUTCDATETIME());`

	if _, err := s.db.ExecContext(ctx, query, asGUID(id.UUID()), name, email); err != nil {
		return fmt.Errorf("recording customer %s: %w", id, err)
	}

	return nil
}

// SaveProduct records the product as the order refers to it.
//
// The price is the one the order was placed at, not whatever the catalog says
// now. This table is a record of orders, not a mirror of the catalog.
func (s *SnapshotStore) SaveProduct(
	ctx context.Context, id domain.ProductID, name string, price decimal.Decimal,
) error {
	const query = `
		MERGE Products WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS Id) AS source ON target.Id = source.Id
		WHEN MATCHED THEN UPDATE SET Name = @p2, Price = @p3, LastModifiedAt = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT (Id, Name, Price, CreatedAt)
			VALUES (@p1, @p2, @p3, SYSUTCDATETIME());`

	if _, err := s.db.ExecContext(ctx, query, asGUID(id.UUID()), name, price.String()); err != nil {
		return fmt.Errorf("recording product %s: %w", id, err)
	}

	return nil
}
