package domain

import (
	"encoding/json"
	"strings"
)

// cardLast4Length is how many digits of the card an order keeps.
const cardLast4Length = 4

// Payment is how an order is paid for, by reference.
//
// The card itself lives in the Payment service. An order keeps the token that
// service issued for it, the last four digits so a customer can tell which card
// they used, and the payment method. Nothing here can be charged on its own.
//
// The token is still kept out of logs and responses: it is the handle a future
// charge would be made with, so it is treated like a credential rather than a
// label. The fmt and encoding/json representations both leave it out.
type Payment struct {
	paymentToken  string
	cardLast4     string
	paymentMethod int
}

// NewPayment validates and builds a payment.
func NewPayment(paymentToken, cardLast4 string, paymentMethod int) (Payment, error) {
	if strings.TrimSpace(paymentToken) == "" {
		return Payment{}, invalidf("PaymentToken is required")
	}
	if !isDigits(cardLast4, cardLast4Length) {
		return Payment{}, invalidf("CardLast4 must be the last %d digits of the card", cardLast4Length)
	}

	return Payment{
		paymentToken:  paymentToken,
		cardLast4:     cardLast4,
		paymentMethod: paymentMethod,
	}, nil
}

// isDigits reports whether value is exactly length ASCII digits.
func isDigits(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// IsZero reports whether the order has no payment recorded.
//
// Orders placed before checkouts carried a payment token were stored without
// one, so the absence of a payment is a state the domain has to be able to
// express rather than a broken value.
func (p Payment) IsZero() bool {
	return p.paymentToken == ""
}

// PaymentToken returns the token. Only persistence should call it.
func (p Payment) PaymentToken() string { return p.paymentToken }

// CardLast4 returns the last four digits of the card.
func (p Payment) CardLast4() string { return p.cardLast4 }

// PaymentMethod returns the method code.
func (p Payment) PaymentMethod() int { return p.paymentMethod }

// String leaves the token out so formatting a Payment cannot leak it.
func (p Payment) String() string {
	if p.IsZero() {
		return "Payment{none}"
	}

	return "Payment{cardLast4: " + p.cardLast4 + ", token: [redacted]}"
}

// MarshalJSON refuses to serialize the payment.
//
// Returning an error rather than a redacted object is deliberate: a Payment
// reaching a JSON encoder means something is about to send it somewhere it does
// not belong, and failing loudly is better than emitting a body that looks
// complete but silently dropped the fields.
func (p Payment) MarshalJSON() ([]byte, error) {
	return nil, &Error{Message: "payment details must not be serialized; map them to an explicit contract instead"}
}

// compile-time checks that the redacting behaviour is actually wired up.
var (
	_ json.Marshaler = Payment{}
)
