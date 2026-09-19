package cqrs_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
)

// placeOrder stands in for a command carrying payment details, which is exactly
// the kind of request the logging middleware must never write out.
type placeOrder struct {
	CardNumber string
	CVV        string
}

const (
	cardNumber = "5555555555554444"
	// Letters outside the hex range, so the value cannot occur by chance in a
	// UUID, a timestamp or a duration. Three digits such as "355" do, and a
	// leak check searching for them fails for the wrong reason a few percent
	// of the time. The domain accepts any three characters.
	cvv = "zqx"
)

func TestLoggingNeverWritesTheRequest(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	// Debug level, so every line the middleware can emit is captured.
	log := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handle := cqrs.Chain[placeOrder, struct{}](
		cqrs.HandlerFunc[placeOrder, struct{}](func(context.Context, placeOrder) (struct{}, error) {
			return struct{}{}, nil
		}),
		cqrs.Logging[placeOrder, struct{}](log, "PlaceOrder"),
	)

	if _, err := handle(t.Context(), placeOrder{CardNumber: cardNumber, CVV: cvv}); err != nil {
		t.Fatalf("handling: %v", err)
	}

	assertNoPaymentDetails(t, output.String())

	// The operation is still identifiable, so the logs stay useful without the
	// request in them.
	if !strings.Contains(output.String(), "PlaceOrder") {
		t.Errorf("logs = %s, want the operation name", output.String())
	}
}

func TestLoggingAFailureDoesNotWriteTheRequestEither(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handle := cqrs.Chain[placeOrder, struct{}](
		cqrs.HandlerFunc[placeOrder, struct{}](func(context.Context, placeOrder) (struct{}, error) {
			return struct{}{}, errors.New("the database is unreachable")
		}),
		cqrs.Logging[placeOrder, struct{}](log, "PlaceOrder"),
	)

	// The failure path logs at error level, which is the one most likely to be
	// shipped somewhere and kept.
	if _, err := handle(t.Context(), placeOrder{CardNumber: cardNumber, CVV: cvv}); err == nil {
		t.Fatal("the handler's failure should surface")
	}

	assertNoPaymentDetails(t, output.String())
}

func assertNoPaymentDetails(t *testing.T, logs string) {
	t.Helper()

	if strings.Contains(logs, cardNumber) {
		t.Errorf("the card number reached the logs: %s", logs)
	}
	if strings.Contains(logs, cvv) {
		t.Errorf("the CVV reached the logs: %s", logs)
	}
}
