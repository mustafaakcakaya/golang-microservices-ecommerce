package carts

import "context"

// DiscountLookup resolves the discount for a product name.
//
// Basket depends on this narrow interface rather than the generated gRPC client
// so the handler stays testable and unaware of the transport. The Discount
// service answers a zero amount for products with no coupon, so a missing
// discount is an ordinary result rather than an error.
type DiscountLookup interface {
	AmountFor(ctx context.Context, productName string) (int32, error)
}
