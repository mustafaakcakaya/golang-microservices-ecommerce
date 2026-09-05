package sqlite_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/coupons"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/sqlite"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// newRepository opens a SQLite file in the test's temp directory and migrates
// it. No container is needed - the driver is pure Go - so these run in the
// short suite too.
func newRepository(t *testing.T) (*sqlite.CouponRepository, *sql.DB) {
	t.Helper()

	db, err := sqlite.Open(filepath.Join(t.TempDir(), "discount.db"))
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	return sqlite.NewCouponRepository(db), db
}

func TestCreateAssignsIDAndRoundTrips(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)

	created, err := repo.Create(t.Context(), coupons.Coupon{
		ProductName: "iPhone X", Description: "iPhone discount", Amount: 150,
	})
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if created.ID == 0 {
		t.Error("id should be assigned by the database")
	}

	got, err := repo.ByProductName(t.Context(), "iPhone X")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got.Amount != 150 || got.Description != "iPhone discount" || got.ID != created.ID {
		t.Errorf("coupon = %+v, want the stored values", got)
	}
}

func TestByProductNameReportsNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)

	_, err := repo.ByProductName(t.Context(), "nothing")

	// The gRPC layer turns this into the "no discount" placeholder; a bare sql
	// error would become Internal instead.
	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound (error %v)", got, err)
	}
}

func TestDuplicateProductIsRejected(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	coupon := coupons.Coupon{ProductName: "iPhone X", Amount: 150}

	if _, err := repo.Create(t.Context(), coupon); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := repo.Create(t.Context(), coupon)

	// Product name is the business key: a second row would shadow the first and
	// make lookups depend on row order.
	if got := apperr.KindOf(err); got != apperr.KindConflict {
		t.Errorf("kind = %v, want KindConflict (error %v)", got, err)
	}
}

func TestUpdateReplacesFields(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	created, err := repo.Create(t.Context(), coupons.Coupon{
		ProductName: "Samsung 10", Description: "old", Amount: 100,
	})
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	updated, err := repo.Update(t.Context(), coupons.Coupon{
		ProductName: "Samsung 10", Description: "new", Amount: 120,
	})
	if err != nil {
		t.Fatalf("updating: %v", err)
	}
	if updated.ID != created.ID {
		t.Errorf("id = %d, want the existing row %d", updated.ID, created.ID)
	}

	got, err := repo.ByProductName(t.Context(), "Samsung 10")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got.Amount != 120 || got.Description != "new" {
		t.Errorf("coupon = %+v, want the updated values", got)
	}
}

func TestUpdateMissingCouponReportsNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)

	_, err := repo.Update(t.Context(), coupons.Coupon{ProductName: "ghost", Amount: 1})

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound (error %v)", got, err)
	}
}

func TestDeleteRemovesCouponAndReportsMissingOnes(t *testing.T) {
	t.Parallel()

	repo, _ := newRepository(t)
	if _, err := repo.Create(t.Context(), coupons.Coupon{ProductName: "iPhone X", Amount: 150}); err != nil {
		t.Fatalf("creating: %v", err)
	}

	if err := repo.Delete(t.Context(), "iPhone X"); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	// Unlike Basket, deleting a missing coupon is an error here: the caller
	// asked to remove something specific that was not there.
	err := repo.Delete(t.Context(), "iPhone X")
	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Errorf("second delete kind = %v, want KindNotFound (error %v)", got, err)
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	t.Parallel()

	repo, db := newRepository(t)

	inserted, err := sqlite.Seed(t.Context(), db)
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted = %d, want the two sample coupons", inserted)
	}

	again, err := sqlite.Seed(t.Context(), db)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if again != 0 {
		t.Errorf("second run inserted %d, want 0", again)
	}

	coupon, err := repo.ByProductName(t.Context(), "Samsung 10")
	if err != nil {
		t.Fatalf("loading seeded coupon: %v", err)
	}
	if coupon.Amount != 100 {
		t.Errorf("amount = %d, want 100", coupon.Amount)
	}
}

func TestSeedProductNamesMatchTheCatalogSpelling(t *testing.T) {
	t.Parallel()

	repo, db := newRepository(t)
	if _, err := sqlite.Seed(t.Context(), db); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// Discounts are looked up by exact product name, so a coupon whose spelling
	// differs from the catalog's by one letter silently never applies. These
	// are the names the Catalog service seeds.
	for _, productName := range []string{"iPhone X", "Samsung 10"} {
		if _, err := repo.ByProductName(t.Context(), productName); err != nil {
			t.Errorf("no coupon for %q, which the catalog seeds: %v", productName, err)
		}
	}
}
