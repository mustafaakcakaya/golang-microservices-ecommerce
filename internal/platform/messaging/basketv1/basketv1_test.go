package basketv1_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
)

func sampleEvent() *basketv1.CheckoutRequested {
	return &basketv1.CheckoutRequested{
		Header:       messaging.NewHeader("trace-1"),
		UserName:     "swn01",
		CustomerID:   uuid.New(),
		FirstName:    "Mustafa",
		LastName:     "Akcakaya",
		EmailAddress: "mustafa@example.com",
		AddressLine:  "Test Address",
		Country:      "Turkey",
		State:        "Istanbul",
		ZipCode:      "34000",
		TotalPrice:   messaging.NewMoney(decimal.RequireFromString("1000.50")),
		Lines: []basketv1.Line{
			{
				ProductID:   uuid.New(),
				ProductName: "iPhone X",
				Quantity:    2,
				Price:       messaging.NewMoney(decimal.RequireFromString("500.25")),
			},
		},
	}
}

func TestTheContractCarriesNoPaymentDetails(t *testing.T) {
	t.Parallel()

	// Checked over the type rather than over one encoded message: a field
	// added later would be missed by a test that only inspects a payload.
	forbidden := []string{"card", "cvv", "cvc", "securitycode", "pan", "iban", "pin"}

	var walk func(reflect.Type, string)
	walk = func(structType reflect.Type, path string) {
		for i := range structType.NumField() {
			field := structType.Field(i)
			name := strings.ToLower(field.Name)

			for _, banned := range forbidden {
				if strings.Contains(name, banned) {
					t.Errorf("%s%s looks like payment data and must not be on this contract",
						path, field.Name)
				}
			}

			fieldType := field.Type
			for fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			// decimal and uuid are structs too; only walk our own contracts.
			if fieldType.Kind() == reflect.Struct && strings.Contains(fieldType.PkgPath(), "messaging/basketv1") {
				walk(fieldType, path+field.Name+".")
			}
		}
	}

	walk(reflect.TypeOf(basketv1.CheckoutRequested{}), "")
}

func TestThePayloadRoundTripsThroughTheRegistry(t *testing.T) {
	t.Parallel()

	registry := messaging.NewRegistry()
	basketv1.Register(registry)

	event := sampleEvent()
	envelope, err := messaging.NewEnvelope(event, event.CustomerID.String())
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}
	if envelope.Contract != (basketv1.CheckoutRequested{}).Contract() {
		t.Errorf("contract = %v, want basket.checkout-requested v1", envelope.Contract)
	}

	decoded, err := registry.Decode(envelope)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	checkout, ok := decoded.(*basketv1.CheckoutRequested)
	if !ok {
		t.Fatalf("decoded = %T, want *basketv1.CheckoutRequested", decoded)
	}

	if checkout.UserName != event.UserName || len(checkout.Lines) != 1 {
		t.Errorf("decoded = %+v, want the event that was written", checkout)
	}
	// The lines are what let a consumer build the order from what was actually
	// in the basket.
	if checkout.Lines[0].ProductName != "iPhone X" || checkout.Lines[0].Quantity != 2 {
		t.Errorf("line = %+v, want the basket line", checkout.Lines[0])
	}
	if !checkout.TotalPrice.Equal(decimal.RequireFromString("1000.50")) {
		t.Errorf("total = %s, want 1000.50", checkout.TotalPrice)
	}
}
