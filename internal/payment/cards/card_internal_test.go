package cards

import (
	"testing"
	"time"
)

func TestTheLuhnCheckAcceptsRealNumbersAndCatchesTypos(t *testing.T) {
	t.Parallel()

	for number, want := range map[string]bool{
		"5555555555554444":     true,  // a published test number
		"4111111111111111":     true,  // another
		"4012888888881881":     true,  // and another
		"5555555555554445":     false, // last digit wrong
		"5555555555545444":     false, // two neighbours swapped
		"55555555":             false, // too short to be a card
		"55555555555544445555": false, // too long
		"5555a55555554444":     false, // not all digits
		"":                     false,
	} {
		if got := validCardNumber(number); got != want {
			t.Errorf("validCardNumber(%q) = %v, want %v", number, got, want)
		}
	}
}

func TestSeparatorsAreNotPartOfTheNumber(t *testing.T) {
	t.Parallel()

	if got := normalizeCardNumber("5555 5555-5555 4444"); got != "5555555555554444" {
		t.Errorf("normalized = %q, want the digits only", got)
	}
}

func TestACardIsValidUntilTheEndOfItsMonth(t *testing.T) {
	t.Parallel()

	end, ok := expiryEnd("12/28")
	if !ok {
		t.Fatal("12/28 should parse")
	}

	lastMoment := time.Date(2028, time.December, 31, 23, 59, 59, 0, time.UTC)
	if !lastMoment.Before(end) {
		t.Errorf("a 12/28 card should still be valid on %s", lastMoment)
	}
	if firstAfter := time.Date(2029, time.January, 1, 0, 0, 0, 0, time.UTC); firstAfter.Before(end) {
		t.Errorf("a 12/28 card should have expired by %s", firstAfter)
	}
}

func TestOnlyMonthSlashYearIsAnExpiry(t *testing.T) {
	t.Parallel()

	for _, expiration := range []string{"13/28", "00/28", "1/28", "12/2028", "1228", "ab/cd", ""} {
		if _, ok := expiryEnd(expiration); ok {
			t.Errorf("%q should not parse as an expiry", expiration)
		}
	}
}

func TestASecurityCodeIsThreeOrFourDigits(t *testing.T) {
	t.Parallel()

	for cvv, want := range map[string]bool{
		"123": true, "1234": true, "12": false, "12345": false, "12a": false, "": false,
	} {
		if got := validCVV(cvv); got != want {
			t.Errorf("validCVV(%q) = %v, want %v", cvv, got, want)
		}
	}
}

func TestTokensSayNothingAboutTheCard(t *testing.T) {
	t.Parallel()

	first, second := newToken(), newToken()

	// pt_ and 32 hex characters: 128 random bits.
	if len(first) != len(tokenPrefix)+32 || first[:len(tokenPrefix)] != tokenPrefix {
		t.Errorf("token = %q, want pt_ followed by 128 bits in hex", first)
	}
	if first == second {
		t.Error("two cards were given the same token")
	}
}
