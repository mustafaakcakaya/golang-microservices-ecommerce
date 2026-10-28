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
	UnitPrice   messaging.Money `json:"unitPrice"`
}

// Address is a postal address on a checkout.
type Address struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	EmailAddress string `json:"emailAddress"`
	AddressLine  string `json:"addressLine"`
	Country      string `json:"country"`
	State        string `json:"state"`
	ZipCode      string `json:"zipCode"`
}

// Payment says how a checkout is paid for, by reference.
//
// The card itself stays in the Payment service. What travels is the token it
// issued for the card and the last four digits, which are for showing the
// customer which card they used and nothing else.
type Payment struct {
	PaymentToken  string `json:"paymentToken"`
	CardLast4     string `json:"cardLast4"`
	PaymentMethod int    `json:"paymentMethod"`
}

// CheckoutSubmitted is published when a customer checks out their basket.
//
// The items are the basket exactly as it was checked out, so the order is built
// from what the customer bought. The total is their sum; a consumer recomputes
// it and refuses a message where the two disagree, since that means the
// services no longer agree about what was ordered.
//
// There is no card number, expiry or security code here, and that absence is
// the contract's most important property. Card data on a broker is readable by
// every consumer, sits in queues and logs, and outlives the request that
// carried it.
type CheckoutSubmitted struct {
	messaging.Header

	UserName   string    `json:"userName"`
	CustomerID uuid.UUID `json:"customerId"`

	TotalPrice messaging.Money `json:"totalPrice"`
	Items      []Line          `json:"items"`

	ShippingAddress Address `json:"shippingAddress"`
	BillingAddress  Address `json:"billingAddress"`

	Payment Payment `json:"payment"`
}

// Contract identifies CheckoutSubmitted on the wire.
func (CheckoutSubmitted) Contract() messaging.Contract {
	return messaging.Contract{EventType: "basket.checkout-submitted", Version: 1}
}

// Register adds every contract in this package to a registry.
func Register(registry *messaging.Registry) {
	registry.Register(func() messaging.Event { return &CheckoutSubmitted{} })
}
