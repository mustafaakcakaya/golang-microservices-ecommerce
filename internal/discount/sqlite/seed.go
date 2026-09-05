package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/coupons"
)

// Seed inserts the sample coupons when the table is empty.
//
// Product names must match the Catalog spelling exactly: discounts are looked
// up by name, so a coupon whose name differs by a single letter silently never
// applies. "iPhone X" is the spelling Catalog seeds.
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
		{ProductName: "iPhone X", Description: "iPhone discount", Amount: 150},
		{ProductName: "Samsung 10", Description: "Samsung discount", Amount: 100},
	}
}
