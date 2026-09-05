package domain_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
)

const (
	testCardNumber = "5555555555554444"
	testCVV        = "123"
)

func newPayment(t *testing.T) domain.Payment {
	t.Helper()

	payment, err := domain.NewPayment("Mustafa Akcakaya", testCardNumber, "12/28", testCVV, 1)
	if err != nil {
		t.Fatalf("building payment: %v", err)
	}
	return payment
}

func TestPaymentRequiresItsFields(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		cardName, cardNumber, cvv string
	}{
		"missing card name":   {"", testCardNumber, testCVV},
		"blank card name":     {"   ", testCardNumber, testCVV},
		"missing card number": {"Mustafa", "", testCVV},
		"missing cvv":         {"Mustafa", testCardNumber, ""},
		"cvv too long":        {"Mustafa", testCardNumber, "12345"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := domain.NewPayment(tc.cardName, tc.cardNumber, "12/28", tc.cvv, 1); err == nil {
				t.Error("expected a rule violation")
			}
		})
	}
}

func TestFormattingAPaymentDoesNotRevealTheCard(t *testing.T) {
	t.Parallel()

	payment := newPayment(t)

	// Each of these is a plausible way a card number reaches a log line. %+v is
	// the one worth pinning: it expands structs field by field unless the type
	// says otherwise.
	rendered := []string{
		payment.String(),
		fmt.Sprintf("%v", payment),
		fmt.Sprintf("%+v", payment),
	}

	for _, text := range rendered {
		if strings.Contains(text, testCardNumber) {
			t.Errorf("rendered payment leaks the card number: %s", text)
		}
		if strings.Contains(text, testCVV) {
			t.Errorf("rendered payment leaks the CVV: %s", text)
		}
	}
}

func TestFormattingAnOrderDoesNotRevealTheCard(t *testing.T) {
	t.Parallel()

	order := newOrder(t)

	// %+v on an aggregate is the accident this guards against: it walks into
	// every field, including the payment.
	text := fmt.Sprintf("%+v", order)

	if strings.Contains(text, testCardNumber) || strings.Contains(text, testCVV) {
		t.Errorf("formatted order leaks payment details: %s", text)
	}
}

func TestMarshallingAPaymentFailsRatherThanLeaking(t *testing.T) {
	t.Parallel()

	payment := newPayment(t)

	encoded, err := json.Marshal(payment)

	// Failing loudly beats emitting a body that looks complete but quietly
	// dropped the fields - the caller would not notice either way.
	if err == nil {
		t.Fatalf("marshalling should fail, got %s", encoded)
	}
	if strings.Contains(string(encoded), testCardNumber) {
		t.Errorf("encoded payment leaks the card number: %s", encoded)
	}
}

func TestMaskedCardNumberKeepsOnlyTheLastFour(t *testing.T) {
	t.Parallel()

	payment := newPayment(t)

	masked := payment.MaskedCardNumber()

	if masked != "************4444" {
		t.Errorf("masked = %q, want the last four digits only", masked)
	}
}

func TestAccessorsStillExposeTheValuesForPersistence(t *testing.T) {
	t.Parallel()

	payment := newPayment(t)

	// Redaction must not make the data unreachable: storage still needs it.
	if payment.CardNumber() != testCardNumber || payment.CVV() != testCVV {
		t.Error("accessors should return the stored values")
	}
}
