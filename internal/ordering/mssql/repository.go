package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
		if err := upsertOrder(ctx, tx, order); err != nil {
			return err
		}

		// Lines are replaced wholesale rather than diffed: the aggregate owns
		// them, so the stored set is whatever it currently holds.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM OrderItems WHERE OrderId = @p1`, asGUID(order.ID.UUID())); err != nil {
			return fmt.Errorf("clearing order lines: %w", err)
		}

		for _, item := range order.Items() {
			if err := insertOrderItem(ctx, tx, order.ID, item); err != nil {
				return err
			}
		}

		return r.stageEvents(ctx, tx, order.Events())
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

// List returns a page of orders, newest first, with the total count.
func (r *OrderRepository) List(ctx context.Context, page pagination.Request) ([]*domain.Order, int64, error) {
	normalized := page.Normalized()

	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM Orders`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting orders: %w", err)
	}

	// A deterministic order is required for paging to be stable; OrderName is
	// unique enough to break ties on identical timestamps.
	rows, err := r.db.QueryContext(ctx, selectOrders+`
		ORDER BY CreatedAt DESC, OrderName
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

// ByName returns every order whose name contains the search term.
//
// It is a search rather than a lookup: order names are short references, and
// asking for "ORD" should find ORD_1 and ORD_2.
func (r *OrderRepository) ByName(ctx context.Context, name string) ([]*domain.Order, error) {
	rows, err := r.db.QueryContext(ctx, selectOrders+`
		WHERE OrderName LIKE @p1 ESCAPE '\'
		ORDER BY CreatedAt DESC, OrderName`,
		"%"+escapeLike(name)+"%")
	if err != nil {
		return nil, fmt.Errorf("listing orders named %q: %w", name, err)
	}
	defer func() { _ = rows.Close() }()

	return r.collect(ctx, rows)
}

// ByCustomer returns every order belonging to a customer.
func (r *OrderRepository) ByCustomer(ctx context.Context, customerID domain.CustomerID) ([]*domain.Order, error) {
	rows, err := r.db.QueryContext(ctx, selectOrders+`
		WHERE CustomerId = @p1
		ORDER BY CreatedAt DESC, OrderName`,
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
		items, err := r.itemsFor(ctx, order.ID)
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
	payment := order.Payment
	shipping := order.ShippingAddress
	billing := order.BillingAddress

	const query = `
		MERGE Orders WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS Id) AS source ON target.Id = source.Id
		WHEN MATCHED THEN UPDATE SET
			CustomerId = @p2, OrderName = @p3, Status = @p4,
			ShippingAddress_FirstName = @p5, ShippingAddress_LastName = @p6,
			ShippingAddress_EmailAddress = @p7, ShippingAddress_AddressLine = @p8,
			ShippingAddress_Country = @p9, ShippingAddress_State = @p10, ShippingAddress_ZipCode = @p11,
			BillingAddress_FirstName = @p12, BillingAddress_LastName = @p13,
			BillingAddress_EmailAddress = @p14, BillingAddress_AddressLine = @p15,
			BillingAddress_Country = @p16, BillingAddress_State = @p17, BillingAddress_ZipCode = @p18,
			Payment_CardName = @p19, Payment_CardNumber = @p20, Payment_Expiration = @p21,
			Payment_CVV = @p22, Payment_PaymentMethod = @p23,
			LastModifiedAt = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN INSERT (
			Id, CustomerId, OrderName, Status,
			ShippingAddress_FirstName, ShippingAddress_LastName, ShippingAddress_EmailAddress,
			ShippingAddress_AddressLine, ShippingAddress_Country, ShippingAddress_State, ShippingAddress_ZipCode,
			BillingAddress_FirstName, BillingAddress_LastName, BillingAddress_EmailAddress,
			BillingAddress_AddressLine, BillingAddress_Country, BillingAddress_State, BillingAddress_ZipCode,
			Payment_CardName, Payment_CardNumber, Payment_Expiration, Payment_CVV, Payment_PaymentMethod,
			CreatedAt
		) VALUES (
			@p1, @p2, @p3, @p4,
			@p5, @p6, @p7, @p8, @p9, @p10, @p11,
			@p12, @p13, @p14, @p15, @p16, @p17, @p18,
			@p19, @p20, @p21, @p22, @p23,
			SYSUTCDATETIME()
		);`

	_, err := tx.ExecContext(ctx, query,
		asGUID(order.ID.UUID()), asGUID(order.CustomerID.UUID()), order.OrderName.String(), order.Status.String(),
		shipping.FirstName, shipping.LastName, nullable(shipping.EmailAddress), shipping.AddressLine,
		shipping.Country, shipping.State, shipping.ZipCode,
		billing.FirstName, billing.LastName, nullable(billing.EmailAddress), billing.AddressLine,
		billing.Country, billing.State, billing.ZipCode,
		nullable(payment.CardName()), payment.CardNumber(), payment.Expiration(),
		payment.CVV(), payment.PaymentMethod(),
	)
	if err != nil {
		if isForeignKeyViolation(err) {
			return apperr.BadRequest("customer %s does not exist", order.CustomerID)
		}
		return fmt.Errorf("saving order %s: %w", order.ID, err)
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

// escapeLike neutralises the wildcards SQL Server's LIKE understands.
//
// The term is already a bound parameter, so this is not about SQL injection: it
// is that a search for "%" would otherwise match every order, and one for "_"
// every single-character name. Escaping keeps a search term meaning itself.
func escapeLike(value string) string {
	return likeEscaper.Replace(value)
}

// The backslash is declared as the escape character by the queries that use it.
var likeEscaper = strings.NewReplacer(
	`\`, `\\`,
	"%", `\%`,
	"_", `\_`,
	"[", `\[`,
)

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
