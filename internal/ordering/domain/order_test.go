package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
)

func newOrder(t *testing.T) *domain.Order {
	t.Helper()

	orderID, err := domain.NewOrderID(uuid.New())
	if err != nil {
		t.Fatalf("order id: %v", err)
	}
	customerID, err := domain.NewCustomerID(uuid.New())
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}
	orderName, err := domain.NewOrderName("ORD_1")
	if err != nil {
		t.Fatalf("order name: %v", err)
	}
	address, err := domain.NewAddress("Mustafa", "Akcakaya", "mustafa@example.com", "Test Address", "Turkey", "Istanbul", "34000")
	if err != nil {
		t.Fatalf("address: %v", err)
	}

	return domain.NewOrder(orderID, customerID, orderName, address, address, newPayment(t))
}

func productID(t *testing.T) domain.ProductID {
	t.Helper()

	id, err := domain.NewProductID(uuid.New())
	if err != nil {
		t.Fatalf("product id: %v", err)
	}
	return id
}

func TestCreatingAnOrderRaisesOrderCreated(t *testing.T) {
	t.Parallel()

	order := newOrder(t)

	events := order.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want exactly one", len(events))
	}
	created, ok := events[0].(domain.OrderCreated)
	if !ok {
		t.Fatalf("event = %T, want OrderCreated", events[0])
	}
	if created.Order != order {
		t.Error("the event should carry the order it describes")
	}
	if order.Status != domain.OrderStatusPending {
		t.Errorf("status = %v, want Pending", order.Status)
	}
}

func TestPullEventsClearsThemSoTheyCannotPublishTwice(t *testing.T) {
	t.Parallel()

	order := newOrder(t)

	first := order.PullEvents()
	second := order.PullEvents()

	if len(first) != 1 {
		t.Fatalf("first pull = %d events, want 1", len(first))
	}
	// A second publisher run must not re-send what the first already took.
	if len(second) != 0 {
		t.Errorf("second pull = %d events, want none", len(second))
	}
}

func TestUpdateRaisesOrderUpdatedAndAppliesChanges(t *testing.T) {
	t.Parallel()

	order := newOrder(t)
	order.PullEvents()

	newName, err := domain.NewOrderName("ORD_9")
	if err != nil {
		t.Fatalf("order name: %v", err)
	}
	address, err := domain.NewAddress("John", "Doe", "john@example.com", "Broadway 1", "England", "Nottingham", "08050")
	if err != nil {
		t.Fatalf("address: %v", err)
	}

	if err := order.Update(newName, address, address, newPayment(t), domain.OrderStatusCompleted); err != nil {
		t.Fatalf("updating: %v", err)
	}

	if order.OrderName.String() != "ORD_9" || order.Status != domain.OrderStatusCompleted {
		t.Errorf("order = %s/%v, want the updated values", order.OrderName, order.Status)
	}

	events := order.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want one", len(events))
	}
	if _, ok := events[0].(domain.OrderUpdated); !ok {
		t.Errorf("event = %T, want OrderUpdated", events[0])
	}
}

func TestUpdateRejectsAnUnknownStatus(t *testing.T) {
	t.Parallel()

	order := newOrder(t)
	order.PullEvents()

	err := order.Update(order.OrderName, order.ShippingAddress, order.BillingAddress, order.Payment, domain.OrderStatus(99))

	if err == nil {
		t.Fatal("an unknown status should be rejected")
	}
	// A rejected command must leave nothing behind.
	if len(order.Events()) != 0 {
		t.Error("a rejected update should raise no event")
	}
}

func TestTotalPriceMultipliesByQuantity(t *testing.T) {
	t.Parallel()

	order := newOrder(t)

	if err := order.Add(productID(t), 2, decimal.NewFromInt(500)); err != nil {
		t.Fatalf("adding: %v", err)
	}
	if err := order.Add(productID(t), 1, decimal.NewFromInt(400)); err != nil {
		t.Fatalf("adding: %v", err)
	}

	if want := decimal.NewFromInt(1400); !order.TotalPrice().Equal(want) {
		t.Errorf("total = %s, want %s", order.TotalPrice(), want)
	}
}

func TestEmptyOrderTotalsZero(t *testing.T) {
	t.Parallel()

	if got := newOrder(t).TotalPrice(); !got.IsZero() {
		t.Errorf("total = %s, want zero", got)
	}
}

func TestAddRejectsNonPositiveQuantityAndPrice(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		quantity int
		price    int64
	}{
		"zero quantity":     {0, 500},
		"negative quantity": {-1, 500},
		"zero price":        {1, 0},
		"negative price":    {1, -500},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			order := newOrder(t)
			err := order.Add(productID(t), tc.quantity, decimal.NewFromInt(tc.price))

			if err == nil {
				t.Fatal("expected a rule violation")
			}
			if len(order.Items()) != 0 {
				t.Error("a rejected line should not be added")
			}
		})
	}
}

func TestRemoveDropsEveryLineForTheProductAndIgnoresUnknownOnes(t *testing.T) {
	t.Parallel()

	order := newOrder(t)
	target := productID(t)
	other := productID(t)

	for _, id := range []domain.ProductID{target, other, target} {
		if err := order.Add(id, 1, decimal.NewFromInt(100)); err != nil {
			t.Fatalf("adding: %v", err)
		}
	}

	order.Remove(target)

	items := order.Items()
	if len(items) != 1 || items[0].ProductID != other {
		t.Errorf("items = %+v, want only the other product", items)
	}

	// Removing something the order never had is not an error.
	order.Remove(productID(t))
	if len(order.Items()) != 1 {
		t.Error("removing an absent product should change nothing")
	}
}

func TestItemsCannotBeMutatedThroughTheReturnedSlice(t *testing.T) {
	t.Parallel()

	order := newOrder(t)
	if err := order.Add(productID(t), 1, decimal.NewFromInt(100)); err != nil {
		t.Fatalf("adding: %v", err)
	}

	items := order.Items()
	items[0].Quantity = 999

	// The aggregate owns its lines; handing out the backing array would let a
	// caller change the total without going through the aggregate.
	if order.Items()[0].Quantity != 1 {
		t.Error("mutating the returned slice changed the order")
	}
}

func TestRestoreDoesNotRaiseEvents(t *testing.T) {
	t.Parallel()

	original := newOrder(t)
	original.PullEvents()

	restored := domain.Restore(
		original.ID, original.CustomerID, original.OrderName,
		original.ShippingAddress, original.BillingAddress, original.Payment,
		domain.OrderStatusCompleted, nil,
	)

	// Loading is not a domain change; replaying OrderCreated here would publish
	// an event for something that happened long ago.
	if len(restored.Events()) != 0 {
		t.Errorf("restoring raised %d events, want none", len(restored.Events()))
	}
	if restored.Status != domain.OrderStatusCompleted {
		t.Errorf("status = %v, want the stored one", restored.Status)
	}
}
