package orders

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
)

// errPaymentSerialized is returned rather than a redacted object; see
// PaymentInput.MarshalJSON.
var errPaymentSerialized = errors.New("payment details must not be serialized; map them to an explicit contract instead")

// AddressInput is a postal address as a command carries it.
type AddressInput struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	EmailAddress string `json:"emailAddress" validate:"required,email"`
	AddressLine  string `json:"addressLine" validate:"required"`
	Country      string `json:"country"`
	State        string `json:"state"`
	ZipCode      string `json:"zipCode"`
}

// toDomain builds the value object, so the rules that guard an address live in
// the domain rather than being restated per command.
func (a AddressInput) toDomain() (domain.Address, error) {
	return domain.NewAddress(
		a.FirstName, a.LastName, a.EmailAddress,
		a.AddressLine, a.Country, a.State, a.ZipCode,
	)
}

// PaymentInput carries the card details of an incoming order.
//
// It is an inbound-only shape: it is decoded from a request body and turned
// into a domain.Payment, and it must never travel back out. Both the fmt and
// the encoding/json representations refuse to render the card details, which is
// what keeps a %v of a command out of the logs and a stray marshal out of a
// response body - the same protection domain.Payment gives the aggregate.
type PaymentInput struct {
	CardName      string `json:"cardName" validate:"required"`
	CardNumber    string `json:"cardNumber" validate:"required,min=12,max=19"`
	Expiration    string `json:"expiration" validate:"required"`
	CVV           string `json:"cvv" validate:"required,max=3"`
	PaymentMethod int    `json:"paymentMethod"`
}

// toDomain builds the value object.
func (p PaymentInput) toDomain() (domain.Payment, error) {
	return domain.NewPayment(p.CardName, p.CardNumber, p.Expiration, p.CVV, p.PaymentMethod)
}

// String redacts the card details so formatting a command cannot leak them.
func (p PaymentInput) String() string {
	const visible = 4

	masked := strings.Repeat("*", len(p.CardNumber))
	if len(p.CardNumber) > visible {
		masked = strings.Repeat("*", len(p.CardNumber)-visible) + p.CardNumber[len(p.CardNumber)-visible:]
	}

	return "PaymentInput{cardNumber: " + masked + ", cvv: [redacted]}"
}

// MarshalJSON refuses to serialize card details.
//
// Failing loudly beats emitting a redacted object: a command reaching a JSON
// encoder means something is about to send it somewhere it does not belong, and
// a body that looks complete but silently dropped the fields would hide that.
func (p PaymentInput) MarshalJSON() ([]byte, error) {
	return nil, errPaymentSerialized
}

// LineInput is one requested order line.
//
// The price travels with the line rather than being looked up: an order records
// what was charged at the time, which must not change when the catalog price
// later does.
type LineInput struct {
	ProductID uuid.UUID       `json:"productId" validate:"required"`
	Quantity  int             `json:"quantity" validate:"gt=0"`
	Price     decimal.Decimal `json:"price" validate:"gt=0"`
}
