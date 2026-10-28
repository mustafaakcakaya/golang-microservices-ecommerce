package orders

import (
	"errors"

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

// PaymentInput says how an incoming order is paid for.
//
// It carries a reference, not a card: the card goes to the Payment service,
// which keeps it and issues the token. Ordering never sees a card number or a
// security code, so it has none to store, return or leak.
type PaymentInput struct {
	PaymentToken  string `json:"paymentToken" validate:"required"`
	CardLast4     string `json:"cardLast4" validate:"required,len=4,numeric"`
	PaymentMethod int    `json:"paymentMethod"`
}

// toDomain builds the value object.
func (p PaymentInput) toDomain() (domain.Payment, error) {
	return domain.NewPayment(p.PaymentToken, p.CardLast4, p.PaymentMethod)
}

// String leaves the token out, so formatting a command cannot leak it.
func (p PaymentInput) String() string {
	return "PaymentInput{cardLast4: " + p.CardLast4 + ", token: [redacted]}"
}

// MarshalJSON refuses to serialize the payment.
//
// It is an inbound-only shape. Failing loudly beats emitting a redacted
// object: a command reaching a JSON encoder means something is about to send
// it somewhere it does not belong, and a body that looks complete but silently
// dropped the token would hide that.
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
