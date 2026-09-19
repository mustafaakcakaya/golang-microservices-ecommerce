package orders_test

import (
	"context"
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

// fakeSnapshots records what the handler copied into this service.
type fakeSnapshots struct {
	customers map[domain.CustomerID]string
	products  map[domain.ProductID]product
}

type product struct {
	name  string
	price decimal.Decimal
}

func newFakeSnapshots() *fakeSnapshots {
	return &fakeSnapshots{
		customers: map[domain.CustomerID]string{},
		products:  map[domain.ProductID]product{},
	}
}

func (s *fakeSnapshots) SaveCustomer(_ context.Context, id domain.CustomerID, name, _ string) error {
	s.customers[id] = name
	return nil
}

func (s *fakeSnapshots) SaveProduct(
	_ context.Context, id domain.ProductID, name string, price decimal.Decimal,
) error {
	s.products[id] = product{name: name, price: price}
	return nil
}

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

func checkoutEnvelope(t *testing.T, customerID uuid.UUID) messaging.Envelope {
	t.Helper()

	event := &basketv1.CheckoutRequested{
		Header:       messaging.NewHeader("trace-1"),
		UserName:     "swn01",
		CustomerID:   customerID,
		FirstName:    "Mustafa",
		LastName:     "Akcakaya",
		EmailAddress: "mustafa@example.com",
		AddressLine:  "Test Address",
		Country:      "Turkey",
		State:        "Istanbul",
		ZipCode:      "34000",
		TotalPrice:   messaging.NewMoney(decimal.NewFromInt(1400)),
		Lines: []basketv1.Line{
			{
				ProductID:   uuid.New(),
				ProductName: "iPhone X",
				Quantity:    2,
				Price:       messaging.NewMoney(decimal.NewFromInt(500)),
			},
			{
				ProductID:   uuid.New(),
				ProductName: "Samsung 10",
				Quantity:    1,
				Price:       messaging.NewMoney(decimal.NewFromInt(400)),
			},
		},
	}

	envelope, err := messaging.NewEnvelope(event, customerID.String())
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}

	return envelope
}

func TestACheckoutBecomesAnUnpaidOrderOfItsOwnLines(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	snapshots := newFakeSnapshots()
	handler := orders.NewCheckoutHandler(repo, snapshots)

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
		t.Fatalf("items = %+v, want both checkout lines", items)
	}
	if !order.TotalPrice().Equal(decimal.NewFromInt(1400)) {
		t.Errorf("total = %s, want 1400", order.TotalPrice())
	}

	// No card details travelled, so the order is not paid for yet.
	if !order.Payment.IsZero() || order.Status != domain.OrderStatusDraft {
		t.Errorf("order = %v/%v, want an unpaid draft", order.Payment, order.Status)
	}

	// The domain fixes the length, so the user name cannot be the order name.
	if got := order.OrderName.String(); len(got) != 5 {
		t.Errorf("order name = %q, want five characters", got)
	}
}

func TestTheOrderRefersToProductsThisServiceHasRecorded(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	snapshots := newFakeSnapshots()
	handler := orders.NewCheckoutHandler(repo, snapshots)

	customerID := uuid.New()
	if err := handler.Handle(t.Context(), checkoutEnvelope(t, customerID)); err != nil {
		t.Fatalf("handling: %v", err)
	}

	// Ordering keeps its own copies: an order has to stay readable when the
	// catalog renames or removes a product, and the order's foreign keys need
	// something to point at.
	if len(snapshots.products) != 2 {
		t.Fatalf("recorded %d products, want both", len(snapshots.products))
	}
	for _, item := range repo.saved[0].Items() {
		recorded, known := snapshots.products[item.ProductID]
		if !known {
			t.Errorf("product %s was ordered but never recorded", item.ProductID)
			continue
		}
		// The price recorded is the one the order was placed at.
		if !recorded.price.Equal(item.Price) {
			t.Errorf("product %s recorded at %s, ordered at %s",
				item.ProductID, recorded.price, item.Price)
		}
	}

	customer, err := domain.NewCustomerID(customerID)
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}
	if name := snapshots.customers[customer]; name != "Mustafa Akcakaya" {
		t.Errorf("customer recorded as %q, want the buyer's name", name)
	}
}

func TestTheSameCheckoutDeliveredTwiceMakesOneOrder(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCheckoutHandler(repo, newFakeSnapshots())

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

func TestACheckoutWithoutLinesIsRefused(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCheckoutHandler(repo, newFakeSnapshots())

	event := &basketv1.CheckoutRequested{
		Header:       messaging.NewHeader(""),
		CustomerID:   uuid.New(),
		EmailAddress: "mustafa@example.com",
		AddressLine:  "Test Address",
	}
	envelope, err := messaging.NewEnvelope(event, event.CustomerID.String())
	if err != nil {
		t.Fatalf("enveloping: %v", err)
	}

	// An order with no lines has nothing to fulfil; failing means the message
	// is delivered again rather than quietly becoming an empty order.
	if err := handler.Handle(t.Context(), envelope); err == nil {
		t.Fatal("a checkout with no lines should be refused")
	}
	if len(repo.saved) != 0 {
		t.Error("nothing should be stored for a checkout with no lines")
	}
}

func TestOrderReferencesDoNotRepeat(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCheckoutHandler(repo, newFakeSnapshots())

	seen := map[string]bool{}
	for range 200 {
		if err := handler.Handle(t.Context(), checkoutEnvelope(t, uuid.New())); err != nil {
			t.Fatalf("handling: %v", err)
		}
	}

	for _, order := range repo.saved {
		name := order.OrderName.String()
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
