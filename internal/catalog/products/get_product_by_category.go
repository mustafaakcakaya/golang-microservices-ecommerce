package products

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// GetProductByCategoryQuery lists products tagged with a category.
type GetProductByCategoryQuery struct {
	Category string
}

// GetProductByCategoryResult is the handler output.
type GetProductByCategoryResult struct {
	Products []Product
}

// GetProductByCategoryHandler serves the query from the repository.
type GetProductByCategoryHandler struct {
	repo Repository
}

// NewGetProductByCategoryHandler wires the handler.
func NewGetProductByCategoryHandler(repo Repository) *GetProductByCategoryHandler {
	return &GetProductByCategoryHandler{repo: repo}
}

// Handle implements cqrs.Handler.
func (h *GetProductByCategoryHandler) Handle(ctx context.Context, q GetProductByCategoryQuery) (GetProductByCategoryResult, error) {
	items, err := h.repo.ByCategory(ctx, q.Category)
	if err != nil {
		return GetProductByCategoryResult{}, err
	}

	return GetProductByCategoryResult{Products: items}, nil
}

type getProductByCategoryResponse struct {
	Products []Product `json:"products"`
}

// GetProductByCategoryRoute adapts the handler to GET /products/category/{category}.
func GetProductByCategoryRoute(handle cqrs.HandlerFunc[GetProductByCategoryQuery, GetProductByCategoryResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := GetProductByCategoryQuery{Category: chi.URLParam(r, "category")}

		result, err := handle(r.Context(), query)
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, getProductByCategoryResponse(result))
	}
}
