package orders_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/orders"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/inbox"
)

// fakeInbox is the consumer-side deduplication store.
type fakeInbox struct {
	done map[uuid.UUID]bool
}

func (i *fakeInbox) Begin(_ context.Context, messageID uuid.UUID, _ string) (bool, error) {
	return !i.done[messageID], nil
}

func (i *fakeInbox) Complete(_ context.Context, messageID uuid.UUID, _ string) error {
	i.done[messageID] = true
	return nil
}

func checkoutEvent(customerID uuid.UUID) *basketv1.CheckoutSubmitted {
	shipping := basketv1.Address{
		FirstName:    "Mustafa",
		LastName:     "Akcakaya",
		EmailAddress: "mustafa@example.com",
		AddressLine:  "Test Address",
		Country:      "Turkey",
		State:        "Istanbul",
		ZipCode:      "34000",
	}
	billing := shipping
	billing.AddressLine = "Billing Address"

	return &basketv1.CheckoutSubmitted{
		Header:     messaging.NewHeader("trace-1"),
		UserName:   "swn01",
		CustomerID: customerID,
		TotalPrice: messaging.NewMoney(decimal.NewFromInt(1400)),
		Items: []basketv1.Line{
			{
				ProductID:   uuid.New(),
				ProductName: "iPhone X",
				Quantity:    2,
				UnitPrice:   messaging.NewMoney(decimal.NewFromInt(500)),
			},
			{
				ProductID:   uuid.New(),
				ProductName: "Samsung 10",
				Quantity:    1,
				UnitPrice:   messaging.NewMoney(decimal.NewFromInt(400)),
			},
		},
		ShippingAddress: shipping,
		BillingAddress:  billing,
		Payment: basketv1.Payment{
			PaymentToken:  paymentToken,
			CardLast4:     "4444",
			PaymentMethod: 1,
		},
	}
}

func envelopeOf(t *testing.T, event *basketv1.CheckoutSubmitted) messaging.Envelope {
	t.Helper()

	envelope, err := messaging.NewEnvelope(event, event.UserName)
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}

	return envelope
}

func checkoutEnvelope(t *testing.T, customerID uuid.UUID) messaging.Envelope {
	t.Helper()

	return envelopeOf(t, checkoutEvent(customerID))
}

func TestACheckoutBecomesAnOrderOfItsOwnItems(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCheckoutHandler(repo)

	customerID := uuid.New()
	if err := handler.Handle(t.Context(), checkoutEnvelope(t, customerID)); err != nil {
		t.Fatalf("handling: %v", err)
	}

	if len(repo.saved) != 1 {
		t.Fatalf("saved %d orders, want one", len(repo.saved))
	}
	order := repo.saved[0]

	// Built from what was in the basket, not from a fixed list: the customer
	// gets what they actually bought, at the prices they were shown.
	items := order.Items()
	if len(items) != 2 {
		t.Fatalf("items = %+v, want both checkout items", items)
	}
	if !order.TotalPrice().Equal(decimal.NewFromInt(1400)) {
		t.Errorf("total = %s, want 1400", order.TotalPrice())
	}

	// The checkout carries a payment token, so the order is paid by reference
	// and pending rather than a draft.
	payment := order.Payment()
	if payment.PaymentToken() != paymentToken || payment.CardLast4() != "4444" || payment.PaymentMethod() != 1 {
		t.Errorf("payment = %v, want the checkout's token, last four and method", payment)
	}
	if order.Status() != domain.OrderStatusPending {
		t.Errorf("status = %v, want Pending", order.Status())
	}

	if order.ShippingAddress().AddressLine != "Test Address" || order.BillingAddress().AddressLine != "Billing Address" {
		t.Errorf("addresses = %+v / %+v, want the checkout's shipping and billing addresses",
			order.ShippingAddress(), order.BillingAddress())
	}

	// The domain fixes the length, so the user name cannot be the order name.
	if got := order.OrderName().String(); len(got) != 5 {
		t.Errorf("order name = %q, want five characters", got)
	}
}

func TestTheOrderIsPlacedWithTheProductsAndCustomerItRefersTo(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCheckoutHandler(repo)

	customerID := uuid.New()
	if err := handler.Handle(t.Context(), checkoutEnvelope(t, customerID)); err != nil {
		t.Fatalf("handling: %v", err)
	}

	// Handed over together, so they are written in the order's transaction:
	// the order's foreign keys need them, and a crash between separate writes
	// would leave one without the other.
	if len(repo.products) != 2 {
		t.Fatalf("placed with %d products, want both", len(repo.products))
	}
	known := map[domain.ProductID]*domain.Product{}
	for _, product := range repo.products {
		known[product.ID] = product
	}
	for _, item := range repo.saved[0].Items() {
		product, ok := known[item.ProductID]
		if !ok {
			t.Errorf("product %s was ordered but not handed over", item.ProductID)
			continue
		}
		// What the checkout knows about the product, for the case where this
		// service has never seen it.
		if !product.Price.Equal(item.Price) || product.Name == "" {
			t.Errorf("product %s = %q at %s, want the checkout's name and price %s",
				item.ProductID, product.Name, product.Price, item.Price)
		}
	}

	if len(repo.customers) != 1 || repo.customers[0].ID.UUID() != customerID ||
		repo.customers[0].Name != "Mustafa Akcakaya" {
		t.Errorf("customers = %+v, want the buyer", repo.customers)
	}
}

func TestATotalThatDoesNotMatchItsItemsIsRejected(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCheckoutHandler(repo)

	event := checkoutEvent(uuid.New())
	event.TotalPrice = messaging.NewMoney(decimal.NewFromInt(1300))

	err := handler.Handle(t.Context(), envelopeOf(t, event))

	// Rejected rather than retried: no redelivery will make the numbers agree,
	// and a message retried forever holds up every checkout behind it.
	if !messaging.IsRejected(err) {
		t.Fatalf("error = %v, want a rejection", err)
	}
	if !strings.Contains(err.Error(), "1300") || !strings.Contains(err.Error(), "1400") {
		t.Errorf("error = %v, want both totals named", err)
	}
	if len(repo.saved) != 0 {
		t.Error("an order was stored from a checkout whose total is wrong")
	}
}

func TestACheckoutThatContradictsItselfIsRejected(t *testing.T) {
	t.Parallel()

	for name, spoil := range map[string]func(*basketv1.CheckoutSubmitted){
		"no items":          func(e *basketv1.CheckoutSubmitted) { e.Items = nil; e.TotalPrice = messaging.NewMoney(decimal.Zero) },
		"no payment token":  func(e *basketv1.CheckoutSubmitted) { e.Payment.PaymentToken = "" },
		"not four digits":   func(e *basketv1.CheckoutSubmitted) { e.Payment.CardLast4 = "5555555555554444" },
		"no address line":   func(e *basketv1.CheckoutSubmitted) { e.ShippingAddress.AddressLine = "" },
		"no customer":       func(e *basketv1.CheckoutSubmitted) { e.CustomerID = uuid.Nil },
		"no product id":     func(e *basketv1.CheckoutSubmitted) { e.Items[0].ProductID = uuid.Nil },
		"no product name":   func(e *basketv1.CheckoutSubmitted) { e.Items[0].ProductName = "" },
		"no billing e-mail": func(e *basketv1.CheckoutSubmitted) { e.BillingAddress.EmailAddress = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepository()
			handler := orders.NewCheckoutHandler(repo)

			event := checkoutEvent(uuid.New())
			spoil(event)

			if err := handler.Handle(t.Context(), envelopeOf(t, event)); !messaging.IsRejected(err) {
				t.Fatalf("error = %v, want a rejection", err)
			}
			if len(repo.saved) != 0 {
				t.Error("nothing should be stored for a checkout that cannot become an order")
			}
		})
	}
}

// failingRepository refuses every write, as a database that is down does.
type failingRepository struct {
	*fakeRepository
}

func (failingRepository) Place(context.Context, *domain.Order, *domain.Customer, []*domain.Product) error {
	return errors.New("the database is unreachable")
}

func TestAFailureOfThisServiceIsRetriedRatherThanRejected(t *testing.T) {
	t.Parallel()

	handler := orders.NewCheckoutHandler(failingRepository{newFakeRepository()})

	err := handler.Handle(t.Context(), checkoutEnvelope(t, uuid.New()))

	// Nothing is wrong with the message, so it has to come back once the
	// database does. Rejecting it would lose the order.
	if err == nil || messaging.IsRejected(err) {
		t.Fatalf("error = %v, want a plain failure that is delivered again", err)
	}
}

func TestTheSameCheckoutDeliveredTwiceMakesOneOrder(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCheckoutHandler(repo)

	deliver := inbox.Idempotent(&fakeInbox{done: map[uuid.UUID]bool{}},
		"ordering-checkout", slog.New(slog.DiscardHandler), handler.Handle)

	envelope := checkoutEnvelope(t, uuid.New())

	// Publishing is at-least-once, so a repeat is the normal case rather than
	// an exceptional one. Two orders would mean charging twice.
	for range 3 {
		if err := deliver(t.Context(), envelope); err != nil {
			t.Fatalf("delivering: %v", err)
		}
	}

	if len(repo.saved) != 1 {
		t.Errorf("saved %d orders, want one", len(repo.saved))
	}
}

func TestOrderReferencesDoNotRepeat(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCheckoutHandler(repo)

	seen := map[string]bool{}
	for range 200 {
		if err := handler.Handle(t.Context(), checkoutEnvelope(t, uuid.New())); err != nil {
			t.Fatalf("handling: %v", err)
		}
	}

	for _, order := range repo.saved {
		name := order.OrderName().String()
		if strings.ContainsAny(name, "IO01") {
			t.Errorf("reference %q uses a character that is easy to misread", name)
		}
		seen[name] = true
	}

	// Not a uniqueness guarantee - the reference is a label, not a key - but a
	// generator that repeated itself constantly would be useless.
	if len(seen) < 190 {
		t.Errorf("%d distinct references out of 200, want them spread out", len(seen))
	}
}
