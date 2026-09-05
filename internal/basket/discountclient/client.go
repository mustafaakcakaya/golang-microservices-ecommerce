// Package discountclient talks to the Discount gRPC service.
package discountclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/discountpb"
)

// Client resolves discounts over gRPC.
//
// The generated package is reused rather than a second copy of the contract
// being generated for Basket, as the .NET project does by including the .proto
// in both projects: one module means one set of stubs and no chance of the two
// drifting apart.
type Client struct {
	rpc discountpb.DiscountProtoServiceClient
}

// Dial connects to the Discount service. The connection is lazy - gRPC dials on
// first use - so a slow start elsewhere does not block this service's startup.
func Dial(target string) (*Client, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("creating discount client for %q: %w", target, err)
	}

	return &Client{rpc: discountpb.NewDiscountProtoServiceClient(conn)}, conn, nil
}

// New wraps an existing gRPC client, for tests and custom wiring.
func New(rpc discountpb.DiscountProtoServiceClient) *Client {
	return &Client{rpc: rpc}
}

// AmountFor returns the discount for a product, or zero when it has none.
func (c *Client) AmountFor(ctx context.Context, productName string) (int32, error) {
	coupon, err := c.rpc.GetDiscount(ctx, &discountpb.GetDiscountRequest{ProductName: productName})
	if err != nil {
		return 0, fmt.Errorf("looking up discount for %q: %w", productName, err)
	}

	return coupon.GetAmount(), nil
}
