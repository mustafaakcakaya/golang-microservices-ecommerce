package carts

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// DeleteBasketCommand removes a user's basket.
type DeleteBasketCommand struct {
	UserName string `validate:"required"`
}

// DeleteBasketResult reports the outcome.
type DeleteBasketResult struct {
	UserName  string
	IsSuccess bool
}

// DeleteBasketHandler removes the basket through the repository.
type DeleteBasketHandler struct {
	repo Repository
}

// NewDeleteBasketHandler wires the handler.
func NewDeleteBasketHandler(repo Repository) *DeleteBasketHandler {
	return &DeleteBasketHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// Deleting a basket the user does not have succeeds: the caller's intent - the
// basket is gone - holds either way.
func (h *DeleteBasketHandler) Handle(ctx context.Context, c DeleteBasketCommand) (DeleteBasketResult, error) {
	if err := h.repo.Delete(ctx, c.UserName); err != nil {
		return DeleteBasketResult{}, err
	}

	return DeleteBasketResult{UserName: c.UserName, IsSuccess: true}, nil
}

type deleteBasketResponse struct {
	IsSuccess bool `json:"isSuccess"`
}

// DeleteBasketRoute adapts the handler to DELETE /basket/{userName}.
func DeleteBasketRoute(handle cqrs.HandlerFunc[DeleteBasketCommand, DeleteBasketResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := handle(r.Context(), DeleteBasketCommand{UserName: chi.URLParam(r, "userName")})
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, deleteBasketResponse{IsSuccess: result.IsSuccess})
	}
}
