package orders

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
)

// Snapshots records the customers and products an order refers to.
//
// Ordering keeps its own copies so an order stays readable when the catalog
// moves on, and so the order's foreign keys have something to point at.
type Snapshots interface {
	SaveCustomer(ctx context.Context, id domain.CustomerID, name, email string) error
	SaveProduct(ctx context.Context, id domain.ProductID, name string, price decimal.Decimal) error
}

// CheckoutHandler turns a checkout from Basket into an order.
type CheckoutHandler struct {
	repo      Repository
	snapshots Snapshots
}

// NewCheckoutHandler wires the handler.
func NewCheckoutHandler(repo Repository, snapshots Snapshots) *CheckoutHandler {
	return &CheckoutHandler{repo: repo, snapshots: snapshots}
}

// Handle creates an order from one checkout message.
//
// The order is built from the lines the message carries, at the prices the
// customer was shown. The alternative - looking the products up in the catalog
// now - would price the order at whatever the catalog says at the moment it is
// processed, which is not what anyone agreed to.
//
// The message carries no card details, so the order starts out unpaid.
func (h *CheckoutHandler) Handle(ctx context.Context, envelope messaging.Envelope) error {
	var event basketv1.CheckoutRequested
	if err := messaging.Unmarshal(envelope.Payload, &event); err != nil {
		return err
	}

	if len(event.Lines) == 0 {
		return fmt.Errorf("checkout %s carries no lines", envelope.ID)
	}

	customerID, err := domain.NewCustomerID(event.CustomerID)
	if err != nil {
		return err
	}

	// Recorded before the order is saved, because the order's foreign keys
	// refer to them. Both writes are upserts, so a retry is harmless.
	if err := h.snapshots.SaveCustomer(ctx, customerID, customerName(event), event.EmailAddress); err != nil {
		return err
	}

	order, err := h.newOrder(event, customerID)
	if err != nil {
		return err
	}

	for _, line := range event.Lines {
		productID, err := domain.NewProductID(line.ProductID)
		if err != nil {
			return err
		}

		if err := h.snapshots.SaveProduct(ctx, productID, line.ProductName, line.Price.Decimal); err != nil {
			return err
		}
		if err := order.Add(productID, line.Quantity, line.Price.Decimal); err != nil {
			return err
		}
	}

	return h.repo.Save(ctx, order)
}

// newOrder builds the aggregate the checkout describes.
func (h *CheckoutHandler) newOrder(
	event basketv1.CheckoutRequested, customerID domain.CustomerID,
) (*domain.Order, error) {
	orderID, err := domain.NewOrderID(uuid.New())
	if err != nil {
		return nil, err
	}

	orderName, err := domain.NewOrderName(newOrderReference())
	if err != nil {
		return nil, err
	}

	// One address: a checkout collects where the order goes, and billing
	// without a payment to bill has nothing else to say.
	address, err := domain.NewAddress(
		event.FirstName, event.LastName, event.EmailAddress,
		event.AddressLine, event.Country, event.State, event.ZipCode,
	)
	if err != nil {
		return nil, err
	}

	return domain.NewUnpaidOrder(orderID, customerID, orderName, address, address), nil
}

// customerName is what to call the buyer, falling back to the name they signed
// in with when they gave no other.
func customerName(event basketv1.CheckoutRequested) string {
	full := strings.TrimSpace(event.FirstName + " " + event.LastName)
	if full == "" {
		return event.UserName
	}

	return full
}

// referenceAlphabet excludes letters and digits that are easy to confuse when
// a reference is read aloud or copied from a screen.
const referenceAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newOrderReference makes a short human-readable order name.
//
// The domain fixes the length at five characters, so a user name cannot be
// used: most are the wrong length, and two customers would collide. Five
// characters of this alphabet is about 33 million references, and the name is
// a label rather than a key - an order is identified by its id.
func newOrderReference() string {
	const length = 5

	// crypto/rand, so references do not follow a predictable sequence that
	// would let one customer guess another's. It fills the slice completely
	// and never fails.
	reference := make([]byte, length)
	rand.Read(reference)

	for i, b := range reference {
		// The alphabet has exactly 32 characters and 256 divides by 32, so
		// taking the remainder keeps every character equally likely.
		reference[i] = referenceAlphabet[b%byte(len(referenceAlphabet))]
	}

	return string(reference)
}
