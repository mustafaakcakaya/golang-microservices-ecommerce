package carts

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
)

// Announcer publishes a checkout so other services can act on it.
//
// Narrower than the full publisher on purpose: this package sends and never
// receives, and the interface says so.
type Announcer interface {
	Publish(ctx context.Context, envelope messaging.Envelope) error
}

// CheckoutBasketCommand turns a stored basket into a checkout.
//
// It asks for no card details. The checkout message does not carry them and an
// order starts out unpaid, so collecting them here would mean holding card data
// for no purpose - which is worse than not having it.
type CheckoutBasketCommand struct {
	UserName   string    `json:"userName" validate:"required"`
	CustomerID uuid.UUID `json:"customerId" validate:"required"`

	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	EmailAddress string `json:"emailAddress" validate:"required,email"`
	AddressLine  string `json:"addressLine" validate:"required"`
	Country      string `json:"country"`
	State        string `json:"state"`
	ZipCode      string `json:"zipCode"`
}

// CheckoutBasketResult reports whether the checkout was accepted.
type CheckoutBasketResult struct {
	IsSuccess bool
}

// CheckoutBasketHandler announces the checkout and clears the basket.
type CheckoutBasketHandler struct {
	repo      Repository
	announcer Announcer
}

// NewCheckoutBasketHandler wires the handler.
func NewCheckoutBasketHandler(repo Repository, announcer Announcer) *CheckoutBasketHandler {
	return &CheckoutBasketHandler{repo: repo, announcer: announcer}
}

// Handle implements cqrs.Handler.
//
// TODO: move this to the outbox pattern, the way Ordering already publishes.
// Announcing and clearing the basket are two writes to two systems with no
// transaction around them, which is the problem the outbox exists to solve:
// if the delete fails after the message went out, the basket survives and a
// retry publishes a second checkout under a new message id - one the consumer's
// inbox cannot recognise as a duplicate, so the customer gets two orders. The
// order here is deliberate: publishing first risks that duplicate, while
// deleting first would risk losing the checkout entirely.
func (h *CheckoutBasketHandler) Handle(
	ctx context.Context, c CheckoutBasketCommand,
) (CheckoutBasketResult, error) {
	cart, err := h.repo.GetByUserName(ctx, c.UserName)
	if err != nil {
		return CheckoutBasketResult{}, err
	}

	// An order with no lines has nothing to fulfil and a total of zero.
	if len(cart.Items) == 0 {
		return CheckoutBasketResult{}, apperr.BadRequest("the basket of %s is empty", c.UserName)
	}

	envelope, err := messaging.NewEnvelope(checkoutEvent(ctx, c, cart), c.CustomerID.String())
	if err != nil {
		return CheckoutBasketResult{}, err
	}

	if err := h.announcer.Publish(ctx, envelope); err != nil {
		return CheckoutBasketResult{}, err
	}

	if err := h.repo.Delete(ctx, c.UserName); err != nil {
		return CheckoutBasketResult{}, err
	}

	return CheckoutBasketResult{IsSuccess: true}, nil
}

// checkoutEvent maps the command and the stored basket onto the contract.
//
// The prices come from the basket rather than from the catalog: they are what
// the customer was shown, discounts already applied, and an order records what
// was charged at the time.
func checkoutEvent(ctx context.Context, c CheckoutBasketCommand, cart ShoppingCart) *basketv1.CheckoutRequested {
	lines := make([]basketv1.Line, 0, len(cart.Items))
	for _, item := range cart.Items {
		lines = append(lines, basketv1.Line{
			ProductID:   item.ProductID,
			ProductName: item.ProductName,
			Quantity:    item.Quantity,
			Price:       messaging.NewMoney(item.Price),
		})
	}

	return &basketv1.CheckoutRequested{
		// The same id the error responses report as their trace id, so a
		// request and the message it produced can be lined up afterwards.
		Header:       messaging.NewHeader(httpx.CorrelationID(ctx)),
		UserName:     c.UserName,
		CustomerID:   c.CustomerID,
		FirstName:    c.FirstName,
		LastName:     c.LastName,
		EmailAddress: c.EmailAddress,
		AddressLine:  c.AddressLine,
		Country:      c.Country,
		State:        c.State,
		ZipCode:      c.ZipCode,
		TotalPrice:   messaging.NewMoney(cart.TotalPrice()),
		Lines:        lines,
	}
}

// checkoutBasketRequest is the wire shape of a checkout call.
type checkoutBasketRequest struct {
	Checkout CheckoutBasketCommand `json:"checkout"`
}

type checkoutBasketResponse struct {
	IsSuccess bool `json:"isSuccess"`
}

// CheckoutBasketRoute adapts the handler to POST /basket/checkout.
func CheckoutBasketRoute(
	handle cqrs.HandlerFunc[CheckoutBasketCommand, CheckoutBasketResult], log routeLogger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body checkoutBasketRequest
		if err := httpx.DecodeJSON(w, r, &body); err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		result, err := handle(r.Context(), body.Checkout)
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, checkoutBasketResponse(result))
	}
}
