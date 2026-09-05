package domain

import (
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

	ID              OrderID
	CustomerID      CustomerID
	OrderName       OrderName
	ShippingAddress Address
	BillingAddress  Address
	Payment         Payment
	Status          OrderStatus

	items []OrderItem
}

// NewOrder creates an order and raises OrderCreated.
func NewOrder(
	id OrderID,
	customerID CustomerID,
	orderName OrderName,
	shippingAddress Address,
	billingAddress Address,
	payment Payment,
) *Order {
	order := &Order{
		ID:              id,
		CustomerID:      customerID,
		OrderName:       orderName,
		ShippingAddress: shippingAddress,
		BillingAddress:  billingAddress,
		Payment:         payment,
		Status:          OrderStatusPending,
	}

	order.raise(OrderCreated{Order: order})

	return order
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
	orderName OrderName,
	shippingAddress Address,
	billingAddress Address,
	payment Payment,
	status OrderStatus,
) error {
	if !status.IsValid() {
		return invalidf("unknown order status %d", int(status))
	}

	o.OrderName = orderName
	o.ShippingAddress = shippingAddress
	o.BillingAddress = billingAddress
	o.Payment = payment
	o.Status = status

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
		OrderID:   o.ID,
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
	orderName OrderName,
	shippingAddress Address,
	billingAddress Address,
	payment Payment,
	status OrderStatus,
	items []OrderItem,
) *Order {
	order := &Order{
		ID:              id,
		CustomerID:      customerID,
		OrderName:       orderName,
		ShippingAddress: shippingAddress,
		BillingAddress:  billingAddress,
		Payment:         payment,
		Status:          status,
	}

	order.items = make([]OrderItem, len(items))
	copy(order.items, items)

	return order
}
