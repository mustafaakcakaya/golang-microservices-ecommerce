package orders

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// UpdateOrderCommand replaces the mutable parts of an existing order.
//
// The lines are not among them: they are set when the order is placed, and
// there is no endpoint that edits them afterwards. The customer is not either -
// an order does not change owner - so both are absent from the command rather
// than accepted and quietly ignored.
type UpdateOrderCommand struct {
	ID              uuid.UUID    `json:"id" validate:"required"`
	OrderName       string       `json:"orderName" validate:"required"`
	ShippingAddress AddressInput `json:"shippingAddress"`
	BillingAddress  AddressInput `json:"billingAddress"`
	Payment         PaymentInput `json:"payment"`
	Status          string       `json:"status" validate:"required"`
}

// UpdateOrderResult reports whether the update was applied.
type UpdateOrderResult struct {
	IsSuccess bool
}

// UpdateOrderHandler applies the update through the repository.
type UpdateOrderHandler struct {
	repo Repository
}

// NewUpdateOrderHandler wires the handler.
func NewUpdateOrderHandler(repo Repository) *UpdateOrderHandler {
	return &UpdateOrderHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// The order is loaded first so updating a missing id answers 404 rather than
// silently creating one: Save upserts, so writing straight away would resurrect
// a deleted order under the caller's id.
func (h *UpdateOrderHandler) Handle(ctx context.Context, c UpdateOrderCommand) (UpdateOrderResult, error) {
	orderID, err := domain.NewOrderID(c.ID)
	if err != nil {
		return UpdateOrderResult{}, clientError(err)
	}

	order, err := h.repo.ByID(ctx, orderID)
	if err != nil {
		return UpdateOrderResult{}, err
	}

	if err := applyUpdate(order, c); err != nil {
		return UpdateOrderResult{}, clientError(err)
	}

	if err := h.repo.Save(ctx, order); err != nil {
		return UpdateOrderResult{}, err
	}

	return UpdateOrderResult{IsSuccess: true}, nil
}

// applyUpdate moves the command's values onto the aggregate, which raises
// OrderUpdated and enforces the rules that guard each of them.
func applyUpdate(order *domain.Order, c UpdateOrderCommand) error {
	orderName, err := domain.NewOrderName(c.OrderName)
	if err != nil {
		return err
	}
	shipping, err := c.ShippingAddress.toDomain()
	if err != nil {
		return fmt.Errorf("shipping address: %w", err)
	}
	billing, err := c.BillingAddress.toDomain()
	if err != nil {
		return fmt.Errorf("billing address: %w", err)
	}
	payment, err := c.Payment.toDomain()
	if err != nil {
		return err
	}
	status, err := domain.ParseOrderStatus(c.Status)
	if err != nil {
		return err
	}

	return order.Update(orderName, shipping, billing, payment, status)
}

// updateOrderRequest is the wire shape of an update call.
type updateOrderRequest struct {
	Order UpdateOrderCommand `json:"order"`
}

type updateOrderResponse struct {
	IsSuccess bool `json:"isSuccess"`
}

// UpdateOrderRoute adapts the handler to PUT /orders.
func UpdateOrderRoute(handle cqrs.HandlerFunc[UpdateOrderCommand, UpdateOrderResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body updateOrderRequest
		if err := httpx.DecodeJSON(w, r, &body); err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		result, err := handle(r.Context(), body.Order)
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, updateOrderResponse(result))
	}
}
