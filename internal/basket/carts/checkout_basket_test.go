package carts_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/validation"
)

func checkoutCommand(userName string) carts.CheckoutBasketCommand {
	return carts.CheckoutBasketCommand{
		UserName:     userName,
		CustomerID:   uuid.New(),
		FirstName:    "Mustafa",
		LastName:     "Akcakaya",
		EmailAddress: "mustafa@example.com",
		AddressLine:  "Test Address",
		Country:      "Turkey",
		State:        "Istanbul",
		ZipCode:      "34000",

		PaymentToken:  "pt_0123456789abcdef0123456789abcdef",
		CardLast4:     "4444",
		PaymentMethod: 1,
	}
}

func TestCheckoutRecordsTheAnnouncementAndClearsTheBasket(t *testing.T) {
	t.Parallel()

	// Kept rather than rebuilt: sampleCart draws fresh product ids each call.
	basket := sampleCart("mustafa")
	basket.Version = uuid.New()
	repo := newFakeRepository(basket)
	handler := carts.NewCheckoutBasketHandler(repo)

	result, err := handler.Handle(t.Context(), checkoutCommand("mustafa"))
	if err != nil {
		t.Fatalf("checking out: %v", err)
	}
	if !result.IsSuccess {
		t.Error("the checkout reported no success")
	}

	if len(repo.recorded) != 1 {
		t.Fatalf("recorded %d announcements, want one", len(repo.recorded))
	}
	announcement := repo.recorded[0]
	if announcement.Contract != (basketv1.CheckoutSubmitted{}).Contract() {
		t.Errorf("contract = %v, want basket.checkout-submitted v1", announcement.Contract)
	}
	// A basket belongs to a user, so the user is what the message is keyed by.
	if announcement.AggregateID != "mustafa" {
		t.Errorf("aggregate id = %q, want the user name", announcement.AggregateID)
	}

	var event basketv1.CheckoutSubmitted
	if err := messaging.Unmarshal(announcement.Payload, &event); err != nil {
		t.Fatalf("decoding the payload: %v", err)
	}
	// The order is built from what was in the basket, so every line has to
	// travel with the message, names included.
	if len(event.Items) != len(basket.Items) {
		t.Fatalf("items = %d, want the %d basket lines", len(event.Items), len(basket.Items))
	}
	for i, line := range event.Items {
		item := basket.Items[i]
		if line.ProductID != item.ProductID || line.ProductName != item.ProductName ||
			line.Quantity != item.Quantity || !line.UnitPrice.Equal(item.Price) {
			t.Errorf("item %d = %+v, want %+v", i, line, item)
		}
	}
	if !event.TotalPrice.Equal(basket.TotalPrice()) {
		t.Errorf("total = %s, want the basket total", event.TotalPrice)
	}
	want := basketv1.Payment{PaymentToken: "pt_0123456789abcdef0123456789abcdef", CardLast4: "4444", PaymentMethod: 1}
	if event.Payment != want {
		t.Errorf("payment = %+v, want %+v", event.Payment, want)
	}
	if event.ShippingAddress.AddressLine != "Test Address" || event.BillingAddress != event.ShippingAddress {
		t.Errorf("addresses = %+v / %+v, want the checkout address for both",
			event.ShippingAddress, event.BillingAddress)
	}

	// The basket is gone: one that survived would be checked out again.
	if _, err := repo.GetByUserName(t.Context(), "mustafa"); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("the basket survived the checkout (error %v)", err)
	}
}

func TestTheSameBasketCheckedOutTwiceKeepsOneMessageID(t *testing.T) {
	t.Parallel()

	version := uuid.New()

	first := carts.CheckoutID("mustafa", version)
	again := carts.CheckoutID("mustafa", version)
	// Two requests racing on the same basket derive the same id, so the
	// second records nothing new and the customer gets one order.
	if first != again {
		t.Errorf("the same basket derived %s and %s", first, again)
	}

	// A basket filled again afterwards is a different basket. If it derived
	// the old id, a consumer deduplicating on it would drop the new order as
	// a repeat of the old one.
	if refilled := carts.CheckoutID("mustafa", uuid.New()); refilled == first {
		t.Error("a new basket version should derive a new id")
	}
	if other := carts.CheckoutID("someone-else", version); other == first {
		t.Error("another user's basket should derive a different id")
	}
}

func TestTheRecordedIDIsDerivedFromTheBasket(t *testing.T) {
	t.Parallel()

	basket := sampleCart("mustafa")
	basket.Version = uuid.New()
	repo := newFakeRepository(basket)
	handler := carts.NewCheckoutBasketHandler(repo)

	if _, err := handler.Handle(t.Context(), checkoutCommand("mustafa")); err != nil {
		t.Fatalf("checking out: %v", err)
	}

	// The recorded id is what reaches the broker as the message id, so the
	// derivation has to survive all the way into the outbox row.
	want := carts.CheckoutID("mustafa", basket.Version)
	if repo.recorded[0].ID != want {
		t.Errorf("recorded id = %s, want %s", repo.recorded[0].ID, want)
	}
}

func TestAFailedCheckoutLeavesTheBasketAlone(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(sampleCart("mustafa"))
	repo.failCheckout = errors.New("the database is unreachable")
	handler := carts.NewCheckoutBasketHandler(repo)

	if _, err := handler.Handle(t.Context(), checkoutCommand("mustafa")); err == nil {
		t.Fatal("a checkout that could not be recorded should fail")
	}

	// Clearing a basket whose announcement was not recorded would lose the
	// order outright, which is worse than asking the customer to try again.
	if _, err := repo.GetByUserName(t.Context(), "mustafa"); err != nil {
		t.Errorf("the basket was cleared even though nothing was recorded: %v", err)
	}
}

func TestCheckingOutAnUnknownBasketIsNotFound(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	handler := carts.NewCheckoutBasketHandler(repo)

	_, err := handler.Handle(t.Context(), checkoutCommand("nobody"))

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Fatalf("kind = %v, want KindNotFound (error %v)", got, err)
	}
	if len(repo.recorded) != 0 {
		t.Error("nothing should be recorded for a basket that does not exist")
	}
}

func TestCheckingOutAnEmptyBasketIsRefused(t *testing.T) {
	t.Parallel()

	empty := carts.ShoppingCart{UserName: "mustafa", Items: []carts.ShoppingCartItem{}}
	repo := newFakeRepository(empty)
	handler := carts.NewCheckoutBasketHandler(repo)

	_, err := handler.Handle(t.Context(), checkoutCommand("mustafa"))

	// An order with no lines has nothing to fulfil and a total of zero.
	if got := apperr.KindOf(err); got != apperr.KindBadRequest {
		t.Fatalf("kind = %v, want KindBadRequest (error %v)", got, err)
	}
	if len(repo.recorded) != 0 {
		t.Error("an empty basket should not be announced")
	}
}

func TestTheRecordedMessageCarriesNoCardDetails(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(sampleCart("mustafa"))
	handler := carts.NewCheckoutBasketHandler(repo)

	if _, err := handler.Handle(t.Context(), checkoutCommand("mustafa")); err != nil {
		t.Fatalf("checking out: %v", err)
	}

	// The command has nowhere to put card details, so this is really a guard
	// against someone adding one later. The last four digits are allowed.
	payload := strings.ToLower(string(repo.recorded[0].Payload))
	for _, banned := range []string{"cardnumber", "cardname", "expir", "cvv", "cvc"} {
		if strings.Contains(payload, banned) {
			t.Errorf("the recorded payload mentions %q: %s", banned, payload)
		}
	}
}

func TestABasketWithoutAVersionIsNotCheckedOut(t *testing.T) {
	t.Parallel()

	// What a reader holds when a concurrent checkout removed the basket after
	// it was loaded: deriving an id from the empty version would record a
	// second checkout of the same basket.
	gone := sampleCart("mustafa")
	gone.Version = uuid.Nil
	repo := newFakeRepository(gone)
	handler := carts.NewCheckoutBasketHandler(repo)

	_, err := handler.Handle(t.Context(), checkoutCommand("mustafa"))

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Fatalf("kind = %v, want KindNotFound (error %v)", got, err)
	}
	if len(repo.recorded) != 0 {
		t.Error("nothing should be recorded for a basket with no version")
	}
}

func TestACheckoutNeedsAPaymentToken(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(*carts.CheckoutBasketCommand){
		"no token":           func(c *carts.CheckoutBasketCommand) { c.PaymentToken = "" },
		"three digits":       func(c *carts.CheckoutBasketCommand) { c.CardLast4 = "444" },
		"letters for digits": func(c *carts.CheckoutBasketCommand) { c.CardLast4 = "44a4" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command := checkoutCommand("mustafa")
			change(&command)

			if got := apperr.KindOf(validation.Struct(command)); got != apperr.KindValidation {
				t.Errorf("kind = %v, want KindValidation", got)
			}
		})
	}
}
