package carts_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// fakeRepository keeps baskets in memory so handler behaviour can be checked
// without Docker; the PostgreSQL implementation has its own integration tests.
type fakeRepository struct {
	items map[string]carts.ShoppingCart
}

func newFakeRepository(seed ...carts.ShoppingCart) *fakeRepository {
	repo := &fakeRepository{items: make(map[string]carts.ShoppingCart)}
	for _, cart := range seed {
		repo.items[cart.UserName] = cart
	}
	return repo
}

func (r *fakeRepository) GetByUserName(_ context.Context, userName string) (carts.ShoppingCart, error) {
	cart, ok := r.items[userName]
	if !ok {
		return carts.ShoppingCart{}, apperr.NotFound("Basket", userName)
	}
	return cart, nil
}

func (r *fakeRepository) Store(_ context.Context, cart carts.ShoppingCart) error {
	r.items[cart.UserName] = cart
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, userName string) error {
	delete(r.items, userName)
	return nil
}

// fakeDiscounts answers a fixed amount per product name.
type fakeDiscounts struct {
	amounts map[string]int32
	failOn  error
	calls   []string
}

func (d *fakeDiscounts) AmountFor(_ context.Context, productName string) (int32, error) {
	d.calls = append(d.calls, productName)
	if d.failOn != nil {
		return 0, d.failOn
	}
	return d.amounts[productName], nil
}

func noDiscounts() *fakeDiscounts { return &fakeDiscounts{amounts: map[string]int32{}} }

func sampleCart(userName string) carts.ShoppingCart {
	return carts.ShoppingCart{
		UserName: userName,
		Items: []carts.ShoppingCartItem{
			{Quantity: 2, Color: "Red", Price: decimal.NewFromInt(500), ProductID: uuid.New(), ProductName: "iPhone X"},
			{Quantity: 1, Color: "Blue", Price: decimal.NewFromInt(400), ProductID: uuid.New(), ProductName: "Samsung 10"},
		},
	}
}

func TestTotalPriceSumsQuantityTimesPrice(t *testing.T) {
	t.Parallel()

	got := sampleCart("mustafa").TotalPrice()

	if want := decimal.NewFromInt(1400); !got.Equal(want) {
		t.Errorf("total = %s, want %s", got, want)
	}
}

func TestEmptyCartTotalsZero(t *testing.T) {
	t.Parallel()

	if got := (carts.ShoppingCart{UserName: "empty"}).TotalPrice(); !got.IsZero() {
		t.Errorf("total = %s, want 0", got)
	}
}

func TestCartSerializesTotalPrice(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(sampleCart("mustafa"))
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}

	// The total is derived, not stored, but clients still see it.
	total, ok := decoded["totalPrice"]
	if !ok {
		t.Fatalf("totalPrice missing from %s", encoded)
	}
	if total != "1400" {
		t.Errorf("totalPrice = %v, want 1400", total)
	}
}

func TestGetBasketReportsNotFoundForUnknownUser(t *testing.T) {
	t.Parallel()

	handler := carts.NewGetBasketHandler(newFakeRepository())

	_, err := handler.Handle(context.Background(), carts.GetBasketQuery{UserName: "nobody"})

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound (error %v)", got, err)
	}
}

func TestStoreReplacesTheWholeBasket(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(sampleCart("mustafa"))
	handler := carts.NewStoreBasketHandler(repo, noDiscounts())

	replacement := carts.ShoppingCart{
		UserName: "mustafa",
		Items:    []carts.ShoppingCartItem{{Quantity: 1, Price: decimal.NewFromInt(10), ProductName: "Pen"}},
	}

	result, err := handler.Handle(context.Background(), carts.StoreBasketCommand{Cart: replacement})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserName != "mustafa" {
		t.Errorf("userName = %q, want mustafa", result.UserName)
	}

	stored, err := repo.GetByUserName(context.Background(), "mustafa")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	// A store is a replace, not a merge: the previous two lines must be gone.
	if len(stored.Items) != 1 || stored.Items[0].ProductName != "Pen" {
		t.Errorf("stored items = %+v, want only the replacement line", stored.Items)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(sampleCart("mustafa"))
	handler := carts.NewDeleteBasketHandler(repo)

	for i := range 2 {
		result, err := handler.Handle(context.Background(), carts.DeleteBasketCommand{UserName: "mustafa"})
		if err != nil {
			t.Fatalf("delete %d: %v", i+1, err)
		}
		if !result.IsSuccess {
			t.Errorf("delete %d reported failure", i+1)
		}
	}

	if len(repo.items) != 0 {
		t.Error("basket should be gone")
	}
}

func TestRoutes(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(sampleCart("mustafa"))
	router := chi.NewRouter()
	carts.RegisterRoutes(router, repo, noDiscounts(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	do := func(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("get returns the basket with its total", func(t *testing.T) {
		rec := do(t, http.MethodGet, "/basket/mustafa", "")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
		}

		var response struct {
			Cart struct {
				UserName   string `json:"userName"`
				TotalPrice string `json:"totalPrice"`
			} `json:"cart"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if response.Cart.UserName != "mustafa" || response.Cart.TotalPrice != "1400" {
			t.Errorf("cart = %+v, want mustafa with total 1400", response.Cart)
		}
	})

	t.Run("get for an unknown user is 404", func(t *testing.T) {
		if rec := do(t, http.MethodGet, "/basket/nobody", ""); rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("post stores and echoes the user name", func(t *testing.T) {
		body := `{"cart":{"userName":"john","items":[{"quantity":1,"color":"Red","price":25,"productId":"` +
			uuid.New().String() + `","productName":"Pen"}]}}`
		rec := do(t, http.MethodPost, "/basket", body)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), `"userName":"john"`) {
			t.Errorf("body = %s, want the stored user name", rec.Body)
		}
	})

	t.Run("post without a user name is rejected before storing", func(t *testing.T) {
		rec := do(t, http.MethodPost, "/basket", `{"cart":{"userName":"","items":[]}}`)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
		}

		var problem struct {
			Errors map[string]string `json:"validationErrors"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
			t.Fatalf("decoding problem details: %v", err)
		}
		// The rule lives on the nested cart, so the failing field is reported
		// as UserName rather than Cart.
		if _, ok := problem.Errors["UserName"]; !ok {
			t.Errorf("validation errors = %v, want a UserName entry", problem.Errors)
		}
	})

	t.Run("delete answers 200", func(t *testing.T) {
		if rec := do(t, http.MethodDelete, "/basket/mustafa", ""); rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}
