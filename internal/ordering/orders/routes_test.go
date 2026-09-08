package orders_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/orders"
)

// newRouter mounts the real routing table, so these tests cover the wiring as
// well as the handlers: a slice registered under the wrong path or without its
// validation middleware fails here.
func newRouter(repo orders.Repository) http.Handler {
	router := chi.NewRouter()
	orders.RegisterRoutes(router, repo, slog.New(slog.DiscardHandler))
	return router
}

func do(t *testing.T, router http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request := httptest.NewRequestWithContext(t.Context(), method, target, reader)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder
}

// createBody wraps an order in the request envelope.
func createBody(order map[string]any) map[string]any {
	return map[string]any{"order": order}
}

// sampleOrderBody builds a request body by hand: the command type refuses to
// marshal, which is exactly the protection being relied on elsewhere.
func sampleOrderBody() map[string]any {
	address := map[string]any{
		"firstName":    "Mustafa",
		"lastName":     "Akcakaya",
		"emailAddress": "mustafa@example.com",
		"addressLine":  "Test Address",
		"country":      "Turkey",
		"state":        "Istanbul",
		"zipCode":      "34000",
	}

	return map[string]any{
		"customerId":      "6f9619ff-8b86-d011-b42d-00cf4fc964ff",
		"orderName":       "ORD_1",
		"shippingAddress": address,
		"billingAddress":  address,
		"payment": map[string]any{
			"cardName":      "Mustafa Akcakaya",
			"cardNumber":    cardNumber,
			"expiration":    "12/28",
			"cvv":           cvv,
			"paymentMethod": 1,
		},
		"orderItems": []map[string]any{
			{"productId": "7f9619ff-8b86-d011-b42d-00cf4fc964ff", "quantity": 2, "price": 500},
		},
	}
}

func TestPostOrdersCreatesAndPointsAtTheNewOrder(t *testing.T) {
	t.Parallel()

	router := newRouter(newFakeRepository())

	response := do(t, router, http.MethodPost, "/orders", createBody(sampleOrderBody()))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", response.Code, response.Body)
	}

	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if location := response.Header().Get("Location"); location != "/orders/"+body.ID {
		t.Errorf("Location = %q, want the new order's path", location)
	}
}

func TestPostOrdersRejectsAnOrderWithoutLines(t *testing.T) {
	t.Parallel()

	router := newRouter(newFakeRepository())

	order := sampleOrderBody()
	order["orderItems"] = []map[string]any{}

	response := do(t, router, http.MethodPost, "/orders", createBody(order))

	// Validation runs in the pipeline, so this never reaches the handler.
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "validationErrors") {
		t.Errorf("body = %s, want a field-by-field breakdown", response.Body)
	}
}

func TestPostOrdersReportsABrokenOrderNameAsABadRequest(t *testing.T) {
	t.Parallel()

	router := newRouter(newFakeRepository())

	order := sampleOrderBody()
	order["orderName"] = "ORD"

	response := do(t, router, http.MethodPost, "/orders", createBody(order))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", response.Code, response.Body)
	}
}

func TestAResponseNeverCarriesCardDetails(t *testing.T) {
	t.Parallel()

	router := newRouter(newFakeRepository(storedOrder(t, "ORD_1", customerID(t))))

	response := do(t, router, http.MethodGet, "/orders", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if body := response.Body.String(); strings.Contains(body, cardNumber) || strings.Contains(body, cvv) {
		t.Errorf("the response leaked payment details: %s", body)
	}
}

func TestGetOrdersByNameAndByCustomerResolveToDifferentRoutes(t *testing.T) {
	t.Parallel()

	customer := customerID(t)
	router := newRouter(newFakeRepository(storedOrder(t, "ORD_1", customer)))

	byName := do(t, router, http.MethodGet, "/orders/ORD_1", nil)
	if byName.Code != http.StatusOK {
		t.Fatalf("by name: status = %d, want 200", byName.Code)
	}

	// The static /customer segment must win over the {orderName} parameter,
	// or a customer id would be searched for as an order name.
	byCustomer := do(t, router, http.MethodGet, "/orders/customer/"+customer.String(), nil)
	if byCustomer.Code != http.StatusOK {
		t.Fatalf("by customer: status = %d, want 200 (body %s)", byCustomer.Code, byCustomer.Body)
	}

	var body struct {
		Orders []orders.OrderView `json:"orders"`
	}
	if err := json.Unmarshal(byCustomer.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body.Orders) != 1 {
		t.Fatalf("orders = %d, want the customer's single order", len(body.Orders))
	}
}

func TestDeleteOrdersRejectsAnIDThatIsNotAUUID(t *testing.T) {
	t.Parallel()

	router := newRouter(newFakeRepository())

	response := do(t, router, http.MethodDelete, "/orders/not-a-uuid", nil)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", response.Code, response.Body)
	}
}

func TestDeleteOrdersReportsAnUnknownOrderAsNotFound(t *testing.T) {
	t.Parallel()

	router := newRouter(newFakeRepository())

	response := do(t, router, http.MethodDelete, "/orders/6f9619ff-8b86-d011-b42d-00cf4fc964ff", nil)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", response.Code, response.Body)
	}
}
