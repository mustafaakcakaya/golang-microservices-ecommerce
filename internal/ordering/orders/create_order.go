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

// CreateOrderCommand places a new order.
type CreateOrderCommand struct {
	CustomerID      uuid.UUID    `json:"customerId" validate:"required"`
	OrderName       string       `json:"orderName" validate:"required"`
	ShippingAddress AddressInput `json:"shippingAddress"`
	BillingAddress  AddressInput `json:"billingAddress"`
	Payment         PaymentInput `json:"payment"`
	Items           []LineInput  `json:"orderItems" validate:"required,min=1,dive"`
}

// CreateOrderResult carries the id assigned to the new order.
type CreateOrderResult struct {
	ID uuid.UUID
}

// CreateOrderHandler builds the aggregate and saves it.
type CreateOrderHandler struct {
	repo Repository
}

// NewCreateOrderHandler wires the handler.
func NewCreateOrderHandler(repo Repository) *CreateOrderHandler {
	return &CreateOrderHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// Creating the order raises OrderCreated on the aggregate, and nothing here
// publishes it: the event stays put so the repository can write it beside the
// order in the same transaction once the outbox exists. Publishing from the
// handler would mean announcing an order that the commit could still lose.
func (h *CreateOrderHandler) Handle(ctx context.Context, c CreateOrderCommand) (CreateOrderResult, error) {
	order, err := buildOrder(c)
	if err != nil {
		return CreateOrderResult{}, clientError(err)
	}

	if err := h.repo.Save(ctx, order); err != nil {
		return CreateOrderResult{}, err
	}

	return CreateOrderResult{ID: order.ID.UUID()}, nil
}

// buildOrder turns the command into an aggregate.
//
// The id is generated here rather than by the database, so the caller learns it
// without a round trip and the aggregate is complete before it is ever stored.
func buildOrder(c CreateOrderCommand) (*domain.Order, error) {
	orderID, err := domain.NewOrderID(uuid.New())
	if err != nil {
		return nil, err
	}
	customerID, err := domain.NewCustomerID(c.CustomerID)
	if err != nil {
		return nil, err
	}
	orderName, err := domain.NewOrderName(c.OrderName)
	if err != nil {
		return nil, err
	}
	shipping, err := c.ShippingAddress.toDomain()
	if err != nil {
		return nil, fmt.Errorf("shipping address: %w", err)
	}
	billing, err := c.BillingAddress.toDomain()
	if err != nil {
		return nil, fmt.Errorf("billing address: %w", err)
	}
	payment, err := c.Payment.toDomain()
	if err != nil {
		return nil, err
	}

	order := domain.NewOrder(orderID, customerID, orderName, shipping, billing, payment)

	for _, line := range c.Items {
		productID, err := domain.NewProductID(line.ProductID)
		if err != nil {
			return nil, err
		}
		if err := order.Add(productID, line.Quantity, line.Price); err != nil {
			return nil, err
		}
	}

	return order, nil
}

// createOrderRequest is the wire shape of a create call. The order is nested
// under its own key so the envelope has room to grow without breaking clients.
type createOrderRequest struct {
	Order CreateOrderCommand `json:"order"`
}

type createOrderResponse struct {
	ID uuid.UUID `json:"id"`
}

// CreateOrderRoute adapts the handler to POST /orders.
func CreateOrderRoute(handle cqrs.HandlerFunc[CreateOrderCommand, CreateOrderResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createOrderRequest
		if err := httpx.DecodeJSON(w, r, &body); err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		result, err := handle(r.Context(), body.Order)
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		w.Header().Set("Location", fmt.Sprintf("/orders/%s", result.ID))
		httpx.JSON(w, http.StatusCreated, createOrderResponse(result))
	}
}
