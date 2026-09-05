package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/coupons"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// CouponRepository stores coupons in SQLite.
type CouponRepository struct {
	db *sql.DB
}

// NewCouponRepository wires the repository.
func NewCouponRepository(db *sql.DB) *CouponRepository {
	return &CouponRepository{db: db}
}

// ByProductName loads the coupon for a product.
func (r *CouponRepository) ByProductName(ctx context.Context, productName string) (coupons.Coupon, error) {
	const query = `SELECT id, product_name, description, amount FROM coupons WHERE product_name = ?`

	var coupon coupons.Coupon
	err := r.db.QueryRowContext(ctx, query, productName).
		Scan(&coupon.ID, &coupon.ProductName, &coupon.Description, &coupon.Amount)
	if errors.Is(err, sql.ErrNoRows) {
		return coupons.Coupon{}, apperr.NotFound("Coupon", productName)
	}
	if err != nil {
		return coupons.Coupon{}, fmt.Errorf("loading coupon for %q: %w", productName, err)
	}

	return coupon, nil
}

// Create inserts a coupon and returns it with the assigned id.
func (r *CouponRepository) Create(ctx context.Context, coupon coupons.Coupon) (coupons.Coupon, error) {
	const query = `INSERT INTO coupons (product_name, description, amount) VALUES (?, ?, ?) RETURNING id`

	err := r.db.QueryRowContext(ctx, query, coupon.ProductName, coupon.Description, coupon.Amount).
		Scan(&coupon.ID)
	if err != nil {
		// The unique index turns a duplicate product into a conflict rather
		// than a second row that would shadow the first.
		if isUniqueViolation(err) {
			return coupons.Coupon{}, apperr.Conflict("a coupon for %q already exists", coupon.ProductName)
		}
		return coupons.Coupon{}, fmt.Errorf("creating coupon for %q: %w", coupon.ProductName, err)
	}

	return coupon, nil
}

// Update replaces a coupon's fields, addressing it by product name.
func (r *CouponRepository) Update(ctx context.Context, coupon coupons.Coupon) (coupons.Coupon, error) {
	const query = `
		UPDATE coupons SET description = ?, amount = ?
		WHERE product_name = ?
		RETURNING id`

	err := r.db.QueryRowContext(ctx, query, coupon.Description, coupon.Amount, coupon.ProductName).
		Scan(&coupon.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return coupons.Coupon{}, apperr.NotFound("Coupon", coupon.ProductName)
	}
	if err != nil {
		return coupons.Coupon{}, fmt.Errorf("updating coupon for %q: %w", coupon.ProductName, err)
	}

	return coupon, nil
}

// Delete removes a coupon, reporting not found when there is none.
func (r *CouponRepository) Delete(ctx context.Context, productName string) error {
	const query = `DELETE FROM coupons WHERE product_name = ?`

	result, err := r.db.ExecContext(ctx, query, productName)
	if err != nil {
		return fmt.Errorf("deleting coupon for %q: %w", productName, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking delete for %q: %w", productName, err)
	}
	// The .NET service loads first and raises NotFound; the row count gives the
	// same answer in one statement.
	if affected == 0 {
		return apperr.NotFound("Coupon", productName)
	}

	return nil
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure. The
// driver exposes no typed error for it, so the message is matched.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
