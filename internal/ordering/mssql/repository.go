package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

// OrderRepository stores orders in SQL Server.
//
// An order and its lines are written in one transaction: the total is derived
// from the lines, so a half-written order would be a wrong order rather than an
// incomplete one. The outbox row will join the same transaction later, which is
// what makes publishing atomic with the change it describes.
type OrderRepository struct {
	db       *sql.DB
	mapEvent EventMapper
}

// NewOrderRepository wires the repository. The mapper decides which domain
// events become outbox messages; see EventMapper.
func NewOrderRepository(db *sql.DB, mapEvent EventMapper) *OrderRepository {
	return &OrderRepository{db: db, mapEvent: mapEvent}
}

// Save inserts or replaces an order together with its lines and the outbox
// messages its changes produced.
func (r *OrderRepository) Save(ctx context.Context, order *domain.Order) error {
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		return r.saveOrder(ctx, tx, order)
	})
	if err != nil {
		return err
	}

	// The events are dropped only once they are committed. Clearing them before
	// the commit would lose them on a rollback, leaving an order that was saved
	// on a later attempt but never announced.
	order.PullEvents()

	return nil
}

// Place saves a new order together with the customer and the products it
// refers to, in one transaction.
//
// The order's foreign keys point at both, so writing them separately would
// leave a window - and after a crash, a row - where one exists without the
// other. In one transaction the checkout either produces a complete order or
// nothing, and the redelivery that follows a failure starts from scratch.
func (r *OrderRepository) Place(
	ctx context.Context, order *domain.Order, customer *domain.Customer, products []*domain.Product,
) error {
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		if err := saveCustomer(ctx, tx, customer); err != nil {
			return err
		}

		seen := make(map[domain.ProductID]bool, len(products))
		for _, product := range products {
			// The same product on two lines is one product.
			if seen[product.ID] {
				continue
			}
			seen[product.ID] = true

			if err := addProductIfUnknown(ctx, tx, product); err != nil {
				return err
			}
		}

		return r.saveOrder(ctx, tx, order)
	})
	if err != nil {
		return err
	}

	order.PullEvents()

	return nil
}

// saveCustomer records the customer, or refreshes what is known about them.
func saveCustomer(ctx context.Context, tx *sql.Tx, customer *domain.Customer) error {
	const query = `
		MERGE Customers WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS Id) AS source ON target.Id = source.Id
		WHEN MATCHED THEN UPDATE SET Name = @p2, Email = @p3, LastModifiedAt = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT (Id, Name, Email, CreatedAt)
			VALUES (@p1, @p2, @p3, SYSUTCDATETIME());`

	if _, err := tx.ExecContext(ctx, query, asGUID(customer.ID.UUID()), customer.Name, customer.Email); err != nil {
		return fmt.Errorf("recording customer %s: %w", customer.ID, err)
	}

	return nil
}

// addProductIfUnknown adds a product this service has not seen before.
//
// A product it already holds is left as it is. The table is Ordering's own
// reference data, written once from the first order that mentioned the
// product; letting every checkout overwrite it would make the stored name and
// price whatever the last basket happened to say. The lock is what keeps two
// orders introducing the same product at once from both trying to insert it.
func addProductIfUnknown(ctx context.Context, tx *sql.Tx, product *domain.Product) error {
	const query = `
		MERGE Products WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS Id) AS source ON target.Id = source.Id
		WHEN NOT MATCHED THEN INSERT (Id, Name, Price, CreatedAt)
			VALUES (@p1, @p2, @p3, SYSUTCDATETIME());`

	if _, err := tx.ExecContext(ctx, query, asGUID(product.ID.UUID()), product.Name, product.Price.String()); err != nil {
		return fmt.Errorf("recording product %s: %w", product.ID, err)
	}

	return nil
}

// ByID loads one order with its lines.
func (r *OrderRepository) ByID(ctx context.Context, id domain.OrderID) (*domain.Order, error) {
	row := r.db.QueryRowContext(ctx, selectOrders+` WHERE Id = @p1`, asGUID(id.UUID()))

	order, err := scanOrder(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFound("Order", id)
	}
	if err != nil {
		return nil, fmt.Errorf("loading order %s: %w", id, err)
	}

	items, err := r.itemsFor(ctx, id)
	if err != nil {
		return nil, err
	}

	return withItems(order, items), nil
}

// newestFirst is how every list of orders is sorted. Two orders can be created
// in the same instant, so the id breaks the tie: without it the same order
// could turn up on two pages, or on none.
const newestFirst = `
		ORDER BY CreatedAt DESC, Id`

// List returns a page of orders, newest first, with the total count.
func (r *OrderRepository) List(ctx context.Context, page pagination.Request) ([]*domain.Order, int64, error) {
	normalized := page.Normalized()

	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM Orders`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting orders: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, selectOrders+newestFirst+`
		OFFSET @p1 ROWS FETCH NEXT @p2 ROWS ONLY`,
		normalized.Offset(), normalized.Limit())
	if err != nil {
		return nil, 0, fmt.Errorf("listing orders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	orders, err := r.collect(ctx, rows)
	if err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

// ByCustomer returns every order belonging to a customer, newest first.
func (r *OrderRepository) ByCustomer(ctx context.Context, customerID domain.CustomerID) ([]*domain.Order, error) {
	rows, err := r.db.QueryContext(ctx, selectOrders+`
		WHERE CustomerId = @p1`+newestFirst,
		asGUID(customerID.UUID()))
	if err != nil {
		return nil, fmt.Errorf("listing orders for customer %s: %w", customerID, err)
	}
	defer func() { _ = rows.Close() }()

	return r.collect(ctx, rows)
}

// Delete removes an order. Its lines go with it through the cascade.
func (r *OrderRepository) Delete(ctx context.Context, id domain.OrderID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM Orders WHERE Id = @p1`, asGUID(id.UUID()))
	if err != nil {
		return fmt.Errorf("deleting order %s: %w", id, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking delete of order %s: %w", id, err)
	}
	if affected == 0 {
		return apperr.NotFound("Order", id)
	}

	return nil
}

// saveOrder writes the order, its lines and its outbox messages on the
// caller's transaction.
func (r *OrderRepository) saveOrder(ctx context.Context, tx *sql.Tx, order *domain.Order) error {
	if err := upsertOrder(ctx, tx, order); err != nil {
		return err
	}

	// Lines are replaced wholesale rather than diffed: the aggregate owns
	// them, so the stored set is whatever it currently holds.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM OrderItems WHERE OrderId = @p1`, asGUID(order.ID().UUID())); err != nil {
		return fmt.Errorf("clearing order lines: %w", err)
	}

	for _, item := range order.Items() {
		if err := insertOrderItem(ctx, tx, order.ID(), item); err != nil {
			return err
		}
	}

	return r.stageEvents(ctx, tx, order.Events())
}

// inTx runs fn in a transaction, rolling back on any failure.
func (r *OrderRepository) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		// The rollback error is deliberately dropped: the original failure is
		// what the caller needs, and reporting the rollback would hide it.
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// collect reads orders from rows and attaches their lines.
func (r *OrderRepository) collect(ctx context.Context, rows *sql.Rows) ([]*domain.Order, error) {
	var orders []*domain.Order

	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("reading order: %w", err)
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading orders: %w", err)
	}

	// Lines are fetched per order. The page size is bounded, so this stays a
	// small, predictable number of queries rather than one per row of a table.
	for i, order := range orders {
		items, err := r.itemsFor(ctx, order.ID())
		if err != nil {
			return nil, err
		}
		orders[i] = withItems(order, items)
	}

	return orders, nil
}

func (r *OrderRepository) itemsFor(ctx context.Context, id domain.OrderID) ([]domain.OrderItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT Id, OrderId, ProductId, Quantity, Price
		FROM OrderItems WHERE OrderId = @p1 ORDER BY Id`, asGUID(id.UUID()))
	if err != nil {
		return nil, fmt.Errorf("loading lines of order %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()

	var items []domain.OrderItem
	for rows.Next() {
		item, err := scanOrderItem(rows)
		if err != nil {
			return nil, fmt.Errorf("reading line of order %s: %w", id, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading lines of order %s: %w", id, err)
	}

	return items, nil
}

func upsertOrder(ctx context.Context, tx *sql.Tx, order *domain.Order) error {
	payment := order.Payment()
	shipping := order.ShippingAddress()
	billing := order.BillingAddress()

	// An order without a payment stores NULLs rather than empty strings, so
	// "no payment" cannot be mistaken for a token nobody filled in.
	var token, last4, method any
	if !payment.IsZero() {
		token, last4, method = payment.PaymentToken(), payment.CardLast4(), payment.PaymentMethod()
	}

	const query = `
		MERGE Orders WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS Id) AS source ON target.Id = source.Id
		WHEN MATCHED THEN UPDATE SET
			CustomerId = @p2, Status = @p3,
			ShippingAddress_FirstName = @p4, ShippingAddress_LastName = @p5,
			ShippingAddress_EmailAddress = @p6, ShippingAddress_AddressLine = @p7,
			ShippingAddress_Country = @p8, ShippingAddress_State = @p9, ShippingAddress_ZipCode = @p10,
			BillingAddress_FirstName = @p11, BillingAddress_LastName = @p12,
			BillingAddress_EmailAddress = @p13, BillingAddress_AddressLine = @p14,
			BillingAddress_Country = @p15, BillingAddress_State = @p16, BillingAddress_ZipCode = @p17,
			Payment_PaymentToken = @p18, Payment_CardLast4 = @p19, Payment_PaymentMethod = @p20,
			LastModifiedAt = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT (
			Id, CustomerId, Status,
			ShippingAddress_FirstName, ShippingAddress_LastName, ShippingAddress_EmailAddress,
			ShippingAddress_AddressLine, ShippingAddress_Country, ShippingAddress_State, ShippingAddress_ZipCode,
			BillingAddress_FirstName, BillingAddress_LastName, BillingAddress_EmailAddress,
			BillingAddress_AddressLine, BillingAddress_Country, BillingAddress_State, BillingAddress_ZipCode,
			Payment_PaymentToken, Payment_CardLast4, Payment_PaymentMethod,
			CreatedAt
		) VALUES (
			@p1, @p2, @p3,
			@p4, @p5, @p6, @p7, @p8, @p9, @p10,
			@p11, @p12, @p13, @p14, @p15, @p16, @p17,
			@p18, @p19, @p20,
			SYSUTCDATETIME()
		);`

	_, err := tx.ExecContext(ctx, query,
		asGUID(order.ID().UUID()), asGUID(order.CustomerID().UUID()), order.Status().String(),
		shipping.FirstName, shipping.LastName, nullable(shipping.EmailAddress), shipping.AddressLine,
		shipping.Country, shipping.State, shipping.ZipCode,
		billing.FirstName, billing.LastName, nullable(billing.EmailAddress), billing.AddressLine,
		billing.Country, billing.State, billing.ZipCode,
		token, last4, method,
	)
	if err != nil {
		if isForeignKeyViolation(err) {
			return apperr.BadRequest("customer %s does not exist", order.CustomerID())
		}
		return fmt.Errorf("saving order %s: %w", order.ID(), err)
	}

	return nil
}

func insertOrderItem(ctx context.Context, tx *sql.Tx, orderID domain.OrderID, item domain.OrderItem) error {
	const query = `
		INSERT INTO OrderItems (Id, OrderId, ProductId, Quantity, Price, CreatedAt)
		VALUES (@p1, @p2, @p3, @p4, @p5, SYSUTCDATETIME())`

	_, err := tx.ExecContext(ctx, query,
		asGUID(item.ID.UUID()), asGUID(orderID.UUID()), asGUID(item.ProductID.UUID()), item.Quantity, item.Price.String())
	if err != nil {
		if isForeignKeyViolation(err) {
			return apperr.BadRequest("product %s does not exist", item.ProductID)
		}
		return fmt.Errorf("saving line of order %s: %w", orderID, err)
	}

	return nil
}

// nullable turns an empty string into a NULL, so an absent optional value is
// stored as absent rather than as an empty string.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// isForeignKeyViolation reports whether err is a foreign key failure, which
// means the caller referenced something that does not exist rather than the
// database being broken.
func isForeignKeyViolation(err error) bool {
	const foreignKeyViolation = 547

	var mssqlErr mssql.Error
	if errors.As(err, &mssqlErr) {
		return mssqlErr.Number == foreignKeyViolation
	}

	return false
}

// decimalFromString parses a money value read back from the database.
func decimalFromString(value string) (decimal.Decimal, error) {
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, fmt.Errorf("parsing money value %q: %w", value, err)
	}
	return parsed, nil
}
