package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/coupons"
)

// Seed inserts the sample coupons when the table is empty.
//
// The product names come from the .NET DiscountContext.OnModelCreating seed and
// are kept byte-identical, including "Iphone X" - note the lower-case p, which
// does NOT match the "iPhone X" the Catalog service seeds. Discounts are looked
// up by exact product name, so that coupon never applies in practice. The
// mismatch is carried rather than quietly fixed here, so both projects stay in
// step and the bug is fixed in one place once decided.
func Seed(ctx context.Context, db *sql.DB) (int, error) {
	var existing int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM coupons`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("counting coupons before seeding: %w", err)
	}
	if existing > 0 {
		return 0, nil
	}

	repo := NewCouponRepository(db)
	for _, coupon := range sampleCoupons() {
		if _, err := repo.Create(ctx, coupon); err != nil {
			return 0, fmt.Errorf("seeding coupon for %q: %w", coupon.ProductName, err)
		}
	}

	return len(sampleCoupons()), nil
}

func sampleCoupons() []coupons.Coupon {
	return []coupons.Coupon{
		{ProductName: "Iphone X", Description: "Iphone discount", Amount: 150},
		{ProductName: "Samsung 10", Description: "Samsung discount", Amount: 100},
	}
}
