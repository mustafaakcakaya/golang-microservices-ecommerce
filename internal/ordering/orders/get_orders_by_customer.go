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

// GetOrdersByCustomerQuery lists the orders of one customer.
type GetOrdersByCustomerQuery struct {
	CustomerID uuid.UUID
}

// GetOrdersByCustomerResult is the handler output.
type GetOrdersByCustomerResult struct {
	Orders []OrderView
}

// GetOrdersByCustomerHandler serves the query from the repository.
type GetOrdersByCustomerHandler struct {
	repo Repository
}

// NewGetOrdersByCustomerHandler wires the handler.
func NewGetOrdersByCustomerHandler(repo Repository) *GetOrdersByCustomerHandler {
	return &GetOrdersByCustomerHandler{repo: repo}
}

// Handle implements cqrs.Handler.
func (h *GetOrdersByCustomerHandler) Handle(ctx context.Context, q GetOrdersByCustomerQuery) (GetOrdersByCustomerResult, error) {
	customerID, err := domain.NewCustomerID(q.CustomerID)
	if err != nil {
		return GetOrdersByCustomerResult{}, clientError(err)
	}

	list, err := h.repo.ByCustomer(ctx, customerID)
	if err != nil {
		return GetOrdersByCustomerResult{}, err
	}

	return GetOrdersByCustomerResult{Orders: viewsOf(list)}, nil
}

type getOrdersByCustomerResponse struct {
	Orders []OrderView `json:"orders"`
}

// GetOrdersByCustomerRoute adapts the handler to GET /orders/customer/{customerId}.
func GetOrdersByCustomerRoute(handle cqrs.HandlerFunc[GetOrdersByCustomerQuery, GetOrdersByCustomerResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "customerId"))
		if err != nil {
			httpx.Error(r.Context(), w, log, apperr.BadRequest("customer id must be a UUID"))
			return
		}

		result, err := handle(r.Context(), GetOrdersByCustomerQuery{CustomerID: id})
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, getOrdersByCustomerResponse(result))
	}
}
