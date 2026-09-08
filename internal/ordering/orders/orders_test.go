package orders_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/orders"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

const (
	cardNumber = "5555555555554444"
	cvv        = "355"
)

// fakeRepository is an in-memory stand-in so handler behaviour can be tested
// without Docker; the SQL Server implementation has its own integration tests.
type fakeRepository struct {
	saved []*domain.Order
	items map[uuid.UUID]*domain.Order
}

func newFakeRepository(seed ...*domain.Order) *fakeRepository {
	repo := &fakeRepository{items: make(map[uuid.UUID]*domain.Order)}
	for _, order := range seed {
		repo.items[order.ID.UUID()] = order
	}
	return repo
}

func (r *fakeRepository) Save(_ context.Context, order *domain.Order) error {
	r.items[order.ID.UUID()] = order
	r.saved = append(r.saved, order)
	return nil
}

func (r *fakeRepository) ByID(_ context.Context, id domain.OrderID) (*domain.Order, error) {
	order, ok := r.items[id.UUID()]
	if !ok {
		return nil, apperr.NotFound("Order", id)
	}
	return order, nil
}

func (r *fakeRepository) List(_ context.Context, page pagination.Request) ([]*domain.Order, int64, error) {
	all := r.all()

	start := min(page.Offset(), len(all))
	end := min(start+page.Limit(), len(all))

	return all[start:end], int64(len(all)), nil
}

func (r *fakeRepository) ByName(_ context.Context, name string) ([]*domain.Order, error) {
	var matches []*domain.Order
	for _, order := range r.all() {
		if strings.Contains(order.OrderName.String(), name) {
			matches = append(matches, order)
		}
	}
	return matches, nil
}

func (r *fakeRepository) ByCustomer(_ context.Context, customerID domain.CustomerID) ([]*domain.Order, error) {
	var matches []*domain.Order
	for _, order := range r.all() {
		if order.CustomerID == customerID {
			matches = append(matches, order)
		}
	}
	return matches, nil
}

func (r *fakeRepository) Delete(_ context.Context, id domain.OrderID) error {
	if _, ok := r.items[id.UUID()]; !ok {
		return apperr.NotFound("Order", id)
	}
	delete(r.items, id.UUID())
	return nil
}

// all returns the stored orders in a stable order, so paging assertions do not
// depend on map iteration.
func (r *fakeRepository) all() []*domain.Order {
	all := make([]*domain.Order, 0, len(r.items))
	for _, order := range r.items {
		all = append(all, order)
	}

	slices.SortFunc(all, func(a, b *domain.Order) int {
		return strings.Compare(a.OrderName.String(), b.OrderName.String())
	})

	return all
}

func sampleAddress() orders.AddressInput {
	return orders.AddressInput{
		FirstName:    "Mustafa",
		LastName:     "Akcakaya",
		EmailAddress: "mustafa@example.com",
		AddressLine:  "Test Address",
		Country:      "Turkey",
		State:        "Istanbul",
		ZipCode:      "34000",
	}
}

func samplePayment() orders.PaymentInput {
	return orders.PaymentInput{
		CardName:      "Mustafa Akcakaya",
		CardNumber:    cardNumber,
		Expiration:    "12/28",
		CVV:           cvv,
		PaymentMethod: 1,
	}
}

func sampleCommand() orders.CreateOrderCommand {
	return orders.CreateOrderCommand{
		CustomerID:      uuid.New(),
		OrderName:       "ORD_1",
		ShippingAddress: sampleAddress(),
		BillingAddress:  sampleAddress(),
		Payment:         samplePayment(),
		Items: []orders.LineInput{
			{ProductID: uuid.New(), Quantity: 2, Price: decimal.NewFromInt(500)},
		},
	}
}

// storedOrder builds an order the way persistence hands one back: restored,
// with no pending events.
func storedOrder(t *testing.T, name string, customerID domain.CustomerID) *domain.Order {
	t.Helper()

	orderID, err := domain.NewOrderID(uuid.New())
	if err != nil {
		t.Fatalf("order id: %v", err)
	}
	orderName, err := domain.NewOrderName(name)
	if err != nil {
		t.Fatalf("order name: %v", err)
	}
	address, err := sampleAddressDomain()
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	payment, err := domain.NewPayment("Mustafa Akcakaya", cardNumber, "12/28", cvv, 1)
	if err != nil {
		t.Fatalf("payment: %v", err)
	}
	productID, err := domain.NewProductID(uuid.New())
	if err != nil {
		t.Fatalf("product id: %v", err)
	}

	item := domain.OrderItem{
		ID:        domain.OrderItemID(uuid.New()),
		OrderID:   orderID,
		ProductID: productID,
		Quantity:  1,
		Price:     decimal.NewFromInt(100),
	}

	return domain.Restore(orderID, customerID, orderName, address, address, payment,
		domain.OrderStatusPending, []domain.OrderItem{item})
}

func sampleAddressDomain() (domain.Address, error) {
	a := sampleAddress()
	return domain.NewAddress(a.FirstName, a.LastName, a.EmailAddress, a.AddressLine, a.Country, a.State, a.ZipCode)
}

func customerID(t *testing.T) domain.CustomerID {
	t.Helper()

	id, err := domain.NewCustomerID(uuid.New())
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}
	return id
}

func TestCreateStoresTheOrderWithItsLines(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCreateOrderHandler(repo)

	command := sampleCommand()
	result, err := handler.Handle(t.Context(), command)
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	if result.ID == uuid.Nil {
		t.Fatal("the caller should learn the id without a round trip")
	}

	stored, ok := repo.items[result.ID]
	if !ok {
		t.Fatal("the order was not saved")
	}
	if stored.OrderName.String() != "ORD_1" || stored.Status != domain.OrderStatusPending {
		t.Errorf("order = %s/%v, want ORD_1/Pending", stored.OrderName, stored.Status)
	}
	if items := stored.Items(); len(items) != 1 || items[0].Quantity != 2 {
		t.Fatalf("items = %+v, want the single requested line", items)
	}
	if !stored.TotalPrice().Equal(decimal.NewFromInt(1000)) {
		t.Errorf("total = %s, want 1000", stored.TotalPrice())
	}
}

func TestCreateLeavesOrderCreatedOnTheAggregate(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCreateOrderHandler(repo)

	if _, err := handler.Handle(t.Context(), sampleCommand()); err != nil {
		t.Fatalf("creating: %v", err)
	}

	// The handler must not publish or drop the event: it belongs to the same
	// transaction as the order, which the repository owns.
	events := repo.saved[0].Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want the pending OrderCreated", len(events))
	}
	if _, ok := events[0].(domain.OrderCreated); !ok {
		t.Errorf("event = %T, want OrderCreated", events[0])
	}
}

func TestCreateReportsADomainRuleAsABadRequest(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := orders.NewCreateOrderHandler(repo)

	command := sampleCommand()
	command.OrderName = "ORD" // the domain requires exactly five characters

	_, err := handler.Handle(t.Context(), command)

	// Unclassified errors default to internal, so a rule violation that is not
	// reclassified would answer 500 and blame the server for a caller mistake.
	if got := apperr.KindOf(err); got != apperr.KindBadRequest {
		t.Fatalf("kind = %v, want KindBadRequest (error %v)", got, err)
	}
	if len(repo.items) != 0 {
		t.Error("a rejected order must not be stored")
	}
}

func TestCreateRejectsAnOrderWithoutLines(t *testing.T) {
	t.Parallel()

	handle := cqrs.Chain[orders.CreateOrderCommand, orders.CreateOrderResult](
		orders.NewCreateOrderHandler(newFakeRepository()),
		cqrs.Validating[orders.CreateOrderCommand, orders.CreateOrderResult](),
	)

	command := sampleCommand()
	command.Items = nil

	_, err := handle(t.Context(), command)

	if got := apperr.KindOf(err); got != apperr.KindValidation {
		t.Fatalf("kind = %v, want KindValidation (error %v)", got, err)
	}
}

func TestACommandNeverRendersCardDetails(t *testing.T) {
	t.Parallel()

	command := sampleCommand()

	for _, rendered := range []string{
		fmt.Sprintf("%v", command),
		fmt.Sprintf("%+v", command),
		fmt.Sprintf("%v", command.Payment),
	} {
		if strings.Contains(rendered, cardNumber) {
			t.Errorf("a rendered command leaked the card number: %s", rendered)
		}
		if strings.Contains(rendered, cvv) {
			t.Errorf("a rendered command leaked the CVV: %s", rendered)
		}
	}

	// Marshalling fails loudly rather than emitting a body that silently
	// dropped the fields.
	if _, err := json.Marshal(command); err == nil {
		t.Error("a command carrying payment details must refuse to serialize")
	}
}

func TestUpdateReportsAnUnknownOrderAsNotFound(t *testing.T) {
	t.Parallel()

	handler := orders.NewUpdateOrderHandler(newFakeRepository())

	_, err := handler.Handle(t.Context(), orders.UpdateOrderCommand{
		ID:              uuid.New(),
		OrderName:       "ORD_9",
		ShippingAddress: sampleAddress(),
		BillingAddress:  sampleAddress(),
		Payment:         samplePayment(),
		Status:          "Completed",
	})

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Fatalf("kind = %v, want KindNotFound (error %v)", got, err)
	}
}

func TestUpdateChangesTheOrderAndKeepsItsLines(t *testing.T) {
	t.Parallel()

	existing := storedOrder(t, "ORD_1", customerID(t))
	repo := newFakeRepository(existing)
	handler := orders.NewUpdateOrderHandler(repo)

	result, err := handler.Handle(t.Context(), orders.UpdateOrderCommand{
		ID:              existing.ID.UUID(),
		OrderName:       "ORD_2",
		ShippingAddress: sampleAddress(),
		BillingAddress:  sampleAddress(),
		Payment:         samplePayment(),
		Status:          "Completed",
	})
	if err != nil {
		t.Fatalf("updating: %v", err)
	}
	if !result.IsSuccess {
		t.Error("the update reported no success")
	}

	stored := repo.items[existing.ID.UUID()]
	if stored.OrderName.String() != "ORD_2" || stored.Status != domain.OrderStatusCompleted {
		t.Errorf("order = %s/%v, want ORD_2/Completed", stored.OrderName, stored.Status)
	}
	// Lines are not part of an update, so they survive it untouched.
	if items := stored.Items(); len(items) != 1 {
		t.Errorf("items = %+v, want the line the order was placed with", items)
	}
	if len(stored.Events()) != 1 {
		t.Errorf("events = %d, want the pending OrderUpdated", len(stored.Events()))
	}
}

func TestUpdateRejectsAnUnknownStatus(t *testing.T) {
	t.Parallel()

	existing := storedOrder(t, "ORD_1", customerID(t))
	handler := orders.NewUpdateOrderHandler(newFakeRepository(existing))

	_, err := handler.Handle(t.Context(), orders.UpdateOrderCommand{
		ID:              existing.ID.UUID(),
		OrderName:       "ORD_1",
		ShippingAddress: sampleAddress(),
		BillingAddress:  sampleAddress(),
		Payment:         samplePayment(),
		Status:          "Shipped",
	})

	if got := apperr.KindOf(err); got != apperr.KindBadRequest {
		t.Fatalf("kind = %v, want KindBadRequest (error %v)", got, err)
	}
}

func TestDeleteRemovesTheOrder(t *testing.T) {
	t.Parallel()

	existing := storedOrder(t, "ORD_1", customerID(t))
	repo := newFakeRepository(existing)
	handler := orders.NewDeleteOrderHandler(repo)

	result, err := handler.Handle(t.Context(), orders.DeleteOrderCommand{ID: existing.ID.UUID()})
	if err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if !result.IsSuccess {
		t.Error("the delete reported no success")
	}
	if len(repo.items) != 0 {
		t.Error("the order is still stored")
	}
}

func TestDeleteReportsAnUnknownOrderAsNotFound(t *testing.T) {
	t.Parallel()

	handler := orders.NewDeleteOrderHandler(newFakeRepository())

	_, err := handler.Handle(t.Context(), orders.DeleteOrderCommand{ID: uuid.New()})

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Fatalf("kind = %v, want KindNotFound (error %v)", got, err)
	}
}
