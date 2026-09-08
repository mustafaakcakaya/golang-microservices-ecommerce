package orders

import (
	"context"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

// Repository is the persistence seam the handlers depend on. The SQL Server
// implementation lives in the mssql package; tests can substitute a fake.
//
// It speaks in aggregates rather than rows: an order and its lines are saved
// and loaded as one unit, because the invariants that span them have a single
// owner. That is also what will let the outbox row join the same transaction
// without any handler knowing about it.
type Repository interface {
	// Save inserts or replaces the order together with its lines.
	Save(ctx context.Context, order *domain.Order) error

	// ByID returns apperr.NotFound when no order has the id.
	ByID(ctx context.Context, id domain.OrderID) (*domain.Order, error)

	// List returns one page of orders and the total count.
	List(ctx context.Context, page pagination.Request) ([]*domain.Order, int64, error)

	// ByName returns every order whose name matches the search term.
	ByName(ctx context.Context, name string) ([]*domain.Order, error)

	// ByCustomer returns every order belonging to a customer.
	ByCustomer(ctx context.Context, customerID domain.CustomerID) ([]*domain.Order, error)

	// Delete removes the order. Unlike the catalog's delete, removing an
	// unknown order reports apperr.NotFound: an order is a record of something
	// that happened, so a delete that matched nothing is worth telling the
	// caller about rather than answering as if it had worked.
	Delete(ctx context.Context, id domain.OrderID) error
}
