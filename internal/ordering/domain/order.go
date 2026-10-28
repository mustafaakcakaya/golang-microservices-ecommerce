package domain

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OrderItem is one line of an order.
type OrderItem struct {
	Audit

	ID        OrderItemID
	OrderID   OrderID
	ProductID ProductID
	Quantity  int
	Price     decimal.Decimal
}

// Order is the aggregate root: lines are only reachable through it, so the
// invariants that span them (the total, what may be added) have one owner.
type Order struct {
	aggregate
	Audit

	// Every field is private, and there are getters but no setters. Reading an
	// order is nobody's business but their own; changing one has to go through
	// Update, Add or Remove, which is where the rules live and where the
	// events are raised. A setter would be a way around both.
	id              OrderID
	customerID      CustomerID
	shippingAddress Address
	billingAddress  Address
	payment         Payment
	status          OrderStatus

	items []OrderItem
}

// ID identifies the order.
func (o *Order) ID() OrderID { return o.id }

// CustomerID is who placed it.
func (o *Order) CustomerID() CustomerID { return o.customerID }

// ShippingAddress is where it goes.
func (o *Order) ShippingAddress() Address { return o.shippingAddress }

// BillingAddress is where it is billed.
func (o *Order) BillingAddress() Address { return o.billingAddress }

// Payment is how it was paid for, which is the zero value until it is.
func (o *Order) Payment() Payment { return o.payment }

// Status is where it sits in its lifecycle.
func (o *Order) Status() OrderStatus { return o.status }

// NewUnpaidOrder creates an order that has not been paid for yet.
//
// It starts as a draft rather than pending: nothing should act on it as a
// confirmed order until a payment is recorded against it.
func NewUnpaidOrder(
	id OrderID,
	customerID CustomerID,
	shippingAddress Address,
	billingAddress Address,
) *Order {
	order := newOrder(id, customerID, shippingAddress, billingAddress, Payment{}, OrderStatusDraft)

	order.raise(OrderCreated{Order: order})

	return order
}

// NewOrder creates an order and raises OrderCreated.
func NewOrder(
	id OrderID,
	customerID CustomerID,
	shippingAddress Address,
	billingAddress Address,
	payment Payment,
) *Order {
	order := newOrder(id, customerID, shippingAddress, billingAddress, payment, OrderStatusPending)

	order.raise(OrderCreated{Order: order})

	return order
}

// newOrder builds the aggregate without raising anything, so the constructors
// above differ only in what they mean rather than in how they assemble.
func newOrder(
	id OrderID,
	customerID CustomerID,
	shippingAddress Address,
	billingAddress Address,
	payment Payment,
	status OrderStatus,
) *Order {
	return &Order{
		id:              id,
		customerID:      customerID,
		shippingAddress: shippingAddress,
		billingAddress:  billingAddress,
		payment:         payment,
		status:          status,
	}
}

// String renders the order without its payment details.
//
// It is not decoration. Formatting a struct walks into every field, and fmt
// only asks an exported field to render itself - an unexported one is printed
// raw, Stringer or not. Making the aggregate render itself is what stops a
// %+v of an order from printing the payment token the payment hides.
//
// The receiver is a value so that both an Order and an *Order format safely;
// with a pointer receiver, printing a copy would fall back to walking fields.
func (o Order) String() string {
	return fmt.Sprintf("Order{id: %s, status: %s, lines: %d, total: %s, payment: %v}",
		o.id, o.status, len(o.items), o.TotalPrice(), o.payment)
}

// Items returns the lines. The slice is copied so a caller cannot reach past
// the aggregate and change a line without going through it.
func (o *Order) Items() []OrderItem {
	items := make([]OrderItem, len(o.items))
	copy(items, o.items)
	return items
}

// TotalPrice sums the lines. It is derived rather than stored, so it can never
// disagree with the lines it summarises.
func (o *Order) TotalPrice() decimal.Decimal {
	total := decimal.Zero
	for _, item := range o.items {
		total = total.Add(item.Price.Mul(decimal.NewFromInt(int64(item.Quantity))))
	}
	return total
}

// Update replaces the mutable parts of the order and raises OrderUpdated.
func (o *Order) Update(
	shippingAddress Address,
	billingAddress Address,
	payment Payment,
	status OrderStatus,
) error {
	if !status.IsValid() {
		return invalidf("unknown order status %d", int(status))
	}

	o.shippingAddress = shippingAddress
	o.billingAddress = billingAddress
	o.payment = payment
	o.status = status

	o.raise(OrderUpdated{Order: o})

	return nil
}

// Add appends a line.
func (o *Order) Add(productID ProductID, quantity int, price decimal.Decimal) error {
	if quantity <= 0 {
		return invalidf("Quantity must be greater than zero")
	}
	if price.LessThanOrEqual(decimal.Zero) {
		return invalidf("Price must be greater than zero")
	}

	o.items = append(o.items, OrderItem{
		ID:        OrderItemID(uuid.New()),
		OrderID:   o.id,
		ProductID: productID,
		Quantity:  quantity,
		Price:     price,
	})

	return nil
}

// Remove drops every line for a product. Removing a product the order does not
// contain is not an error: the caller's intent - the product is not on the
// order - holds either way.
func (o *Order) Remove(productID ProductID) {
	kept := o.items[:0]
	for _, item := range o.items {
		if item.ProductID != productID {
			kept = append(kept, item)
		}
	}
	o.items = kept
}

// Restore rebuilds an order from storage without raising events.
//
// Loading is not a domain change, so replaying OrderCreated here would publish
// an event for something that happened long ago.
func Restore(
	id OrderID,
	customerID CustomerID,
	shippingAddress Address,
	billingAddress Address,
	payment Payment,
	status OrderStatus,
	items []OrderItem,
) *Order {
	order := newOrder(id, customerID, shippingAddress, billingAddress, payment, status)

	order.items = make([]OrderItem, len(items))
	copy(order.items, items)

	return order
}
