package carts

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
)

// CheckoutBasketCommand turns a stored basket into a checkout.
//
// It carries no card details. The card goes to the Payment service, which
// keeps it and hands back a token; only the token and the last four digits
// come here. Neither the lines nor the total are part of the command either:
// they are read from the stored basket, so a caller cannot check out something
// other than what the basket holds.
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

	// PaymentToken is what the Payment service issued for the card.
	PaymentToken  string `json:"paymentToken" validate:"required"`
	CardLast4     string `json:"cardLast4" validate:"required,len=4,numeric"`
	PaymentMethod int    `json:"paymentMethod"`
}

// CheckoutBasketResult reports whether the checkout was accepted.
type CheckoutBasketResult struct {
	IsSuccess bool
}

// CheckoutBasketHandler records the checkout and clears the basket.
type CheckoutBasketHandler struct {
	repo Repository
}

// NewCheckoutBasketHandler wires the handler.
func NewCheckoutBasketHandler(repo Repository) *CheckoutBasketHandler {
	return &CheckoutBasketHandler{repo: repo}
}

// Handle implements cqrs.Handler.
//
// Nothing is published here. The basket is cleared and the announcement is
// recorded in one transaction, and the relay publishes what was recorded: a
// broker outage then delays the announcement instead of failing a checkout
// whose database work was fine.
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

	// A stored basket always has a version, so a missing one means this is
	// not a stored basket any more - a concurrent checkout removed it after it
	// was read. Deriving an id from the empty version would record a second
	// checkout of the same basket under an id nothing deduplicates against.
	if cart.Version == uuid.Nil {
		return CheckoutBasketResult{}, apperr.NotFound("Basket", c.UserName)
	}

	event := checkoutEvent(ctx, c, cart)
	// Derived rather than random: two requests racing on the same basket agree
	// on one id, so a double click records one message and produces one order.
	event.ID = CheckoutID(c.UserName, cart.Version)

	// Keyed by the user: a basket is identified by its owner, and a broker
	// that partitions keeps one user's checkouts in the order they happened.
	announcement, err := messaging.NewEnvelope(event, c.UserName)
	if err != nil {
		return CheckoutBasketResult{}, err
	}

	if err := h.repo.Checkout(ctx, c.UserName, announcement); err != nil {
		return CheckoutBasketResult{}, err
	}

	return CheckoutBasketResult{IsSuccess: true}, nil
}

// checkoutNamespace names the ids derived below. It is fixed: changing it
// would give every basket a new checkout id and break the deduplication it
// exists for.
var checkoutNamespace = uuid.MustParse("3b7f0c52-9a1e-4d63-8f2b-6c4e1a0d9b75")

// CheckoutID derives a checkout's message id from the user and the exact
// basket being checked out.
//
// The id travels to the broker and is what a consumer deduplicates on, so it
// has to be the same for two attempts at the same basket and different for a
// basket that was filled again afterwards.
func CheckoutID(userName string, basketVersion uuid.UUID) uuid.UUID {
	name := userName + ":" + strings.ReplaceAll(basketVersion.String(), "-", "")

	return uuid.NewSHA1(checkoutNamespace, []byte(name))
}

// checkoutEvent maps the command and the stored basket onto the contract.
//
// The prices come from the basket rather than from the catalog: they are what
// the customer was shown, discounts already applied, and an order records what
// was charged at the time.
func checkoutEvent(ctx context.Context, c CheckoutBasketCommand, cart ShoppingCart) *basketv1.CheckoutSubmitted {
	items := make([]basketv1.Line, 0, len(cart.Items))
	for _, item := range cart.Items {
		items = append(items, basketv1.Line{
			ProductID:   item.ProductID,
			ProductName: item.ProductName,
			Quantity:    item.Quantity,
			UnitPrice:   messaging.NewMoney(item.Price),
		})
	}

	// One address: the checkout collects where the order goes and bills it
	// there too.
	address := basketv1.Address{
		FirstName:    c.FirstName,
		LastName:     c.LastName,
		EmailAddress: c.EmailAddress,
		AddressLine:  c.AddressLine,
		Country:      c.Country,
		State:        c.State,
		ZipCode:      c.ZipCode,
	}

	return &basketv1.CheckoutSubmitted{
		// The same id the error responses report as their trace id, so a
		// request and the message it produced can be lined up afterwards.
		Header:          messaging.NewHeader(httpx.CorrelationID(ctx)),
		UserName:        c.UserName,
		CustomerID:      c.CustomerID,
		TotalPrice:      messaging.NewMoney(cart.TotalPrice()),
		Items:           items,
		ShippingAddress: address,
		BillingAddress:  address,
		Payment: basketv1.Payment{
			PaymentToken:  c.PaymentToken,
			CardLast4:     c.CardLast4,
			PaymentMethod: c.PaymentMethod,
		},
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
