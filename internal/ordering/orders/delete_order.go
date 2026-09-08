package orders

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// DeleteOrderCommand removes an order.
type DeleteOrderCommand struct {
	ID uuid.UUID `validate:"required"`
}

// DeleteOrderResult reports whether the delete was applied.
type DeleteOrderResult struct {
	IsSuccess bool
}

// DeleteOrderHandler removes the order through the repository.
type DeleteOrderHandler struct {
	repo Repository
}

// NewDeleteOrderHandler wires the handler.
func NewDeleteOrderHandler(repo Repository) *DeleteOrderHandler {
	return &DeleteOrderHandler{repo: repo}
}

// Handle implements cqrs.Handler. Deleting an unknown order reports 404; see
// Repository.Delete for why this differs from the catalog.
func (h *DeleteOrderHandler) Handle(ctx context.Context, c DeleteOrderCommand) (DeleteOrderResult, error) {
	orderID, err := domain.NewOrderID(c.ID)
	if err != nil {
		return DeleteOrderResult{}, clientError(err)
	}

	if err := h.repo.Delete(ctx, orderID); err != nil {
		return DeleteOrderResult{}, err
	}

	return DeleteOrderResult{IsSuccess: true}, nil
}

type deleteOrderResponse struct {
	IsSuccess bool `json:"isSuccess"`
}

// DeleteOrderRoute adapts the handler to DELETE /orders/{id}.
func DeleteOrderRoute(handle cqrs.HandlerFunc[DeleteOrderCommand, DeleteOrderResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httpx.Error(r.Context(), w, log, apperr.BadRequest("order id must be a UUID"))
			return
		}

		result, err := handle(r.Context(), DeleteOrderCommand{ID: id})
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, deleteOrderResponse(result))
	}
}
