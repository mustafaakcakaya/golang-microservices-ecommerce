package carts

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
)

// routeLogger is the logger routes use for unexpected errors.
type routeLogger = *slog.Logger

// RegisterRoutes mounts every basket slice on r.
func RegisterRoutes(r chi.Router, repo Repository, discounts DiscountLookup, log *slog.Logger) {
	getBasket := cqrs.Chain[GetBasketQuery, GetBasketResult](
		NewGetBasketHandler(repo),
		cqrs.Logging[GetBasketQuery, GetBasketResult](log, "GetBasket"),
	)
	storeBasket := cqrs.Chain[StoreBasketCommand, StoreBasketResult](
		NewStoreBasketHandler(repo, discounts),
		cqrs.Logging[StoreBasketCommand, StoreBasketResult](log, "StoreBasket"),
		cqrs.Validating[StoreBasketCommand, StoreBasketResult](),
	)
	deleteBasket := cqrs.Chain[DeleteBasketCommand, DeleteBasketResult](
		NewDeleteBasketHandler(repo),
		cqrs.Logging[DeleteBasketCommand, DeleteBasketResult](log, "DeleteBasket"),
		cqrs.Validating[DeleteBasketCommand, DeleteBasketResult](),
	)

	r.Route("/basket", func(r chi.Router) {
		r.Post("/", StoreBasketRoute(storeBasket, log))
		r.Get("/{userName}", GetBasketRoute(getBasket, log))
		r.Delete("/{userName}", DeleteBasketRoute(deleteBasket, log))
	})
}
