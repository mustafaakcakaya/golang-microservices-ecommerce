package domain

import (
	"encoding/json"
	"strings"
)

// maxCVVLength bounds the security code.
const maxCVVLength = 3

// Payment holds the card details an order was paid with.
//
// The fields are unexported and reachable only through accessors, and both the
// fmt and encoding/json representations are redacted. Card numbers and security
// codes must never reach a log line, an error message or an outbound message;
// making the type refuse to render them means that cannot happen by accident -
// a %v of an Order, or marshalling one into a response, stays safe.
type Payment struct {
	cardName      string
	cardNumber    string
	expiration    string
	cvv           string
	paymentMethod int
}

// NewPayment validates and builds a payment.
func NewPayment(cardName, cardNumber, expiration, cvv string, paymentMethod int) (Payment, error) {
	if strings.TrimSpace(cardName) == "" {
		return Payment{}, invalidf("CardName is required")
	}
	if strings.TrimSpace(cardNumber) == "" {
		return Payment{}, invalidf("CardNumber is required")
	}
	if strings.TrimSpace(cvv) == "" {
		return Payment{}, invalidf("CVV is required")
	}
	if len(cvv) > maxCVVLength {
		return Payment{}, invalidf("CVV must be at most %d characters", maxCVVLength)
	}

	return Payment{
		cardName:      cardName,
		cardNumber:    cardNumber,
		expiration:    expiration,
		cvv:           cvv,
		paymentMethod: paymentMethod,
	}, nil
}

// CardName returns the name on the card.
func (p Payment) CardName() string { return p.cardName }

// CardNumber returns the full card number. Only persistence should call it.
func (p Payment) CardNumber() string { return p.cardNumber }

// Expiration returns the expiry as stored.
func (p Payment) Expiration() string { return p.expiration }

// CVV returns the security code. Only persistence should call it.
func (p Payment) CVV() string { return p.cvv }

// PaymentMethod returns the method code.
func (p Payment) PaymentMethod() int { return p.paymentMethod }

// MaskedCardNumber shows only the last four digits, for the rare case where a
// human needs to recognise which card was used.
func (p Payment) MaskedCardNumber() string {
	const visible = 4

	if len(p.cardNumber) <= visible {
		return strings.Repeat("*", len(p.cardNumber))
	}

	return strings.Repeat("*", len(p.cardNumber)-visible) + p.cardNumber[len(p.cardNumber)-visible:]
}

// String redacts the card details so formatting a Payment cannot leak them.
func (p Payment) String() string {
	return "Payment{cardNumber: " + p.MaskedCardNumber() + ", cvv: [redacted]}"
}

// MarshalJSON refuses to serialize card details.
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
