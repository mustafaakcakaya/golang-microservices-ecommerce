// Package coupons holds the Discount service's model and business rules.
package coupons

import "context"

// Coupon is a discount attached to a product name.
type Coupon struct {
	ID          int32
	ProductName string
	Description string
	Amount      int32
}

// NoDiscount is what GetDiscount answers for a product with no coupon.
//
// Basket subtracts the amount from every line without checking whether a
// discount exists, so answering an error here would fail a whole checkout the
// moment one product lacked a coupon.
func NoDiscount() Coupon {
	return Coupon{ProductName: "No Discount", Description: "No Discount Desc", Amount: 0}
}

// Repository stores coupons. ByProductName reports apperr.NotFound when there
// is no coupon for the product; the "no discount" fallback is applied above it,
// so the storage layer stays honest about what it holds.
type Repository interface {
	ByProductName(ctx context.Context, productName string) (Coupon, error)
	Create(ctx context.Context, coupon Coupon) (Coupon, error)
	Update(ctx context.Context, coupon Coupon) (Coupon, error)
	Delete(ctx context.Context, productName string) error
}
