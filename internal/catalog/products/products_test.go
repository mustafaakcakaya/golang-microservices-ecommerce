package products_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/catalog/products"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

// fakeRepository is an in-memory stand-in so handler behaviour can be tested
// without Docker; the Postgres implementation has its own integration tests.
type fakeRepository struct {
	items map[uuid.UUID]products.Product
}

func newFakeRepository(seed ...products.Product) *fakeRepository {
	repo := &fakeRepository{items: make(map[uuid.UUID]products.Product)}
	for _, item := range seed {
		repo.items[item.ID] = item
	}
	return repo
}

func (r *fakeRepository) List(_ context.Context, page pagination.Request) ([]products.Product, int64, error) {
	all := make([]products.Product, 0, len(r.items))
	for _, item := range r.items {
		all = append(all, item)
	}

	start := min(page.Offset(), len(all))
	end := min(start+page.Limit(), len(all))

	return all[start:end], int64(len(all)), nil
}

func (r *fakeRepository) ByID(_ context.Context, id uuid.UUID) (products.Product, error) {
	item, ok := r.items[id]
	if !ok {
		return products.Product{}, apperr.NotFound("Product", id)
	}
	return item, nil
}

func (r *fakeRepository) ByCategory(_ context.Context, category string) ([]products.Product, error) {
	var matches []products.Product
	for _, item := range r.items {
		for _, c := range item.Category {
			if c == category {
				matches = append(matches, item)
				break
			}
		}
	}
	return matches, nil
}

func (r *fakeRepository) Store(_ context.Context, product products.Product) error {
	r.items[product.ID] = product
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.items, id)
	return nil
}

func sampleProduct() products.Product {
	return products.Product{
		ID:          uuid.New(),
		Name:        "iPhone X",
		Category:    []string{"Smart Phone"},
		Description: "a phone",
		ImageFile:   "product-1.png",
		Price:       decimal.NewFromInt(950),
	}
}

func TestCreateAssignsIDAndStoresProduct(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := products.NewCreateProductHandler(repo)

	result, err := handler.Handle(context.Background(), products.CreateProductCommand{
		Name:      "Samsung 10",
		Category:  []string{"Smart Phone"},
		ImageFile: "product-2.png",
		Price:     decimal.NewFromInt(840),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID == uuid.Nil {
		t.Fatal("handler should assign an id so the caller learns it without a round trip")
	}

	stored, err := repo.ByID(context.Background(), result.ID)
	if err != nil {
		t.Fatalf("product was not stored: %v", err)
	}
	if stored.Name != "Samsung 10" {
		t.Errorf("stored name = %q, want %q", stored.Name, "Samsung 10")
	}
}

func TestUpdateMissingProductIsNotFoundRatherThanSilentInsert(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := products.NewUpdateProductHandler(repo)

	_, err := handler.Handle(context.Background(), products.UpdateProductCommand{
		ID:   uuid.New(),
		Name: "ghost",
	})

	// Store upserts, so an unchecked update would resurrect a deleted product.
	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Fatalf("kind = %v, want KindNotFound (error %v)", got, err)
	}
	if len(repo.items) != 0 {
		t.Error("nothing should have been written for a missing product")
	}
}

func TestUpdateReplacesFields(t *testing.T) {
	t.Parallel()

	existing := sampleProduct()
	repo := newFakeRepository(existing)
	handler := products.NewUpdateProductHandler(repo)

	result, err := handler.Handle(context.Background(), products.UpdateProductCommand{
		ID:          existing.ID,
		Name:        "iPhone 15",
		Categories:  []string{"Smart Phone", "Sale"},
		Description: "a newer phone",
		ImageFile:   "product-9.png",
		Price:       decimal.NewFromInt(1200),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsSuccess {
		t.Error("IsSuccess = false, want true")
	}

	updated, err := repo.ByID(context.Background(), existing.ID)
	if err != nil {
		t.Fatalf("loading updated product: %v", err)
	}
	if updated.Name != "iPhone 15" || len(updated.Category) != 2 || !updated.Price.Equal(decimal.NewFromInt(1200)) {
		t.Errorf("updated product = %+v, want the new field values", updated)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	t.Parallel()

	existing := sampleProduct()
	repo := newFakeRepository(existing)
	handler := products.NewDeleteProductHandler(repo)

	for i := range 2 {
		result, err := handler.Handle(context.Background(), products.DeleteProductCommand{ID: existing.ID})
		if err != nil {
			t.Fatalf("delete %d failed: %v", i+1, err)
		}
		if !result.IsSuccess {
			t.Errorf("delete %d reported failure", i+1)
		}
	}

	if len(repo.items) != 0 {
		t.Error("product should be gone")
	}
}

func TestRoutesCoverTheWriteEndpoints(t *testing.T) {
	t.Parallel()

	existing := sampleProduct()
	repo := newFakeRepository(existing)
	router := newRouter(repo)

	t.Run("post creates and points at the new resource", func(t *testing.T) {
		body := `{"name":"Xiaomi Mi 9","category":["Smart Phone"],"description":"d","imageFile":"p.png","price":470}`
		rec := do(t, router, http.MethodPost, "/products", body)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body)
		}

		var response struct {
			ID uuid.UUID `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if want := "/products/" + response.ID.String(); rec.Header().Get("Location") != want {
			t.Errorf("Location = %q, want %q", rec.Header().Get("Location"), want)
		}
	})

	t.Run("put updates through the body id", func(t *testing.T) {
		body := `{"id":"` + existing.ID.String() + `","name":"renamed","categories":["Sale"],"description":"d","imageFile":"p.png","price":1}`
		rec := do(t, router, http.MethodPut, "/products", body)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
		}
		if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte(`"isSuccess":true`)) {
			t.Errorf("body = %s, want isSuccess true", got)
		}
	})

	t.Run("put on a missing product is 404", func(t *testing.T) {
		body := `{"id":"` + uuid.New().String() + `","name":"ghost","categories":[],"description":"","imageFile":"","price":0}`
		rec := do(t, router, http.MethodPut, "/products", body)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("delete answers 200", func(t *testing.T) {
		rec := do(t, router, http.MethodDelete, "/products/"+existing.ID.String(), "")

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("malformed json is a client error, not a 500", func(t *testing.T) {
		rec := do(t, router, http.MethodPost, "/products", `{"name":`)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
		}
	})

	t.Run("malformed uuid in path is a client error", func(t *testing.T) {
		rec := do(t, router, http.MethodDelete, "/products/not-a-uuid", "")

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

func newRouter(repo products.Repository) http.Handler {
	r := chi.NewRouter()
	products.RegisterRoutes(r, repo, slog.New(slog.DiscardHandler))
	return r
}

func do(t *testing.T, router http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, target, reader)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}
