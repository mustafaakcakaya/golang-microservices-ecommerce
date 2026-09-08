// Package integration maps the Ordering service's domain events onto the
// contracts that leave it.
//
// The two are deliberately separate. A domain event carries the aggregate and
// changes freely with the code inside the service; an integration event is a
// promise to other services. This package is the one place the first becomes
// the second, which is also the one place to look when asking what Ordering
// tells the outside world.
package integration

import (
	"context"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/orderingv1"
)

// Map turns a domain event into the message that will be stored in the outbox.
//
// The second return value is false for a domain event that has no outward
// contract: not everything that happens inside a service is anyone else's
// business, and staying silent is the default.
func Map(ctx context.Context, event domain.Event) (messaging.Envelope, bool, error) {
	// The same value the error responses report as their trace id, so a failed
	// request and the message it did or did not produce can be lined up.
	header := messaging.NewHeader(httpx.CorrelationID(ctx))

	switch raised := event.(type) {
	case domain.OrderCreated:
		return envelope(&orderingv1.OrderCreated{
			Header:     header,
			OrderID:    raised.Order.ID.UUID(),
			CustomerID: raised.Order.CustomerID.UUID(),
			OrderName:  raised.Order.OrderName.String(),
			Status:     raised.Order.Status.String(),
			TotalPrice: messaging.NewMoney(raised.Order.TotalPrice()),
			Items:      lines(raised.Order),
		}, raised.Order)

	case domain.OrderUpdated:
		return envelope(&orderingv1.OrderUpdated{
			Header:     header,
			OrderID:    raised.Order.ID.UUID(),
			CustomerID: raised.Order.CustomerID.UUID(),
			OrderName:  raised.Order.OrderName.String(),
			Status:     raised.Order.Status.String(),
			TotalPrice: messaging.NewMoney(raised.Order.TotalPrice()),
		}, raised.Order)

	default:
		return messaging.Envelope{}, false, nil
	}
}

// Registry lists the contracts this service publishes, so a reader can refuse
// anything it was not built to understand.
func Registry() *messaging.Registry {
	registry := messaging.NewRegistry()
	orderingv1.Register(registry)

	return registry
}

func envelope(event messaging.Event, order *domain.Order) (messaging.Envelope, bool, error) {
	built, err := messaging.NewEnvelope(event, order.ID.String())
	if err != nil {
		return messaging.Envelope{}, false, err
	}

	return built, true, nil
}

// lines maps the order lines. Addresses and payment are not mapped at all:
// nothing outside Ordering needs them, so nothing outside Ordering gets them.
func lines(order *domain.Order) []orderingv1.Line {
	items := order.Items()

	mapped := make([]orderingv1.Line, 0, len(items))
	for _, item := range items {
		mapped = append(mapped, orderingv1.Line{
			ProductID: item.ProductID.UUID(),
			Quantity:  item.Quantity,
			Price:     messaging.NewMoney(item.Price),
		})
	}

	return mapped
}
