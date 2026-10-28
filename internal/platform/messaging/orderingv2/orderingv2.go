// Package orderingv2 holds version 2 of the contracts the Ordering service
// publishes.
//
// The version is in the package name because a contract is never edited in
// place: a consumer that has not been redeployed still reads what it was
// written against, so a change that would break it becomes a new package
// rather than an edit to this one.
//
// Version 2 dropped the order name, since an order is identified by its id
// alone. Version 1 was deleted rather than kept alongside: nothing consumed it.
//
// What is missing from these types matters as much as what is present. An order
// carries addresses and card details; none of them are here. A consumer that
// needs the total and the lines gets the total and the lines.
package orderingv2

import (
	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// Line is one line of an order as it leaves the service.
type Line struct {
	ProductID uuid.UUID       `json:"productId"`
	Quantity  int             `json:"quantity"`
	Price     messaging.Money `json:"price"`
}

// OrderCreated is published once an order has been stored.
type OrderCreated struct {
	messaging.Header

	OrderID    uuid.UUID       `json:"orderId"`
	CustomerID uuid.UUID       `json:"customerId"`
	Status     string          `json:"status"`
	TotalPrice messaging.Money `json:"totalPrice"`
	Items      []Line          `json:"items"`
}

// Contract identifies OrderCreated on the wire.
func (OrderCreated) Contract() messaging.Contract {
	return messaging.Contract{EventType: "ordering.order-created", Version: 2}
}

// OrderUpdated is published once an existing order has changed.
//
// It carries no lines: an update cannot change them, so repeating them would
// suggest a consumer could learn about a line change from this event.
type OrderUpdated struct {
	messaging.Header

	OrderID    uuid.UUID       `json:"orderId"`
	CustomerID uuid.UUID       `json:"customerId"`
	Status     string          `json:"status"`
	TotalPrice messaging.Money `json:"totalPrice"`
}

// Contract identifies OrderUpdated on the wire.
func (OrderUpdated) Contract() messaging.Contract {
	return messaging.Contract{EventType: "ordering.order-updated", Version: 2}
}

// Register adds every contract in this package to a registry.
func Register(registry *messaging.Registry) {
	registry.Register(
		func() messaging.Event { return &OrderCreated{} },
		func() messaging.Event { return &OrderUpdated{} },
	)
}
