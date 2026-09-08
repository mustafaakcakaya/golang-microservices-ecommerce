package orders_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/orders"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

func TestGetOrdersReturnsOnePageAndTheTotalCount(t *testing.T) {
	t.Parallel()

	customer := customerID(t)
	repo := newFakeRepository(
		storedOrder(t, "ORD_1", customer),
		storedOrder(t, "ORD_2", customer),
		storedOrder(t, "ORD_3", customer),
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
	if result.Orders.Data[0].OrderName != "ORD_3" {
		t.Errorf("order = %s, want ORD_3", result.Orders.Data[0].OrderName)
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

func TestAViewNeverCarriesCardDetails(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(storedOrder(t, "ORD_1", customerID(t)))
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
	// must not be a way to read a card number back out of the database.
	if strings.Contains(rendered, cardNumber) {
		t.Errorf("the response leaked the card number: %s", rendered)
	}
	if strings.Contains(rendered, `"cvv"`) || strings.Contains(rendered, cvv) {
		t.Errorf("the response leaked the CVV: %s", rendered)
	}
	if !strings.Contains(rendered, "************4444") {
		t.Errorf("the masked card number is missing: %s", rendered)
	}
}

func TestTheViewCarriesTheDerivedTotal(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(storedOrder(t, "ORD_1", customerID(t)))
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

func TestGetOrdersByNameMatchesPartOfTheName(t *testing.T) {
	t.Parallel()

	customer := customerID(t)
	repo := newFakeRepository(
		storedOrder(t, "ORD_1", customer),
		storedOrder(t, "ORD_2", customer),
	)
	handler := orders.NewGetOrdersByNameHandler(repo)

	result, err := handler.Handle(t.Context(), orders.GetOrdersByNameQuery{Name: "ORD"})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}

	if len(result.Orders) != 2 {
		t.Fatalf("orders = %d, want both matches", len(result.Orders))
	}
}

func TestASearchThatMatchesNothingIsAnEmptyList(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(storedOrder(t, "ORD_1", customerID(t)))
	handler := orders.NewGetOrdersByNameHandler(repo)

	result, err := handler.Handle(t.Context(), orders.GetOrdersByNameQuery{Name: "XYZ"})

	// "Nothing matches" is a valid answer to a search, not a missing resource.
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(result.Orders) != 0 {
		t.Errorf("orders = %d, want none", len(result.Orders))
	}
}

func TestGetOrdersByCustomerReturnsOnlyTheirOrders(t *testing.T) {
	t.Parallel()

	mine, theirs := customerID(t), customerID(t)
	repo := newFakeRepository(
		storedOrder(t, "ORD_1", mine),
		storedOrder(t, "ORD_2", theirs),
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
