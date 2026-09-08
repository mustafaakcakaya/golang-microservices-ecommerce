package messaging

import (
	"encoding/json"

	"github.com/shopspring/decimal"
)

// Money is a decimal amount on the wire.
//
// It exists because decimal.Decimal decides between a JSON number and a quoted
// string through a package-level global, which any package in the process may
// set. A contract whose encoding depends on which binary happens to import
// which package is not a contract, so this type pins the choice: always a
// string, which also spares consumers on platforms whose only number type is a
// float from rounding a price.
//
// Decoding stays lenient and accepts a bare number too, so a payload written
// before this type existed still reads.
type Money struct {
	decimal.Decimal
}

// NewMoney wraps an amount.
func NewMoney(amount decimal.Decimal) Money {
	return Money{Decimal: amount}
}

// MarshalJSON writes the amount as a string.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON reads a string or a number.
//
// The embedded field is named explicitly: m.UnmarshalJSON would resolve to
// this method rather than the one being delegated to, and recurse forever.
func (m *Money) UnmarshalJSON(data []byte) error {
	return m.Decimal.UnmarshalJSON(data)
}
