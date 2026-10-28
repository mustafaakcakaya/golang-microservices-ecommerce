package domain

import "strings"

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
