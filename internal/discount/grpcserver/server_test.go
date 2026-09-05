package grpcserver_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/coupons"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/discountpb"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/grpcserver"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// fakeRepository stands in for storage so the server's own behaviour - status
// mapping and the no-discount fallback - is what gets tested.
type fakeRepository struct {
	items  map[string]coupons.Coupon
	failOn error
}

func newFakeRepository(seed ...coupons.Coupon) *fakeRepository {
	repo := &fakeRepository{items: make(map[string]coupons.Coupon)}
	for _, coupon := range seed {
		repo.items[coupon.ProductName] = coupon
	}
	return repo
}

func (r *fakeRepository) ByProductName(_ context.Context, productName string) (coupons.Coupon, error) {
	if r.failOn != nil {
		return coupons.Coupon{}, r.failOn
	}
	coupon, ok := r.items[productName]
	if !ok {
		return coupons.Coupon{}, apperr.NotFound("Coupon", productName)
	}
	return coupon, nil
}

func (r *fakeRepository) Create(_ context.Context, coupon coupons.Coupon) (coupons.Coupon, error) {
	if r.failOn != nil {
		return coupons.Coupon{}, r.failOn
	}
	if _, exists := r.items[coupon.ProductName]; exists {
		return coupons.Coupon{}, apperr.Conflict("a coupon for %q already exists", coupon.ProductName)
	}
	coupon.ID = int32(len(r.items) + 1)
	r.items[coupon.ProductName] = coupon
	return coupon, nil
}

func (r *fakeRepository) Update(_ context.Context, coupon coupons.Coupon) (coupons.Coupon, error) {
	if r.failOn != nil {
		return coupons.Coupon{}, r.failOn
	}
	existing, ok := r.items[coupon.ProductName]
	if !ok {
		return coupons.Coupon{}, apperr.NotFound("Coupon", coupon.ProductName)
	}
	coupon.ID = existing.ID
	r.items[coupon.ProductName] = coupon
	return coupon, nil
}

func (r *fakeRepository) Delete(_ context.Context, productName string) error {
	if r.failOn != nil {
		return r.failOn
	}
	if _, ok := r.items[productName]; !ok {
		return apperr.NotFound("Coupon", productName)
	}
	delete(r.items, productName)
	return nil
}

// newClient serves the gRPC server over an in-memory connection, so the tests
// exercise real marshalling and status codes without binding a port.
func newClient(t *testing.T, repo coupons.Repository) discountpb.DiscountProtoServiceClient {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	discountpb.RegisterDiscountProtoServiceServer(server, grpcserver.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil))))

	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("serving: %v", err)
		}
	}()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dialling: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
	})

	return discountpb.NewDiscountProtoServiceClient(conn)
}

func TestGetDiscountReturnsTheCoupon(t *testing.T) {
	t.Parallel()

	client := newClient(t, newFakeRepository(coupons.Coupon{
		ID: 1, ProductName: "Samsung 10", Description: "Samsung discount", Amount: 100,
	}))

	got, err := client.GetDiscount(t.Context(), &discountpb.GetDiscountRequest{ProductName: "Samsung 10"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetAmount() != 100 || got.GetProductName() != "Samsung 10" {
		t.Errorf("coupon = %+v, want the stored one", got)
	}
	// The contract misspells the field; the value still has to arrive.
	if got.GetDesciption() != "Samsung discount" {
		t.Errorf("description = %q, want the stored text", got.GetDesciption())
	}
}

func TestGetDiscountFallsBackToNoDiscount(t *testing.T) {
	t.Parallel()

	client := newClient(t, newFakeRepository())

	got, err := client.GetDiscount(t.Context(), &discountpb.GetDiscountRequest{ProductName: "unknown"})

	// Basket subtracts the amount without checking, so a NotFound here would
	// fail an entire checkout instead of applying no discount.
	if err != nil {
		t.Fatalf("a missing coupon must not be an error: %v", err)
	}
	if got.GetAmount() != 0 || got.GetProductName() != "No Discount" {
		t.Errorf("coupon = %+v, want the zero-amount placeholder", got)
	}
}

func TestGetDiscountReportsRealFailures(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	repo.failOn = errors.New("database is down")
	client := newClient(t, repo)

	_, err := client.GetDiscount(t.Context(), &discountpb.GetDiscountRequest{ProductName: "Samsung 10"})

	// A broken database must not masquerade as "no discount".
	if status.Code(err) != codes.Internal {
		t.Errorf("code = %v, want Internal (error %v)", status.Code(err), err)
	}
}

func TestCreateDiscountStoresAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	client := newClient(t, newFakeRepository())
	coupon := &discountpb.CouponModel{ProductName: "iPhone X", Desciption: "Iphone discount", Amount: 150}

	created, err := client.CreateDiscount(t.Context(), &discountpb.CreateDiscountRequest{Coupon: coupon})
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if created.GetId() == 0 {
		t.Error("id should be assigned")
	}

	_, err = client.CreateDiscount(t.Context(), &discountpb.CreateDiscountRequest{Coupon: coupon})
	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("code = %v, want AlreadyExists (error %v)", status.Code(err), err)
	}
}

func TestCreateDiscountRejectsMissingInput(t *testing.T) {
	t.Parallel()

	client := newClient(t, newFakeRepository())

	_, err := client.CreateDiscount(t.Context(), &discountpb.CreateDiscountRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("missing coupon: code = %v, want InvalidArgument", status.Code(err))
	}

	_, err = client.CreateDiscount(t.Context(), &discountpb.CreateDiscountRequest{
		Coupon: &discountpb.CouponModel{Amount: 10},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("missing product name: code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestUpdateDiscountAppliesChangesAndReportsMissing(t *testing.T) {
	t.Parallel()

	client := newClient(t, newFakeRepository(coupons.Coupon{
		ID: 1, ProductName: "Samsung 10", Description: "old", Amount: 100,
	}))

	updated, err := client.UpdateDiscount(t.Context(), &discountpb.UpdateDiscountRequest{
		Coupon: &discountpb.CouponModel{ProductName: "Samsung 10", Desciption: "new", Amount: 120},
	})
	if err != nil {
		t.Fatalf("updating: %v", err)
	}
	if updated.GetAmount() != 120 || updated.GetDesciption() != "new" {
		t.Errorf("coupon = %+v, want the updated values", updated)
	}

	_, err = client.UpdateDiscount(t.Context(), &discountpb.UpdateDiscountRequest{
		Coupon: &discountpb.CouponModel{ProductName: "ghost", Amount: 1},
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("code = %v, want NotFound (error %v)", status.Code(err), err)
	}
}

func TestDeleteDiscountRemovesAndReportsMissing(t *testing.T) {
	t.Parallel()

	client := newClient(t, newFakeRepository(coupons.Coupon{ID: 1, ProductName: "iPhone X", Amount: 150}))

	response, err := client.DeleteDiscount(t.Context(), &discountpb.DeleteDiscountRequest{ProductName: "iPhone X"})
	if err != nil {
		t.Fatalf("deleting: %v", err)
	}
	// The contract types success as a string; the .NET client reads "true".
	if response.GetSuccess() != "true" {
		t.Errorf("success = %q, want \"true\"", response.GetSuccess())
	}

	_, err = client.DeleteDiscount(t.Context(), &discountpb.DeleteDiscountRequest{ProductName: "iPhone X"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("code = %v, want NotFound (error %v)", status.Code(err), err)
	}
}
