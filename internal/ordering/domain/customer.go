package domain

import (
	"strings"

	"github.com/shopspring/decimal"
)

// Customer is a buyer. Orders reference it, but it is not part of the Order
// aggregate: the two change independently.
type Customer struct {
	Audit

	ID    CustomerID
	Name  string
	Email string
}

// NewCustomer validates and builds a customer.
func NewCustomer(id CustomerID, name, email string) (*Customer, error) {
	if strings.TrimSpace(name) == "" {
		return nil, invalidf("Name is required")
	}
	if strings.TrimSpace(email) == "" {
		return nil, invalidf("Email is required")
	}

	return &Customer{ID: id, Name: name, Email: email}, nil
}

// Product is a catalog entry as Ordering knows it: the service keeps its own
// copy of the name and price so an order stays readable even if the Catalog
// service later changes them.
type Product struct {
	Audit

	ID    ProductID
	Name  string
	Price decimal.Decimal
}

// NewProduct validates and builds a product.
func NewProduct(id ProductID, name string, price decimal.Decimal) (*Product, error) {
	if strings.TrimSpace(name) == "" {
		return nil, invalidf("Name is required")
	}
	if price.LessThanOrEqual(decimal.Zero) {
		return nil, invalidf("Price must be greater than zero")
	}

	return &Product{ID: id, Name: name, Price: price}, nil
}
