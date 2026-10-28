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

// GetOrderByIDQuery fetches one order.
type GetOrderByIDQuery struct {
	ID uuid.UUID
}

// GetOrderByIDResult is the handler output.
type GetOrderByIDResult struct {
	Order OrderView
}

// GetOrderByIDHandler serves the query from the repository.
type GetOrderByIDHandler struct {
	repo Repository
}

// NewGetOrderByIDHandler wires the handler.
func NewGetOrderByIDHandler(repo Repository) *GetOrderByIDHandler {
	return &GetOrderByIDHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// Unlike a list, asking for one order that does not exist is answered with
// apperr.NotFound: the caller named a specific order, and there is none.
func (h *GetOrderByIDHandler) Handle(ctx context.Context, q GetOrderByIDQuery) (GetOrderByIDResult, error) {
	orderID, err := domain.NewOrderID(q.ID)
	if err != nil {
		return GetOrderByIDResult{}, clientError(err)
	}

	order, err := h.repo.ByID(ctx, orderID)
	if err != nil {
		return GetOrderByIDResult{}, err
	}

	return GetOrderByIDResult{Order: viewOf(order)}, nil
}

type getOrderByIDResponse struct {
	Order OrderView `json:"order"`
}

// GetOrderByIDRoute adapts the handler to GET /orders/{id}.
func GetOrderByIDRoute(handle cqrs.HandlerFunc[GetOrderByIDQuery, GetOrderByIDResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httpx.Error(r.Context(), w, log, apperr.BadRequest("order id must be a UUID"))
			return
		}

		result, err := handle(r.Context(), GetOrderByIDQuery{ID: id})
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, getOrderByIDResponse(result))
	}
}
