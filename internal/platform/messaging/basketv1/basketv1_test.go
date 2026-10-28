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

func sampleEvent() *basketv1.CheckoutSubmitted {
	address := basketv1.Address{
		FirstName:    "Mustafa",
		LastName:     "Akcakaya",
		EmailAddress: "mustafa@example.com",
		AddressLine:  "Test Address",
		Country:      "Turkey",
		State:        "Istanbul",
		ZipCode:      "34000",
	}

	return &basketv1.CheckoutSubmitted{
		Header:     messaging.NewHeader("trace-1"),
		UserName:   "swn01",
		CustomerID: uuid.New(),
		TotalPrice: messaging.NewMoney(decimal.RequireFromString("1000.50")),
		Items: []basketv1.Line{
			{
				ProductID:   uuid.New(),
				ProductName: "iPhone X",
				Quantity:    2,
				UnitPrice:   messaging.NewMoney(decimal.RequireFromString("500.25")),
			},
		},
		ShippingAddress: address,
		BillingAddress:  address,
		Payment: basketv1.Payment{
			PaymentToken:  "pt_0123456789abcdef0123456789abcdef",
			CardLast4:     "4444",
			PaymentMethod: 1,
		},
	}
}

func TestTheContractCarriesNoCardData(t *testing.T) {
	t.Parallel()

	// Checked over the type rather than over one encoded message: a field
	// added later would be missed by a test that only inspects a payload.
	// The last four digits are allowed on purpose; the number, its expiry and
	// the security code are not.
	forbidden := []string{"cardnumber", "cardname", "expir", "cvv", "cvc", "securitycode", "iban"}

	var walk func(reflect.Type, string)
	walk = func(structType reflect.Type, path string) {
		for i := range structType.NumField() {
			field := structType.Field(i)
			name := strings.ToLower(field.Name)

			for _, banned := range forbidden {
				if strings.Contains(name, banned) {
					t.Errorf("%s%s looks like card data and must not be on this contract",
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

	walk(reflect.TypeOf(basketv1.CheckoutSubmitted{}), "")
}

func TestThePaymentIsOnlyAReference(t *testing.T) {
	t.Parallel()

	paymentType := reflect.TypeOf(basketv1.Payment{})

	var fields []string
	for i := range paymentType.NumField() {
		fields = append(fields, paymentType.Field(i).Name)
	}

	// A new field here is a decision about what card data may travel, so it
	// has to be made on purpose rather than slipped in.
	want := []string{"PaymentToken", "CardLast4", "PaymentMethod"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("payment fields = %v, want %v", fields, want)
	}
}

func TestThePayloadRoundTripsThroughTheRegistry(t *testing.T) {
	t.Parallel()

	registry := messaging.NewRegistry()
	basketv1.Register(registry)

	event := sampleEvent()
	envelope, err := messaging.NewEnvelope(event, event.UserName)
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}
	if envelope.Contract != (messaging.Contract{EventType: "basket.checkout-submitted", Version: 1}) {
		t.Errorf("contract = %v, want basket.checkout-submitted v1", envelope.Contract)
	}

	decoded, err := registry.Decode(envelope)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	checkout, ok := decoded.(*basketv1.CheckoutSubmitted)
	if !ok {
		t.Fatalf("decoded = %T, want *basketv1.CheckoutSubmitted", decoded)
	}

	if checkout.UserName != event.UserName || len(checkout.Items) != 1 {
		t.Errorf("decoded = %+v, want the event that was written", checkout)
	}
	// The items are what let a consumer build the order from what was actually
	// in the basket.
	if checkout.Items[0].ProductName != "iPhone X" || checkout.Items[0].Quantity != 2 ||
		!checkout.Items[0].UnitPrice.Equal(decimal.RequireFromString("500.25")) {
		t.Errorf("item = %+v, want the basket line", checkout.Items[0])
	}
	if !checkout.TotalPrice.Equal(decimal.RequireFromString("1000.50")) {
		t.Errorf("total = %s, want 1000.50", checkout.TotalPrice)
	}
	if checkout.Payment != event.Payment {
		t.Errorf("payment = %+v, want %+v", checkout.Payment, event.Payment)
	}
	if checkout.ShippingAddress != event.ShippingAddress || checkout.BillingAddress != event.BillingAddress {
		t.Errorf("addresses = %+v / %+v, want the ones written", checkout.ShippingAddress, checkout.BillingAddress)
	}
}

func TestTheWireNamesAreStable(t *testing.T) {
	t.Parallel()

	payload, err := messaging.Marshal(sampleEvent())
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	// Consumers in other services read these names; renaming a Go field must
	// not rename them.
	for _, name := range []string{
		`"userName"`, `"customerId"`, `"totalPrice"`, `"items"`,
		`"productId"`, `"productName"`, `"quantity"`, `"unitPrice"`,
		`"shippingAddress"`, `"billingAddress"`,
		`"payment"`, `"paymentToken"`, `"cardLast4"`, `"paymentMethod"`,
	} {
		if !strings.Contains(string(payload), name) {
			t.Errorf("payload has no %s field: %s", name, payload)
		}
	}
}
