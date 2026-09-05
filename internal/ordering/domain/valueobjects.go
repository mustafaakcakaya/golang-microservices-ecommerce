package domain

import "strings"

// orderNameLength is the exact length an order name must have.
const orderNameLength = 5

// OrderName is the human-readable reference of an order (for example ORD_1).
type OrderName struct {
	value string
}

// NewOrderName validates and wraps an order name.
func NewOrderName(value string) (OrderName, error) {
	if strings.TrimSpace(value) == "" {
		return OrderName{}, invalidf("OrderName is required")
	}
	if len(value) != orderNameLength {
		return OrderName{}, invalidf("OrderName must be exactly %d characters", orderNameLength)
	}

	return OrderName{value: value}, nil
}

// String returns the name.
func (n OrderName) String() string { return n.value }

// Address is a postal address attached to an order.
type Address struct {
	FirstName    string
	LastName     string
	EmailAddress string
	AddressLine  string
	Country      string
	State        string
	ZipCode      string
}

// NewAddress validates and builds an address.
func NewAddress(firstName, lastName, emailAddress, addressLine, country, state, zipCode string) (Address, error) {
	if strings.TrimSpace(emailAddress) == "" {
		return Address{}, invalidf("EmailAddress is required")
	}
	if strings.TrimSpace(addressLine) == "" {
		return Address{}, invalidf("AddressLine is required")
	}

	return Address{
		FirstName:    firstName,
		LastName:     lastName,
		EmailAddress: emailAddress,
		AddressLine:  addressLine,
		Country:      country,
		State:        state,
		ZipCode:      zipCode,
	}, nil
}
