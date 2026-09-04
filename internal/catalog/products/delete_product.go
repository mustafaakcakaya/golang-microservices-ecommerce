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

// DeleteProductCommand removes a product from the catalog.
type DeleteProductCommand struct {
	ID uuid.UUID
}

// DeleteProductResult reports whether the delete was applied.
type DeleteProductResult struct {
	IsSuccess bool
}

// DeleteProductHandler removes the product through the repository.
type DeleteProductHandler struct {
	repo Repository
}

// NewDeleteProductHandler wires the handler.
func NewDeleteProductHandler(repo Repository) *DeleteProductHandler {
	return &DeleteProductHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// Deleting an unknown id succeeds, as it does in the .NET handler: Marten's
// Delete does not check existence first. The outcome the caller cares about -
// the product is gone - holds either way.
func (h *DeleteProductHandler) Handle(ctx context.Context, c DeleteProductCommand) (DeleteProductResult, error) {
	if err := h.repo.Delete(ctx, c.ID); err != nil {
		return DeleteProductResult{}, err
	}

	return DeleteProductResult{IsSuccess: true}, nil
}

type deleteProductResponse struct {
	IsSuccess bool `json:"isSuccess"`
}

// DeleteProductRoute adapts the handler to DELETE /products/{id}.
func DeleteProductRoute(handle cqrs.HandlerFunc[DeleteProductCommand, DeleteProductResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httpx.Error(r.Context(), w, log, apperr.BadRequest("product id must be a UUID"))
			return
		}

		result, err := handle(r.Context(), DeleteProductCommand{ID: id})
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, deleteProductResponse(result))
	}
}
