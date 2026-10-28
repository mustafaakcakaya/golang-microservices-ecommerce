package orders_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/orders"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

func TestGetOrdersReturnsOnePageAndTheTotalCount(t *testing.T) {
	t.Parallel()

	customer := customerID(t)
	repo := newFakeRepository(
		storedOrder(t, customer),
		storedOrder(t, customer),
		storedOrder(t, customer),
	)
	handler := orders.NewGetOrdersHandler(repo)

	result, err := handler.Handle(t.Context(), orders.GetOrdersQuery{
		Page: pagination.Request{PageIndex: 1, PageSize: 2},
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	// The count describes the whole set, not the page, so a client can render
	// "page 2 of 2" without asking again.
	if result.Orders.Count != 3 {
		t.Errorf("count = %d, want 3", result.Orders.Count)
	}
	if len(result.Orders.Data) != 1 {
		t.Fatalf("data = %d orders, want the single order on the second page", len(result.Orders.Data))
	}
	if want := repo.all()[2].ID().UUID(); result.Orders.Data[0].ID != want {
		t.Errorf("order = %s, want %s, the last in the list", result.Orders.Data[0].ID, want)
	}
}

func TestAnEmptyPageSerializesAsAnEmptyList(t *testing.T) {
	t.Parallel()

	handler := orders.NewGetOrdersHandler(newFakeRepository())

	result, err := handler.Handle(t.Context(), orders.GetOrdersQuery{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	body, err := json.Marshal(result.Orders)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	// A null here would make every client handle two shapes of "no orders".
	if !strings.Contains(string(body), `"data":[]`) {
		t.Errorf("body = %s, want an empty data array", body)
	}
}

func TestAViewShowsTheCardOnlyByItsLastFourDigits(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(storedOrder(t, customerID(t)))
	handler := orders.NewGetOrdersHandler(repo)

	result, err := handler.Handle(t.Context(), orders.GetOrdersQuery{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	body, err := json.Marshal(result.Orders)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	rendered := string(body)

	// The whole reason the read model is a separate type: reading an order back
	// must not hand out the token a charge would be made with.
	if strings.Contains(rendered, paymentToken) || strings.Contains(rendered, "paymentToken") {
		t.Errorf("the response leaked the payment token: %s", rendered)
	}
	if !strings.Contains(rendered, `"cardLast4":"4444"`) {
		t.Errorf("the last four digits are missing: %s", rendered)
	}
}

func TestTheViewCarriesTheDerivedTotal(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(storedOrder(t, customerID(t)))
	handler := orders.NewGetOrdersHandler(repo)

	result, err := handler.Handle(t.Context(), orders.GetOrdersQuery{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	view := result.Orders.Data[0]
	if !view.TotalPrice.Equal(view.Items[0].Price) {
		t.Errorf("total = %s, want the sum of the lines (%s)", view.TotalPrice, view.Items[0].Price)
	}
}

func TestAnUnpaidOrderReportsNoPayment(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(unpaidOrder(t, customerID(t)))
	handler := orders.NewGetOrdersHandler(repo)

	result, err := handler.Handle(t.Context(), orders.GetOrdersQuery{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	if result.Orders.Data[0].Payment != nil {
		t.Errorf("payment = %+v, want it absent for an order stored without one",
			result.Orders.Data[0].Payment)
	}

	body, err := json.Marshal(result.Orders)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	// Null rather than an object of empty strings, which would read like a
	// card nobody filled in.
	if !strings.Contains(string(body), `"payment":null`) {
		t.Errorf("body = %s, want a null payment", body)
	}
}

func TestGetOrderByIDHandlerReportsAnUnknownOrderAsNotFound(t *testing.T) {
	t.Parallel()

	handler := orders.NewGetOrderByIDHandler(newFakeRepository(storedOrder(t, customerID(t))))

	_, err := handler.Handle(t.Context(), orders.GetOrderByIDQuery{ID: uuid.New()})

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Fatalf("kind = %v, want KindNotFound (error %v)", got, err)
	}
}

func TestGetOrdersByCustomerReturnsOnlyTheirOrders(t *testing.T) {
	t.Parallel()

	mine, theirs := customerID(t), customerID(t)
	repo := newFakeRepository(
		storedOrder(t, mine),
		storedOrder(t, theirs),
	)
	handler := orders.NewGetOrdersByCustomerHandler(repo)

	result, err := handler.Handle(t.Context(), orders.GetOrdersByCustomerQuery{CustomerID: mine.UUID()})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	if len(result.Orders) != 1 || result.Orders[0].CustomerID != mine.UUID() {
		t.Fatalf("orders = %+v, want only the customer's own", result.Orders)
	}
}
