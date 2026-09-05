package domain

import "github.com/google/uuid"

// Identifiers are distinct types rather than bare UUIDs so the compiler rejects
// passing a ProductID where an OrderID belongs - a mistake that is otherwise
// invisible at a call site taking several ids.

// OrderID identifies an order.
type OrderID uuid.UUID

// NewOrderID validates and wraps an order id.
func NewOrderID(value uuid.UUID) (OrderID, error) {
	if value == uuid.Nil {
		return OrderID{}, invalidf("OrderId cannot be empty")
	}
	return OrderID(value), nil
}

// UUID returns the underlying value.
func (id OrderID) UUID() uuid.UUID { return uuid.UUID(id) }

// String renders the id.
func (id OrderID) String() string { return uuid.UUID(id).String() }

// CustomerID identifies a customer.
type CustomerID uuid.UUID

// NewCustomerID validates and wraps a customer id.
func NewCustomerID(value uuid.UUID) (CustomerID, error) {
	if value == uuid.Nil {
		return CustomerID{}, invalidf("CustomerId cannot be empty")
	}
	return CustomerID(value), nil
}

// UUID returns the underlying value.
func (id CustomerID) UUID() uuid.UUID { return uuid.UUID(id) }

// String renders the id.
func (id CustomerID) String() string { return uuid.UUID(id).String() }

// ProductID identifies a product.
type ProductID uuid.UUID

// NewProductID validates and wraps a product id.
func NewProductID(value uuid.UUID) (ProductID, error) {
	if value == uuid.Nil {
		return ProductID{}, invalidf("ProductId cannot be empty")
	}
	return ProductID(value), nil
}

// UUID returns the underlying value.
func (id ProductID) UUID() uuid.UUID { return uuid.UUID(id) }

// String renders the id.
func (id ProductID) String() string { return uuid.UUID(id).String() }

// OrderItemID identifies a line within an order.
type OrderItemID uuid.UUID

// NewOrderItemID validates and wraps an order item id.
func NewOrderItemID(value uuid.UUID) (OrderItemID, error) {
	if value == uuid.Nil {
		return OrderItemID{}, invalidf("OrderItemId cannot be empty")
	}
	return OrderItemID(value), nil
}

// UUID returns the underlying value.
func (id OrderItemID) UUID() uuid.UUID { return uuid.UUID(id) }

// String renders the id.
func (id OrderItemID) String() string { return uuid.UUID(id).String() }
