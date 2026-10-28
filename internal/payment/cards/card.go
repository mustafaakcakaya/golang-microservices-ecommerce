// Package cards holds the Payment service's model, rules, handlers and routes.
//
// This is the one service that receives a card. It keeps the number, the
// expiry and the name on the card, and hands back a token that refers to them.
// Every other service works with the token: a checkout, an order and every
// message between them carry the token and the last four digits, never the
// card itself.
//
// The security code is the exception even here. It is checked for shape and
// then dropped: it is never stored, logged or returned.
package cards

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// StoredCard is a card as this service keeps it. There is no security code
// field, so there is nothing that could put one in the database.
type StoredCard struct {
	Token      string
	CardNumber string
	Expiration string
	CardName   string
}

// Last4 returns the digits other services may show.
func (c StoredCard) Last4() string {
	return c.CardNumber[len(c.CardNumber)-4:]
}

// String leaves the card number out, so formatting a StoredCard cannot leak it.
func (c StoredCard) String() string {
	return "StoredCard{token: " + c.Token + ", last4: " + c.Last4() + "}"
}

// Repository keeps cards.
type Repository interface {
	// Store saves a card under its token.
	Store(ctx context.Context, card StoredCard) error
}

// tokenPrefix marks a value as a payment token wherever it ends up, so one is
// recognisable in a message or an order without knowing where it came from.
const tokenPrefix = "pt_"

// newToken makes a token that says nothing about the card it refers to.
//
// 128 random bits: nothing derived from the card, so a token cannot be turned
// back into a number, and too many possibilities to guess one that exists.
func newToken() string {
	random := make([]byte, 16)
	// crypto/rand fills the slice completely and never fails.
	rand.Read(random)

	return tokenPrefix + hex.EncodeToString(random)
}

// normalizeCardNumber removes the separators people type between digit groups.
func normalizeCardNumber(number string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(number)
}

// The lengths a card number can have. Shorter ones are not card numbers; the
// longest issued today have nineteen digits.
const (
	minCardDigits = 12
	maxCardDigits = 19
)

// validCardNumber reports whether number is a plausible card number: the right
// number of digits, with a Luhn check digit that matches.
//
// The Luhn check catches a mistyped digit or two swapped neighbours, which is
// what it was designed for. It says nothing about whether the card exists.
func validCardNumber(number string) bool {
	if len(number) < minCardDigits || len(number) > maxCardDigits || !allDigits(number) {
		return false
	}

	sum := 0
	double := false
	for i := len(number) - 1; i >= 0; i-- {
		digit := int(number[i] - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}

	return sum%10 == 0
}

// validCVV reports whether a security code has the shape one has: three
// digits, or four on some cards.
func validCVV(cvv string) bool {
	return (len(cvv) == 3 || len(cvv) == 4) && allDigits(cvv)
}

// expiryEnd parses MM/YY and returns the first moment after the card expires.
//
// A card printed with 12/28 can be used throughout December 2028, so it stops
// being valid when January 2029 begins.
func expiryEnd(expiration string) (time.Time, bool) {
	month, year, found := strings.Cut(expiration, "/")
	if !found || len(month) != 2 || len(year) != 2 || !allDigits(month) || !allDigits(year) {
		return time.Time{}, false
	}

	m, _ := strconv.Atoi(month)
	y, _ := strconv.Atoi(year)
	if m < 1 || m > 12 {
		return time.Time{}, false
	}

	return time.Date(2000+y, time.Month(m)+1, 1, 0, 0, 0, 0, time.UTC), true
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
