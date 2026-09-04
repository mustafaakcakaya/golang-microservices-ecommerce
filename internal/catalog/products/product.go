// Package products holds the Catalog service's product feature slices.
//
// Each slice (get products, get by id, create, ...) lives in its own file with
// its request, handler and HTTP route together, mirroring the Products/<Slice>
// folders of the .NET service.
package products

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func init() {
	// The .NET API serializes decimal as a JSON number. shopspring/decimal
	// defaults to a quoted string; switching it here keeps the wire format of
	// this service identical to the original so existing clients keep working.
	decimal.MarshalJSONWithoutQuotes = true
}

// Product is the catalog entry. It is a document, not a relational row: the
// whole struct is stored as JSONB, as Marten does in the .NET version.
type Product struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Category    []string        `json:"category"`
	Description string          `json:"description"`
	ImageFile   string          `json:"imageFile"`
	Price       decimal.Decimal `json:"price"`
}
