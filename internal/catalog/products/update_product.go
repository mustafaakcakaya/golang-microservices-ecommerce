package products

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// UpdateProductCommand replaces a product's fields.
type UpdateProductCommand struct {
	ID          uuid.UUID `validate:"required"`
	Name        string    `validate:"required,min=2,max=150"`
	Categories  []string
	Description string
	ImageFile   string
	Price       decimal.Decimal `validate:"gt=0"`
}

// UpdateProductResult reports whether the update was applied.
type UpdateProductResult struct {
	IsSuccess bool
}

// UpdateProductHandler applies the update through the repository.
type UpdateProductHandler struct {
	repo Repository
}

// NewUpdateProductHandler wires the handler.
func NewUpdateProductHandler(repo Repository) *UpdateProductHandler {
	return &UpdateProductHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// The product is loaded first so updating a missing id is a 404 rather than a
// silent insert: Store upserts, so writing straight away would resurrect a
// deleted product. The .NET handler loads for the same reason.
func (h *UpdateProductHandler) Handle(ctx context.Context, c UpdateProductCommand) (UpdateProductResult, error) {
	product, err := h.repo.ByID(ctx, c.ID)
	if err != nil {
		return UpdateProductResult{}, err
	}

	product.Name = c.Name
	product.Category = c.Categories
	product.Description = c.Description
	product.ImageFile = c.ImageFile
	product.Price = c.Price

	if err := h.repo.Store(ctx, product); err != nil {
		return UpdateProductResult{}, err
	}

	return UpdateProductResult{IsSuccess: true}, nil
}

// updateProductRequest mirrors the .NET UpdateProductRequest, which carries the
// id in the body rather than the path.
type updateProductRequest struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Categories  []string        `json:"categories"`
	Description string          `json:"description"`
	ImageFile   string          `json:"imageFile"`
	Price       decimal.Decimal `json:"price"`
}

type updateProductResponse struct {
	IsSuccess bool `json:"isSuccess"`
}

// UpdateProductRoute adapts the handler to PUT /products.
func UpdateProductRoute(handle cqrs.HandlerFunc[UpdateProductCommand, UpdateProductResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body updateProductRequest
		if err := httpx.DecodeJSON(w, r, &body); err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		result, err := handle(r.Context(), UpdateProductCommand(body))
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, updateProductResponse(result))
	}
}
