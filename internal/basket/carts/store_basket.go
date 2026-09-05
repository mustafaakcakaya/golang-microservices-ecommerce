package carts

import (
	"context"
	"net/http"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// StoreBasketCommand creates or replaces a user's basket.
type StoreBasketCommand struct {
	Cart ShoppingCart `validate:"required"`
}

// StoreBasketResult echoes whose basket was stored.
type StoreBasketResult struct {
	UserName string
}

// StoreBasketHandler writes the basket through the repository.
type StoreBasketHandler struct {
	repo Repository
}

// NewStoreBasketHandler wires the handler.
func NewStoreBasketHandler(repo Repository) *StoreBasketHandler {
	return &StoreBasketHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// The .NET handler also deducts discounts here by calling the Discount gRPC
// service. That call arrives with the Discount service itself; until then the
// basket is stored with the prices the caller sent.
func (h *StoreBasketHandler) Handle(ctx context.Context, c StoreBasketCommand) (StoreBasketResult, error) {
	if err := h.repo.Store(ctx, c.Cart); err != nil {
		return StoreBasketResult{}, err
	}

	return StoreBasketResult{UserName: c.Cart.UserName}, nil
}

type storeBasketRequest struct {
	Cart ShoppingCart `json:"cart"`
}

type storeBasketResponse struct {
	UserName string `json:"userName"`
}

// StoreBasketRoute adapts the handler to POST /basket.
func StoreBasketRoute(handle cqrs.HandlerFunc[StoreBasketCommand, StoreBasketResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body storeBasketRequest
		if err := httpx.DecodeJSON(w, r, &body); err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		result, err := handle(r.Context(), StoreBasketCommand(body))
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, storeBasketResponse(result))
	}
}
