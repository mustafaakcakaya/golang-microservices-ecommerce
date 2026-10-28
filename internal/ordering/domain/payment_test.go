package domain_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
)

const (
	// Letters no UUID or digit string can contain, so a test that looks for
	// the token in rendered text cannot match it by chance.
	testToken = "pt_zqxzqxzqxzqx"
	testLast4 = "4444"
)

func newPayment(t *testing.T) domain.Payment {
	t.Helper()

	payment, err := domain.NewPayment(testToken, testLast4, 1)
	if err != nil {
		t.Fatalf("building payment: %v", err)
	}
	return payment
}

func TestPaymentRequiresItsFields(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		token, last4 string
	}{
		"missing token":      {"", testLast4},
		"blank token":        {"   ", testLast4},
		"missing last four":  {testToken, ""},
		"three digits":       {testToken, "444"},
		"a whole card":       {testToken, "5555555555554444"},
		"letters for digits": {testToken, "44a4"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := domain.NewPayment(tc.token, tc.last4, 1); err == nil {
				t.Error("expected a rule violation")
			}
		})
	}
}

func TestFormattingAPaymentDoesNotRevealTheToken(t *testing.T) {
	t.Parallel()

	payment := newPayment(t)

	// Each of these is a plausible way a token reaches a log line. %+v is the
	// one worth pinning: it expands structs field by field unless the type
	// says otherwise.
	for _, text := range []string{
		payment.String(),
		fmt.Sprintf("%v", payment),
		fmt.Sprintf("%+v", payment),
	} {
		if strings.Contains(text, testToken) {
			t.Errorf("rendered payment leaks the token: %s", text)
		}
	}
}

func TestFormattingAnOrderDoesNotRevealTheToken(t *testing.T) {
	t.Parallel()

	order := newOrder(t)

	// %+v on an aggregate is the accident this guards against: it walks into
	// every field, including the payment.
	if text := fmt.Sprintf("%+v", order); strings.Contains(text, testToken) {
		t.Errorf("formatted order leaks the payment token: %s", text)
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
	if strings.Contains(string(encoded), testToken) {
		t.Errorf("encoded payment leaks the token: %s", encoded)
	}
}

func TestAccessorsStillExposeTheValuesForPersistence(t *testing.T) {
	t.Parallel()

	payment := newPayment(t)

	// Redaction must not make the data unreachable: storage still needs it.
	if payment.PaymentToken() != testToken || payment.CardLast4() != testLast4 || payment.PaymentMethod() != 1 {
		t.Error("accessors should return the stored values")
	}
}
