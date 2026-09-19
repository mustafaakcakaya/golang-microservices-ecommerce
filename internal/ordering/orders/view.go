package orders

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
)

// AddressView is a postal address as a read endpoint returns it.
type AddressView struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	EmailAddress string `json:"emailAddress"`
	AddressLine  string `json:"addressLine"`
	Country      string `json:"country"`
	State        string `json:"state"`
	ZipCode      string `json:"zipCode"`
}

// PaymentView is what a read endpoint may say about how an order was paid.
//
// There is no card number and no CVV here, and that is the whole point of the
// type. Storing the card details is one thing; handing them back out over an
// unauthenticated GET is another, and a single DTO shared between the write and
// read sides is what silently turns the first into the second. The masked
// number is kept so a human can still recognise which card was used.
type PaymentView struct {
	CardName         string `json:"cardName"`
	MaskedCardNumber string `json:"maskedCardNumber"`
	Expiration       string `json:"expiration"`
	PaymentMethod    int    `json:"paymentMethod"`
}

// OrderItemView is one line of an order.
type OrderItemView struct {
	OrderID   uuid.UUID       `json:"orderId"`
	ProductID uuid.UUID       `json:"productId"`
	Quantity  int             `json:"quantity"`
	Price     decimal.Decimal `json:"price"`
}

// OrderView is the read model of an order.
//
// The status travels as its name rather than its number: the number is a
// storage detail, and "Pending" is readable in a response where 2 is not.
type OrderView struct {
	ID              uuid.UUID   `json:"id"`
	CustomerID      uuid.UUID   `json:"customerId"`
	OrderName       string      `json:"orderName"`
	Status          string      `json:"status"`
	ShippingAddress AddressView `json:"shippingAddress"`
	BillingAddress  AddressView `json:"billingAddress"`
	// Absent until the order is paid for, rather than an object full of empty
	// strings that reads like a card nobody filled in.
	Payment    *PaymentView    `json:"payment"`
	Items      []OrderItemView `json:"orderItems"`
	TotalPrice decimal.Decimal `json:"totalPrice"`
}

// viewOf maps an aggregate to its read model.
//
// The mapping is written by hand rather than serialized from the aggregate:
// an automatic mapping would carry whatever the domain happens to hold today,
// so adding a field to Order would silently publish it. Here every field that
// leaves the service is named on purpose.
func viewOf(order *domain.Order) OrderView {
	items := order.Items()

	lines := make([]OrderItemView, 0, len(items))
	for _, item := range items {
		lines = append(lines, OrderItemView{
			OrderID:   item.OrderID.UUID(),
			ProductID: item.ProductID.UUID(),
			Quantity:  item.Quantity,
			Price:     item.Price,
		})
	}

	return OrderView{
		ID:              order.ID().UUID(),
		CustomerID:      order.CustomerID().UUID(),
		OrderName:       order.OrderName().String(),
		Status:          order.Status().String(),
		ShippingAddress: addressView(order.ShippingAddress()),
		BillingAddress:  addressView(order.BillingAddress()),
		Payment:         paymentView(order.Payment()),
		Items:           lines,
		TotalPrice:      order.TotalPrice(),
	}
}

// viewsOf maps a list of aggregates, guaranteeing a non-nil slice so an empty
// result serializes as [] rather than null.
func viewsOf(list []*domain.Order) []OrderView {
	views := make([]OrderView, 0, len(list))
	for _, order := range list {
		views = append(views, viewOf(order))
	}
	return views
}

// paymentView renders the payment, or nothing when there is none yet.
func paymentView(payment domain.Payment) *PaymentView {
	if payment.IsZero() {
		return nil
	}

	return &PaymentView{
		CardName:         payment.CardName(),
		MaskedCardNumber: payment.MaskedCardNumber(),
		Expiration:       payment.Expiration(),
		PaymentMethod:    payment.PaymentMethod(),
	}
}

func addressView(address domain.Address) AddressView {
	return AddressView(address)
}
