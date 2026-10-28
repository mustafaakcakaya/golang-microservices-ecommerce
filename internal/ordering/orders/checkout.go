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

// CheckoutHandler turns a checkout from Basket into an order.
type CheckoutHandler struct {
	repo Repository
}

// NewCheckoutHandler wires the handler.
func NewCheckoutHandler(repo Repository) *CheckoutHandler {
	return &CheckoutHandler{repo: repo}
}

// Handle creates an order from one checkout message.
//
// The order is built from the items the message carries, at the prices the
// customer was shown. The alternative - looking the products up in the catalog
// now - would price the order at whatever the catalog says at the moment it is
// processed, which is not what anyone agreed to.
//
// A message that contradicts itself is rejected rather than retried: no number
// of redeliveries will make a total match lines it does not match, and a
// message retried forever holds up every checkout behind it. Failures of this
// service's own - the database being down - are returned as they are, so the
// message comes back once it can be handled.
func (h *CheckoutHandler) Handle(ctx context.Context, envelope messaging.Envelope) error {
	var event basketv1.CheckoutSubmitted
	if err := messaging.Unmarshal(envelope.Payload, &event); err != nil {
		return messaging.Reject(err)
	}

	order, customer, products, err := h.orderFrom(event)
	if err != nil {
		return messaging.Reject(fmt.Errorf("checkout %s: %w", envelope.ID, err))
	}

	return h.repo.Place(ctx, order, customer, products)
}

// orderFrom builds the order the checkout describes, and the customer and
// products it refers to.
func (h *CheckoutHandler) orderFrom(
	event basketv1.CheckoutSubmitted,
) (*domain.Order, *domain.Customer, []*domain.Product, error) {
	if len(event.Items) == 0 {
		return nil, nil, nil, fmt.Errorf("it carries no items")
	}

	// Recomputed rather than trusted. A total that does not match its lines
	// means Basket and this service disagree about the order, and guessing
	// which side is right would hide that.
	if linesTotal := totalOf(event.Items); !linesTotal.Equal(event.TotalPrice.Decimal) {
		return nil, nil, nil, fmt.Errorf("its total %s does not match its items (%s)",
			event.TotalPrice.Decimal, linesTotal)
	}

	customerID, err := domain.NewCustomerID(event.CustomerID)
	if err != nil {
		return nil, nil, nil, err
	}
	customer, err := domain.NewCustomer(customerID, customerName(event), event.ShippingAddress.EmailAddress)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("customer: %w", err)
	}

	order, err := h.newOrder(event, customerID)
	if err != nil {
		return nil, nil, nil, err
	}

	products := make([]*domain.Product, 0, len(event.Items))
	for _, line := range event.Items {
		productID, err := domain.NewProductID(line.ProductID)
		if err != nil {
			return nil, nil, nil, err
		}

		product, err := domain.NewProduct(productID, line.ProductName, line.UnitPrice.Decimal)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("product %s: %w", productID, err)
		}
		products = append(products, product)

		if err := order.Add(productID, line.Quantity, line.UnitPrice.Decimal); err != nil {
			return nil, nil, nil, err
		}
	}

	return order, customer, products, nil
}

// newOrder builds the aggregate the checkout describes.
func (h *CheckoutHandler) newOrder(
	event basketv1.CheckoutSubmitted, customerID domain.CustomerID,
) (*domain.Order, error) {
	orderID, err := domain.NewOrderID(uuid.New())
	if err != nil {
		return nil, err
	}

	orderName, err := domain.NewOrderName(newOrderReference())
	if err != nil {
		return nil, err
	}

	shipping, err := addressOf(event.ShippingAddress)
	if err != nil {
		return nil, fmt.Errorf("shipping address: %w", err)
	}
	billing, err := addressOf(event.BillingAddress)
	if err != nil {
		return nil, fmt.Errorf("billing address: %w", err)
	}

	payment, err := domain.NewPayment(
		event.Payment.PaymentToken, event.Payment.CardLast4, event.Payment.PaymentMethod)
	if err != nil {
		return nil, fmt.Errorf("payment: %w", err)
	}

	return domain.NewOrder(orderID, customerID, orderName, shipping, billing, payment), nil
}

func addressOf(address basketv1.Address) (domain.Address, error) {
	return domain.NewAddress(
		address.FirstName, address.LastName, address.EmailAddress,
		address.AddressLine, address.Country, address.State, address.ZipCode,
	)
}

// totalOf sums the items the way an order sums its lines.
func totalOf(items []basketv1.Line) decimal.Decimal {
	total := decimal.Zero
	for _, line := range items {
		total = total.Add(line.UnitPrice.Mul(decimal.NewFromInt(int64(line.Quantity))))
	}
	return total
}

// customerName is what to call the buyer, falling back to the name they signed
// in with when they gave no other.
func customerName(event basketv1.CheckoutSubmitted) string {
	full := strings.TrimSpace(event.ShippingAddress.FirstName + " " + event.ShippingAddress.LastName)
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
