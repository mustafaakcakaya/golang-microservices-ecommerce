package carts_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/basket/carts"
)

func lineWith(name string, price int64, quantity int) carts.ShoppingCartItem {
	return carts.ShoppingCartItem{ProductName: name, Price: decimal.NewFromInt(price), Quantity: quantity}
}

func TestStoreAppliesDiscountBeforePersisting(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	discounts := &fakeDiscounts{amounts: map[string]int32{"iPhone X": 150, "Samsung 10": 100}}
	handler := carts.NewStoreBasketHandler(repo, discounts)

	_, err := handler.Handle(context.Background(), carts.StoreBasketCommand{
		Cart: carts.ShoppingCart{
			UserName: "mustafa",
			Items:    []carts.ShoppingCartItem{lineWith("iPhone X", 950, 2), lineWith("Samsung 10", 840, 1)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repo.GetByUserName(context.Background(), "mustafa")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	// What is stored must be what the customer is charged, so the discount is
	// applied before the write rather than at read time.
	if !stored.Items[0].Price.Equal(decimal.NewFromInt(800)) {
		t.Errorf("first line = %s, want 950-150=800", stored.Items[0].Price)
	}
	if !stored.Items[1].Price.Equal(decimal.NewFromInt(740)) {
		t.Errorf("second line = %s, want 840-100=740", stored.Items[1].Price)
	}
	// Total reflects quantity: 800*2 + 740*1.
	if want := decimal.NewFromInt(2340); !stored.TotalPrice().Equal(want) {
		t.Errorf("total = %s, want %s", stored.TotalPrice(), want)
	}
}

func TestProductsWithoutCouponsKeepTheirPrice(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	discounts := &fakeDiscounts{amounts: map[string]int32{"iPhone X": 150}}
	handler := carts.NewStoreBasketHandler(repo, discounts)

	_, err := handler.Handle(context.Background(), carts.StoreBasketCommand{
		Cart: carts.ShoppingCart{
			UserName: "mustafa",
			Items:    []carts.ShoppingCartItem{lineWith("Huawei Plus", 650, 1)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, _ := repo.GetByUserName(context.Background(), "mustafa")
	// The Discount service answers zero for unknown products, so this is an
	// ordinary result, not an error path.
	if !stored.Items[0].Price.Equal(decimal.NewFromInt(650)) {
		t.Errorf("price = %s, want it unchanged at 650", stored.Items[0].Price)
	}
}

func TestDiscountLargerThanPriceClampsAtZero(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	discounts := &fakeDiscounts{amounts: map[string]int32{"Pen": 50}}
	handler := carts.NewStoreBasketHandler(repo, discounts)

	_, err := handler.Handle(context.Background(), carts.StoreBasketCommand{
		Cart: carts.ShoppingCart{
			UserName: "mustafa",
			Items:    []carts.ShoppingCartItem{lineWith("Pen", 10, 1)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, _ := repo.GetByUserName(context.Background(), "mustafa")
	// The .NET handler subtracts without a guard, which would store -40 here
	// and produce a negative basket total.
	if !stored.Items[0].Price.IsZero() {
		t.Errorf("price = %s, want 0 rather than a negative line", stored.Items[0].Price)
	}
	if stored.TotalPrice().IsNegative() {
		t.Errorf("total = %s, want it non-negative", stored.TotalPrice())
	}
}

func TestDiscountFailureLeavesTheBasketUnstored(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	discounts := &fakeDiscounts{amounts: map[string]int32{}, failOn: errors.New("discount service is down")}
	handler := carts.NewStoreBasketHandler(repo, discounts)

	_, err := handler.Handle(context.Background(), carts.StoreBasketCommand{
		Cart: carts.ShoppingCart{
			UserName: "mustafa",
			Items:    []carts.ShoppingCartItem{lineWith("iPhone X", 950, 1)},
		},
	})

	if err == nil {
		t.Fatal("a discount lookup failure must fail the request")
	}
	// Storing undiscounted prices would silently overcharge, so nothing is
	// written and the caller can retry.
	if len(repo.items) != 0 {
		t.Errorf("basket was stored despite the failure: %+v", repo.items)
	}
}

func TestDiscountIsLookedUpOncePerLine(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	discounts := &fakeDiscounts{amounts: map[string]int32{}}
	handler := carts.NewStoreBasketHandler(repo, discounts)

	_, err := handler.Handle(context.Background(), carts.StoreBasketCommand{
		Cart: carts.ShoppingCart{
			UserName: "mustafa",
			Items: []carts.ShoppingCartItem{
				lineWith("iPhone X", 950, 1),
				lineWith("Samsung 10", 840, 1),
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// One round trip per line: the contract has no batch RPC, so this is the
	// cost to be aware of, and the test pins it.
	if len(discounts.calls) != 2 {
		t.Errorf("lookups = %v, want one per line", discounts.calls)
	}
}

func TestCallerCartIsNotMutated(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	discounts := &fakeDiscounts{amounts: map[string]int32{"iPhone X": 150}}
	handler := carts.NewStoreBasketHandler(repo, discounts)

	original := []carts.ShoppingCartItem{lineWith("iPhone X", 950, 1)}
	cart := carts.ShoppingCart{UserName: "mustafa", Items: original}

	if _, err := handler.Handle(context.Background(), carts.StoreBasketCommand{Cart: cart}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Discounting copies the lines; mutating the caller's slice in place would
	// make the command a surprising input/output parameter.
	if !original[0].Price.Equal(decimal.NewFromInt(950)) {
		t.Errorf("caller's line = %s, want it untouched at 950", original[0].Price)
	}
}
