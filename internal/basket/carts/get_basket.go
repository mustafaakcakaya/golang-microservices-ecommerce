package carts

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// GetBasketQuery reads one user's basket.
type GetBasketQuery struct {
	UserName string
}

// GetBasketResult carries the basket.
type GetBasketResult struct {
	Cart ShoppingCart
}

// GetBasketHandler reads through the repository.
type GetBasketHandler struct {
	repo Repository
}

// NewGetBasketHandler wires the handler.
func NewGetBasketHandler(repo Repository) *GetBasketHandler {
	return &GetBasketHandler{repo: repo}
}

// Handle implements cqrs.Handler.
func (h *GetBasketHandler) Handle(ctx context.Context, q GetBasketQuery) (GetBasketResult, error) {
	cart, err := h.repo.GetByUserName(ctx, q.UserName)
	if err != nil {
		return GetBasketResult{}, err
	}

	return GetBasketResult{Cart: cart}, nil
}

type getBasketResponse struct {
	Cart ShoppingCart `json:"cart"`
}

// GetBasketRoute adapts the handler to GET /basket/{userName}.
func GetBasketRoute(handle cqrs.HandlerFunc[GetBasketQuery, GetBasketResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := handle(r.Context(), GetBasketQuery{UserName: chi.URLParam(r, "userName")})
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, getBasketResponse(result))
	}
}
