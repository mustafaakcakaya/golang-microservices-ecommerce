// Package orders holds the Ordering service's order feature slices.
//
// Each slice (create, update, delete, ...) lives in its own file with its
// command, handler and HTTP route together, so a feature is read and changed in
// one place.
//
// The write and read shapes are deliberately not the same type. A command
// carries the card details needed to place an order; a view never does. Sharing
// one DTO between the two is what turns a read endpoint into a way to fetch
// card numbers back out of the database.
package orders

import (
	"errors"
	"log/slog"

	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

func init() {
	// Money goes on the wire as a JSON number, matching the Catalog service.
	// shopspring/decimal defaults to a quoted string, so the setting is flipped
	// once here for the whole service.
	decimal.MarshalJSONWithoutQuotes = true
}

// routeLogger is the logger routes use for unexpected errors.
type routeLogger = *slog.Logger

// clientError reclassifies a domain rule violation as input the caller can
// correct.
//
// The transport layer treats anything unclassified as internal, and a
// *domain.Error carries no classification of its own - so without this, an
// order name of the wrong length would answer 500 and hide a caller mistake
// behind a server fault.
func clientError(err error) error {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return apperr.BadRequest("%s", domainErr.Message)
	}

	return err
}
