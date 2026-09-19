package carts_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/basketv1"
)

// fakeAnnouncer stands in for the broker.
type fakeAnnouncer struct {
	published []messaging.Envelope
	failWith  error
}

func (a *fakeAnnouncer) Publish(_ context.Context, envelope messaging.Envelope) error {
	if a.failWith != nil {
		return a.failWith
	}
	a.published = append(a.published, envelope)

	return nil
}

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
	}
}

func TestCheckoutAnnouncesTheBasketAndClearsIt(t *testing.T) {
	t.Parallel()

	// Kept rather than rebuilt: sampleCart draws fresh product ids each call.
	basket := sampleCart("mustafa")
	repo := newFakeRepository(basket)
	announcer := &fakeAnnouncer{}
	handler := carts.NewCheckoutBasketHandler(repo, announcer)

	command := checkoutCommand("mustafa")
	result, err := handler.Handle(t.Context(), command)
	if err != nil {
		t.Fatalf("checking out: %v", err)
	}
	if !result.IsSuccess {
		t.Error("the checkout reported no success")
	}

	if len(announcer.published) != 1 {
		t.Fatalf("published %d messages, want one", len(announcer.published))
	}
	envelope := announcer.published[0]
	if envelope.Contract != (basketv1.CheckoutRequested{}).Contract() {
		t.Errorf("contract = %v, want basket.checkout-requested v1", envelope.Contract)
	}

	var event basketv1.CheckoutRequested
	if err := messaging.Unmarshal(envelope.Payload, &event); err != nil {
		t.Fatalf("decoding the payload: %v", err)
	}
	// The order is built from what was in the basket, so every line has to
	// travel with the message, names included.
	if len(event.Lines) != len(basket.Items) {
		t.Fatalf("lines = %d, want the %d basket lines", len(event.Lines), len(basket.Items))
	}
	for i, line := range event.Lines {
		item := basket.Items[i]
		if line.ProductID != item.ProductID || line.ProductName != item.ProductName ||
			line.Quantity != item.Quantity || !line.Price.Equal(item.Price) {
			t.Errorf("line %d = %+v, want %+v", i, line, item)
		}
	}
	if !event.TotalPrice.Equal(basket.TotalPrice()) {
		t.Errorf("total = %s, want the basket total", event.TotalPrice)
	}

	// The basket is gone: a checked-out basket that survives would be
	// checked out again.
	if _, err := repo.GetByUserName(t.Context(), "mustafa"); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("the basket survived the checkout (error %v)", err)
	}
}

func TestAFailedAnnouncementKeepsTheBasket(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(sampleCart("mustafa"))
	announcer := &fakeAnnouncer{failWith: errors.New("the broker is unreachable")}
	handler := carts.NewCheckoutBasketHandler(repo, announcer)

	if _, err := handler.Handle(t.Context(), checkoutCommand("mustafa")); err == nil {
		t.Fatal("a checkout that could not be announced should fail")
	}

	// Clearing a basket nobody was told about would lose the order outright,
	// which is worse than asking the customer to try again.
	if _, err := repo.GetByUserName(t.Context(), "mustafa"); err != nil {
		t.Errorf("the basket was cleared even though nothing was announced: %v", err)
	}
}

func TestCheckingOutAnUnknownBasketIsNotFound(t *testing.T) {
	t.Parallel()

	announcer := &fakeAnnouncer{}
	handler := carts.NewCheckoutBasketHandler(newFakeRepository(), announcer)

	_, err := handler.Handle(t.Context(), checkoutCommand("nobody"))

	if got := apperr.KindOf(err); got != apperr.KindNotFound {
		t.Fatalf("kind = %v, want KindNotFound (error %v)", got, err)
	}
	if len(announcer.published) != 0 {
		t.Error("nothing should be announced for a basket that does not exist")
	}
}

func TestCheckingOutAnEmptyBasketIsRefused(t *testing.T) {
	t.Parallel()

	empty := carts.ShoppingCart{UserName: "mustafa", Items: []carts.ShoppingCartItem{}}
	repo := newFakeRepository(empty)
	announcer := &fakeAnnouncer{}
	handler := carts.NewCheckoutBasketHandler(repo, announcer)

	_, err := handler.Handle(t.Context(), checkoutCommand("mustafa"))

	// An order with no lines has nothing to fulfil and a total of zero.
	if got := apperr.KindOf(err); got != apperr.KindBadRequest {
		t.Fatalf("kind = %v, want KindBadRequest (error %v)", got, err)
	}
	if len(announcer.published) != 0 {
		t.Error("an empty basket should not be announced")
	}
}

func TestTheAnnouncedMessageCarriesNoCardDetails(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(sampleCart("mustafa"))
	announcer := &fakeAnnouncer{}
	handler := carts.NewCheckoutBasketHandler(repo, announcer)

	if _, err := handler.Handle(t.Context(), checkoutCommand("mustafa")); err != nil {
		t.Fatalf("checking out: %v", err)
	}

	// The command has nowhere to put card details, so this is really a guard
	// against someone adding one later.
	payload := strings.ToLower(string(announcer.published[0].Payload))
	for _, banned := range []string{"card", "cvv", "cvc"} {
		if strings.Contains(payload, banned) {
			t.Errorf("the announced payload mentions %q: %s", banned, payload)
		}
	}
}
