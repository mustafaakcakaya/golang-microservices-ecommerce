package domain

// OrderStatus is where an order sits in its lifecycle.
type OrderStatus int

// The numeric values are persisted, so they must not be renumbered.
const (
	OrderStatusDraft     OrderStatus = 1
	OrderStatusPending   OrderStatus = 2
	OrderStatusCompleted OrderStatus = 3
	OrderStatusCancelled OrderStatus = 4
)

// String renders the status.
func (s OrderStatus) String() string {
	switch s {
	case OrderStatusDraft:
		return "Draft"
	case OrderStatusPending:
		return "Pending"
	case OrderStatusCompleted:
		return "Completed"
	case OrderStatusCancelled:
		return "Cancelled"
	default:
		return "Unknown"
	}
}

// IsValid reports whether the status is one the domain defines. Persistence
// reads these back from the database, where an older row could carry a value
// this build no longer knows.
func (s OrderStatus) IsValid() bool {
	switch s {
	case OrderStatusDraft, OrderStatusPending, OrderStatusCompleted, OrderStatusCancelled:
		return true
	default:
		return false
	}
}

// ParseOrderStatus converts a stored name back into a status.
func ParseOrderStatus(name string) (OrderStatus, error) {
	for _, status := range []OrderStatus{
		OrderStatusDraft, OrderStatusPending, OrderStatusCompleted, OrderStatusCancelled,
	} {
		if status.String() == name {
			return status, nil
		}
	}

	return 0, invalidf("unknown order status %q", name)
}
