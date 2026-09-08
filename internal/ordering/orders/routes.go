package orders

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
)

// RegisterRoutes mounts every order slice on r. Handlers are wrapped in the
// shared pipeline here, in one place, so no slice can forget it.
func RegisterRoutes(r chi.Router, repo Repository, log *slog.Logger) {
	getOrders := cqrs.Chain[GetOrdersQuery, GetOrdersResult](
		NewGetOrdersHandler(repo),
		cqrs.Logging[GetOrdersQuery, GetOrdersResult](log, "GetOrders"),
	)
	getOrdersByName := cqrs.Chain[GetOrdersByNameQuery, GetOrdersByNameResult](
		NewGetOrdersByNameHandler(repo),
		cqrs.Logging[GetOrdersByNameQuery, GetOrdersByNameResult](log, "GetOrdersByName"),
	)
	getOrdersByCustomer := cqrs.Chain[GetOrdersByCustomerQuery, GetOrdersByCustomerResult](
		NewGetOrdersByCustomerHandler(repo),
		cqrs.Logging[GetOrdersByCustomerQuery, GetOrdersByCustomerResult](log, "GetOrdersByCustomer"),
	)
	// Commands validate before running; queries carry no rules.
	createOrder := cqrs.Chain[CreateOrderCommand, CreateOrderResult](
		NewCreateOrderHandler(repo),
		cqrs.Logging[CreateOrderCommand, CreateOrderResult](log, "CreateOrder"),
		cqrs.Validating[CreateOrderCommand, CreateOrderResult](),
	)
	updateOrder := cqrs.Chain[UpdateOrderCommand, UpdateOrderResult](
		NewUpdateOrderHandler(repo),
		cqrs.Logging[UpdateOrderCommand, UpdateOrderResult](log, "UpdateOrder"),
		cqrs.Validating[UpdateOrderCommand, UpdateOrderResult](),
	)
	deleteOrder := cqrs.Chain[DeleteOrderCommand, DeleteOrderResult](
		NewDeleteOrderHandler(repo),
		cqrs.Logging[DeleteOrderCommand, DeleteOrderResult](log, "DeleteOrder"),
		cqrs.Validating[DeleteOrderCommand, DeleteOrderResult](),
	)

	r.Route("/orders", func(r chi.Router) {
		r.Get("/", GetOrdersRoute(getOrders, log))
		r.Post("/", CreateOrderRoute(createOrder, log))
		r.Put("/", UpdateOrderRoute(updateOrder, log))
		r.Delete("/{id}", DeleteOrderRoute(deleteOrder, log))
		// The static segment is matched before the parameter, so a customer
		// lookup is not mistaken for an order name.
		r.Get("/customer/{customerId}", GetOrdersByCustomerRoute(getOrdersByCustomer, log))
		r.Get("/{orderName}", GetOrdersByNameRoute(getOrdersByName, log))
	})
}
