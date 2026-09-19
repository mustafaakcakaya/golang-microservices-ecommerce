// Package basketv1 holds version 1 of the contracts the Basket service
// publishes.
//
// As in every contract package, the version is in the package name because a
// contract is never edited in place: a consumer that has not been redeployed
// still reads what it was written against.
package basketv1

import (
	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// Line is one basket line as it leaves the service.
//
// The name and price travel with the line rather than being looked up by the
// consumer. The price is what the customer was shown, discounts already
// applied, and an order has to record what was charged at the time - not what
// the catalog happens to say later.
type Line struct {
	ProductID   uuid.UUID       `json:"productId"`
	ProductName string          `json:"productName"`
	Quantity    int             `json:"quantity"`
	Price       messaging.Money `json:"price"`
}

// CheckoutRequested is published when a customer checks out their basket.
//
// There are no card details here, and that absence is the contract's most
// important property. A card number or security code on a broker is readable by
// every consumer, sits in queues and logs, and outlives the request that
// carried it. The customer is therefore identified, addressed and billed by
// reference; what they pay with is not part of this message, and an order
// created from it starts out unpaid.
type CheckoutRequested struct {
	messaging.Header

	UserName   string    `json:"userName"`
	CustomerID uuid.UUID `json:"customerId"`

	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	EmailAddress string `json:"emailAddress"`
	AddressLine  string `json:"addressLine"`
	Country      string `json:"country"`
	State        string `json:"state"`
	ZipCode      string `json:"zipCode"`

	TotalPrice messaging.Money `json:"totalPrice"`
	Lines      []Line          `json:"lines"`
}

// Contract identifies CheckoutRequested on the wire.
func (CheckoutRequested) Contract() messaging.Contract {
	return messaging.Contract{EventType: "basket.checkout-requested", Version: 1}
}

// Register adds every contract in this package to a registry.
func Register(registry *messaging.Registry) {
	registry.Register(func() messaging.Event { return &CheckoutRequested{} })
}
