package orders

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// GetOrdersByNameQuery searches orders by name.
type GetOrdersByNameQuery struct {
	Name string
}

// GetOrdersByNameResult is the handler output.
type GetOrdersByNameResult struct {
	Orders []OrderView
}

// GetOrdersByNameHandler serves the query from the repository.
type GetOrdersByNameHandler struct {
	repo Repository
}

// NewGetOrdersByNameHandler wires the handler.
func NewGetOrdersByNameHandler(repo Repository) *GetOrdersByNameHandler {
	return &GetOrdersByNameHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// A search that matches nothing is an empty list, not a 404: the caller asked
// what matches, and "nothing" is a valid answer to that question.
func (h *GetOrdersByNameHandler) Handle(ctx context.Context, q GetOrdersByNameQuery) (GetOrdersByNameResult, error) {
	list, err := h.repo.ByName(ctx, q.Name)
	if err != nil {
		return GetOrdersByNameResult{}, err
	}

	return GetOrdersByNameResult{Orders: viewsOf(list)}, nil
}

type getOrdersByNameResponse struct {
	Orders []OrderView `json:"orders"`
}

// GetOrdersByNameRoute adapts the handler to GET /orders/{orderName}.
func GetOrdersByNameRoute(handle cqrs.HandlerFunc[GetOrdersByNameQuery, GetOrdersByNameResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "orderName")
		if name == "" {
			httpx.Error(r.Context(), w, log, apperr.BadRequest("order name is required"))
			return
		}

		result, err := handle(r.Context(), GetOrdersByNameQuery{Name: name})
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, getOrdersByNameResponse(result))
	}
}
