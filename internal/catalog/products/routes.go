package products

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
)

// routeLogger is the logger routes use for unexpected errors.
type routeLogger = *slog.Logger

// RegisterRoutes mounts every product slice on r. Handlers are wrapped in the
// shared pipeline here, in one place, so no slice can forget it.
func RegisterRoutes(r chi.Router, repo Repository, log *slog.Logger) {
	getProducts := cqrs.Chain[GetProductsQuery, GetProductsResult](
		NewGetProductsHandler(repo),
		cqrs.Logging[GetProductsQuery, GetProductsResult](log, "GetProducts"),
	)
	getProductByID := cqrs.Chain[GetProductByIDQuery, GetProductByIDResult](
		NewGetProductByIDHandler(repo),
		cqrs.Logging[GetProductByIDQuery, GetProductByIDResult](log, "GetProductById"),
	)
	getProductByCategory := cqrs.Chain[GetProductByCategoryQuery, GetProductByCategoryResult](
		NewGetProductByCategoryHandler(repo),
		cqrs.Logging[GetProductByCategoryQuery, GetProductByCategoryResult](log, "GetProductByCategory"),
	)
	// Commands validate before running; queries carry no rules.
	createProduct := cqrs.Chain[CreateProductCommand, CreateProductResult](
		NewCreateProductHandler(repo),
		cqrs.Logging[CreateProductCommand, CreateProductResult](log, "CreateProduct"),
		cqrs.Validating[CreateProductCommand, CreateProductResult](),
	)
	updateProduct := cqrs.Chain[UpdateProductCommand, UpdateProductResult](
		NewUpdateProductHandler(repo),
		cqrs.Logging[UpdateProductCommand, UpdateProductResult](log, "UpdateProduct"),
		cqrs.Validating[UpdateProductCommand, UpdateProductResult](),
	)
	deleteProduct := cqrs.Chain[DeleteProductCommand, DeleteProductResult](
		NewDeleteProductHandler(repo),
		cqrs.Logging[DeleteProductCommand, DeleteProductResult](log, "DeleteProduct"),
		cqrs.Validating[DeleteProductCommand, DeleteProductResult](),
	)

	r.Route("/products", func(r chi.Router) {
		r.Get("/", GetProductsRoute(getProducts, log))
		r.Post("/", CreateProductRoute(createProduct, log))
		r.Put("/", UpdateProductRoute(updateProduct, log))
		r.Get("/{id}", GetProductByIDRoute(getProductByID, log))
		r.Delete("/{id}", DeleteProductRoute(deleteProduct, log))
		r.Get("/category/{category}", GetProductByCategoryRoute(getProductByCategory, log))
	})
}
