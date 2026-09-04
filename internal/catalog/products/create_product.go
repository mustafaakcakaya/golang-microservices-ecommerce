package products

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// CreateProductCommand adds a product to the catalog.
type CreateProductCommand struct {
	Name        string
	Category    []string
	Description string
	ImageFile   string
	Price       decimal.Decimal
}

// CreateProductResult carries the id assigned to the new product.
type CreateProductResult struct {
	ID uuid.UUID
}

// CreateProductHandler writes the product through the repository.
type CreateProductHandler struct {
	repo Repository
}

// NewCreateProductHandler wires the handler.
func NewCreateProductHandler(repo Repository) *CreateProductHandler {
	return &CreateProductHandler{repo: repo}
}

// Handle implements cqrs.Handler. The id is generated here rather than by the
// database, so the caller learns it without a round trip - the same effect
// Marten's identity assignment has in the .NET handler.
func (h *CreateProductHandler) Handle(ctx context.Context, c CreateProductCommand) (CreateProductResult, error) {
	product := Product{
		ID:          uuid.New(),
		Name:        c.Name,
		Category:    c.Category,
		Description: c.Description,
		ImageFile:   c.ImageFile,
		Price:       c.Price,
	}

	if err := h.repo.Store(ctx, product); err != nil {
		return CreateProductResult{}, err
	}

	return CreateProductResult{ID: product.ID}, nil
}

// createProductRequest mirrors the .NET CreateProductRequest.
type createProductRequest struct {
	Name        string          `json:"name"`
	Category    []string        `json:"category"`
	Description string          `json:"description"`
	ImageFile   string          `json:"imageFile"`
	Price       decimal.Decimal `json:"price"`
}

type createProductResponse struct {
	ID uuid.UUID `json:"id"`
}

// CreateProductRoute adapts the handler to POST /products.
func CreateProductRoute(handle cqrs.HandlerFunc[CreateProductCommand, CreateProductResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createProductRequest
		if err := httpx.DecodeJSON(w, r, &body); err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		result, err := handle(r.Context(), CreateProductCommand(body))
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		w.Header().Set("Location", fmt.Sprintf("/products/%s", result.ID))
		httpx.JSON(w, http.StatusCreated, createProductResponse(result))
	}
}
