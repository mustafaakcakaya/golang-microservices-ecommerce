package carts

import (
	"context"
	"net/http"

	"github.com/shopspring/decimal"

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

// StoreBasketHandler applies discounts and writes the basket.
type StoreBasketHandler struct {
	repo      Repository
	discounts DiscountLookup
}

// NewStoreBasketHandler wires the handler.
func NewStoreBasketHandler(repo Repository, discounts DiscountLookup) *StoreBasketHandler {
	return &StoreBasketHandler{repo: repo, discounts: discounts}
}

// Handle implements cqrs.Handler.
//
// Prices are discounted before the basket is stored, so the stored basket is
// what the customer will be charged - the same order the .NET handler uses.
//
// A lookup failure fails the whole request rather than storing undiscounted
// prices. That couples Basket's availability to Discount, which is the .NET
// behaviour and the safer of the two: silently charging full price is worse
// than asking the caller to retry.
func (h *StoreBasketHandler) Handle(ctx context.Context, c StoreBasketCommand) (StoreBasketResult, error) {
	cart, err := h.applyDiscounts(ctx, c.Cart)
	if err != nil {
		return StoreBasketResult{}, err
	}

	if err := h.repo.Store(ctx, cart); err != nil {
		return StoreBasketResult{}, err
	}

	return StoreBasketResult{UserName: cart.UserName}, nil
}

// applyDiscounts subtracts each product's coupon from its line price.
//
// One call per line, as in the .NET handler: the contract offers no batch RPC,
// so a basket of n products costs n round trips.
func (h *StoreBasketHandler) applyDiscounts(ctx context.Context, cart ShoppingCart) (ShoppingCart, error) {
	discounted := make([]ShoppingCartItem, len(cart.Items))
	copy(discounted, cart.Items)

	for i := range discounted {
		amount, err := h.discounts.AmountFor(ctx, discounted[i].ProductName)
		if err != nil {
			return ShoppingCart{}, err
		}

		price := discounted[i].Price.Sub(decimal.NewFromInt(int64(amount)))
		// A coupon worth more than the product would otherwise store a negative
		// price and a negative basket total. The .NET handler subtracts without
		// this guard.
		if price.IsNegative() {
			price = decimal.Zero
		}

		discounted[i].Price = price
	}

	cart.Items = discounted

	return cart, nil
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
