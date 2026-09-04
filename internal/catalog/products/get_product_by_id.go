package products

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// GetProductByIDQuery loads a single product.
type GetProductByIDQuery struct {
	ID uuid.UUID
}

// GetProductByIDResult is the handler output.
type GetProductByIDResult struct {
	Product Product
}

// GetProductByIDHandler serves the query from the repository.
type GetProductByIDHandler struct {
	repo Repository
}

// NewGetProductByIDHandler wires the handler.
func NewGetProductByIDHandler(repo Repository) *GetProductByIDHandler {
	return &GetProductByIDHandler{repo: repo}
}

// Handle implements cqrs.Handler. A missing product surfaces as the
// repository's apperr.NotFound, the counterpart of ProductNotFoundException.
func (h *GetProductByIDHandler) Handle(ctx context.Context, q GetProductByIDQuery) (GetProductByIDResult, error) {
	product, err := h.repo.ByID(ctx, q.ID)
	if err != nil {
		return GetProductByIDResult{}, err
	}

	return GetProductByIDResult{Product: product}, nil
}

type getProductByIDResponse struct {
	Product Product `json:"product"`
}

// GetProductByIDRoute adapts the handler to GET /products/{id}.
func GetProductByIDRoute(handle cqrs.HandlerFunc[GetProductByIDQuery, GetProductByIDResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httpx.Error(r.Context(), w, log, apperr.BadRequest("product id must be a UUID"))
			return
		}

		result, err := handle(r.Context(), GetProductByIDQuery{ID: id})
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, getProductByIDResponse(result))
	}
}
