// Package products holds the Catalog service's product feature slices.
//
// Each slice (get products, get by id, create, ...) lives in its own file with
// its request, handler and HTTP route together, so a feature is read and
// changed in one place.
package products

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func init() {
	// Prices go on the wire as JSON numbers. shopspring/decimal defaults to a
	// quoted string, so the setting is flipped once here for the whole service.
	decimal.MarshalJSONWithoutQuotes = true
}

// Product is the catalog entry. It is a document, not a relational row: the
// whole struct is stored as JSONB, since nothing queries its fields
// individually beyond the category.
type Product struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Category    []string        `json:"category"`
	Description string          `json:"description"`
	ImageFile   string          `json:"imageFile"`
	Price       decimal.Decimal `json:"price"`
}
