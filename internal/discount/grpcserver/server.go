// Package grpcserver exposes the Discount service over gRPC.
package grpcserver

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/coupons"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/discountpb"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// Server implements the DiscountProtoService contract.
type Server struct {
	discountpb.UnimplementedDiscountProtoServiceServer

	repo coupons.Repository
	log  *slog.Logger
}

// New wires the server.
func New(repo coupons.Repository, log *slog.Logger) *Server {
	return &Server{repo: repo, log: log}
}

// GetDiscount returns the coupon for a product, or a zero-amount placeholder
// when the product has none.
//
// Answering NotFound would be more honest, but Basket subtracts the amount from
// every line without checking, so an error would fail an entire checkout the
// moment one product lacked a coupon. The .NET service makes the same choice.
func (s *Server) GetDiscount(ctx context.Context, request *discountpb.GetDiscountRequest) (*discountpb.CouponModel, error) {
	coupon, err := s.repo.ByProductName(ctx, request.GetProductName())
	switch {
	case err == nil:
	case apperr.KindOf(err) == apperr.KindNotFound:
		coupon = coupons.NoDiscount()
	default:
		return nil, s.fail(ctx, "GetDiscount", request.GetProductName(), err)
	}

	s.log.DebugContext(ctx, "discount resolved",
		"productName", request.GetProductName(), "amount", coupon.Amount)

	return toModel(coupon), nil
}

// CreateDiscount stores a new coupon.
func (s *Server) CreateDiscount(ctx context.Context, request *discountpb.CreateDiscountRequest) (*discountpb.CouponModel, error) {
	coupon, err := fromModel(request.GetCoupon())
	if err != nil {
		return nil, err
	}

	created, err := s.repo.Create(ctx, coupon)
	if err != nil {
		return nil, s.fail(ctx, "CreateDiscount", coupon.ProductName, err)
	}

	s.log.InfoContext(ctx, "discount created", "productName", created.ProductName)

	return toModel(created), nil
}

// UpdateDiscount replaces an existing coupon's fields.
func (s *Server) UpdateDiscount(ctx context.Context, request *discountpb.UpdateDiscountRequest) (*discountpb.CouponModel, error) {
	coupon, err := fromModel(request.GetCoupon())
	if err != nil {
		return nil, err
	}

	updated, err := s.repo.Update(ctx, coupon)
	if err != nil {
		return nil, s.fail(ctx, "UpdateDiscount", coupon.ProductName, err)
	}

	s.log.InfoContext(ctx, "discount updated", "productName", updated.ProductName)

	return toModel(updated), nil
}

// DeleteDiscount removes a coupon.
func (s *Server) DeleteDiscount(ctx context.Context, request *discountpb.DeleteDiscountRequest) (*discountpb.DeleteDiscountResponse, error) {
	if err := s.repo.Delete(ctx, request.GetProductName()); err != nil {
		return nil, s.fail(ctx, "DeleteDiscount", request.GetProductName(), err)
	}

	s.log.InfoContext(ctx, "discount deleted", "productName", request.GetProductName())

	// The contract types success as a string, not a bool; kept as-is so the
	// .NET client keeps working.
	return &discountpb.DeleteDiscountResponse{Success: "true"}, nil
}

// fail maps an application error to a gRPC status, logging anything unexpected.
func (s *Server) fail(ctx context.Context, operation, productName string, err error) error {
	code := codes.Internal
	message := "internal error"

	if appErr, ok := apperr.As(err); ok {
		switch appErr.Kind {
		case apperr.KindNotFound:
			code, message = codes.NotFound, appErr.Message
		case apperr.KindConflict:
			code, message = codes.AlreadyExists, appErr.Message
		case apperr.KindBadRequest, apperr.KindValidation:
			code, message = codes.InvalidArgument, appErr.Message
		case apperr.KindInternal:
		}
	}

	if code == codes.Internal {
		// Only unexpected failures are worth an error log; a missing coupon is
		// an ordinary answer.
		s.log.ErrorContext(ctx, "discount operation failed",
			"operation", operation, "productName", productName, "error", err)
	}

	return status.Error(code, message)
}

func toModel(coupon coupons.Coupon) *discountpb.CouponModel {
	return &discountpb.CouponModel{
		Id:          coupon.ID,
		ProductName: coupon.ProductName,
		// The field is misspelled in the shared contract. Renaming it would
		// break the .NET service and its clients, so the typo is carried.
		Desciption: coupon.Description,
		Amount:     coupon.Amount,
	}
}

func fromModel(model *discountpb.CouponModel) (coupons.Coupon, error) {
	if model == nil {
		return coupons.Coupon{}, status.Error(codes.InvalidArgument, "coupon is required")
	}
	if model.GetProductName() == "" {
		return coupons.Coupon{}, status.Error(codes.InvalidArgument, "productName is required")
	}

	return coupons.Coupon{
		ID:          model.GetId(),
		ProductName: model.GetProductName(),
		Description: model.GetDesciption(),
		Amount:      model.GetAmount(),
	}, nil
}

// Compile-time check: adding an RPC to the contract without implementing it
// here becomes a build failure rather than a runtime Unimplemented.
var _ discountpb.DiscountProtoServiceServer = (*Server)(nil)
