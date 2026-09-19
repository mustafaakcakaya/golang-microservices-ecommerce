// Package carts holds the Basket service's model, handlers and routes.
package carts

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func init() {
	// Money goes on the wire as a JSON number, matching Catalog and Ordering.
	// shopspring/decimal decides this through a package-level global that
	// defaults to a quoted string, so without this the same price would be a
	// number from one service and a string from another - and every client
	// would have to handle both.
	decimal.MarshalJSONWithoutQuotes = true
}

// ShoppingCart is one user's basket. The user name is the identity: a user has
// exactly one basket, and storing replaces it.
type ShoppingCart struct {
	UserName string             `json:"userName" validate:"required"`
	Items    []ShoppingCartItem `json:"items"`

	// Version identifies this exact basket and changes on every write. It is
	// storage's own bookkeeping, not part of the wire format, so a client
	// neither sends it nor sees it - but a checkout derives its message id
	// from it, which is what stops a double click becoming two orders.
	Version uuid.UUID `json:"-"`
}

// ShoppingCartItem is a single line in the basket.
type ShoppingCartItem struct {
	Quantity    int             `json:"quantity"`
	Color       string          `json:"color"`
	Price       decimal.Decimal `json:"price"`
	ProductID   uuid.UUID       `json:"productId"`
	ProductName string          `json:"productName"`
}

// TotalPrice sums the lines.
//
// It is derived rather than stored, so it can never drift from the lines it
// sums. MarshalJSON adds it to the wire format so clients still see the field.
func (c ShoppingCart) TotalPrice() decimal.Decimal {
	total := decimal.Zero
	for _, item := range c.Items {
		total = total.Add(item.Price.Mul(decimal.NewFromInt(int64(item.Quantity))))
	}
	return total
}

// MarshalJSON emits totalPrice alongside the stored fields, without letting the
// derived value into the database.
func (c ShoppingCart) MarshalJSON() ([]byte, error) {
	type cart ShoppingCart // alias avoids recursing into this method

	return json.Marshal(struct {
		cart
		TotalPrice decimal.Decimal `json:"totalPrice"`
	}{cart(c), c.TotalPrice()})
}

// Repository stores baskets. GetByUserName reports apperr.NotFound when the
// user has no basket, which the HTTP layer turns into a 404.
type Repository interface {
	GetByUserName(ctx context.Context, userName string) (ShoppingCart, error)
	Store(ctx context.Context, cart ShoppingCart) error
	Delete(ctx context.Context, userName string) error
}
