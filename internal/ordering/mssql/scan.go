package mssql

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/ordering/domain"
)

// SQL Server stores UNIQUEIDENTIFIER with the first three groups in
// little-endian order. Scanning straight into a uuid.UUID copies those raw
// bytes and silently returns a different id than was written - the value in the
// database is correct, only the Go side is reversed. The driver's own type
// performs the conversion, so every id crosses the boundary through it.

// selectOrders lists the columns every order query reads, so the scan order is
// defined in one place.
const selectOrders = `
	SELECT
		Id, CustomerId, OrderName, Status,
		ShippingAddress_FirstName, ShippingAddress_LastName, ShippingAddress_EmailAddress,
		ShippingAddress_AddressLine, ShippingAddress_Country, ShippingAddress_State, ShippingAddress_ZipCode,
		BillingAddress_FirstName, BillingAddress_LastName, BillingAddress_EmailAddress,
		BillingAddress_AddressLine, BillingAddress_Country, BillingAddress_State, BillingAddress_ZipCode,
		Payment_CardName, Payment_CardNumber, Payment_Expiration, Payment_CVV, Payment_PaymentMethod
	FROM Orders`

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanOrder rebuilds an order from a row.
//
// Values are rebuilt through the domain constructors so a row that violates a
// rule is caught here rather than becoming an aggregate that could never have
// been created through the API.
func scanOrder(row scanner) (*domain.Order, error) {
	var (
		id, customerID         guid
		orderName, status      string
		shipFirst, shipLast    string
		shipEmail              *string
		shipLine, shipCountry  string
		shipState, shipZip     string
		billFirst, billLast    string
		billEmail              *string
		billLine, billCountry  string
		billState, billZip     string
		cardName               *string
		cardNumber, expiration string
		cvv                    string
		paymentMethod          int
	)

	if err := row.Scan(
		&id, &customerID, &orderName, &status,
		&shipFirst, &shipLast, &shipEmail, &shipLine, &shipCountry, &shipState, &shipZip,
		&billFirst, &billLast, &billEmail, &billLine, &billCountry, &billState, &billZip,
		&cardName, &cardNumber, &expiration, &cvv, &paymentMethod,
	); err != nil {
		return nil, err
	}

	orderID, err := domain.NewOrderID(uuid.UUID(id))
	if err != nil {
		return nil, err
	}
	customer, err := domain.NewCustomerID(uuid.UUID(customerID))
	if err != nil {
		return nil, err
	}
	name, err := domain.NewOrderName(orderName)
	if err != nil {
		return nil, err
	}
	orderStatus, err := domain.ParseOrderStatus(status)
	if err != nil {
		return nil, err
	}
	shipping, err := domain.NewAddress(shipFirst, shipLast, deref(shipEmail), shipLine, shipCountry, shipState, shipZip)
	if err != nil {
		return nil, fmt.Errorf("shipping address: %w", err)
	}
	billing, err := domain.NewAddress(billFirst, billLast, deref(billEmail), billLine, billCountry, billState, billZip)
	if err != nil {
		return nil, fmt.Errorf("billing address: %w", err)
	}
	payment, err := domain.NewPayment(deref(cardName), cardNumber, expiration, cvv, paymentMethod)
	if err != nil {
		return nil, fmt.Errorf("payment: %w", err)
	}

	return domain.Restore(orderID, customer, name, shipping, billing, payment, orderStatus, nil), nil
}

// scanOrderItem rebuilds one line.
func scanOrderItem(row scanner) (domain.OrderItem, error) {
	var (
		id, orderID, productID guid
		quantity               int
		price                  string
	)

	if err := row.Scan(&id, &orderID, &productID, &quantity, &price); err != nil {
		return domain.OrderItem{}, err
	}

	itemID, err := domain.NewOrderItemID(uuid.UUID(id))
	if err != nil {
		return domain.OrderItem{}, err
	}
	order, err := domain.NewOrderID(uuid.UUID(orderID))
	if err != nil {
		return domain.OrderItem{}, err
	}
	product, err := domain.NewProductID(uuid.UUID(productID))
	if err != nil {
		return domain.OrderItem{}, err
	}
	amount, err := decimalFromString(price)
	if err != nil {
		return domain.OrderItem{}, err
	}

	return domain.OrderItem{
		ID:        itemID,
		OrderID:   order,
		ProductID: product,
		Quantity:  quantity,
		Price:     amount,
	}, nil
}

// withItems returns the order with its lines attached.
func withItems(order *domain.Order, items []domain.OrderItem) *domain.Order {
	return domain.Restore(
		order.ID, order.CustomerID, order.OrderName,
		order.ShippingAddress, order.BillingAddress, order.Payment,
		order.Status, items,
	)
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
