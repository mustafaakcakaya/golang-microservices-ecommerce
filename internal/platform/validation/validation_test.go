package validation_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/validation"
)

type command struct {
	Name  string          `validate:"required,min=2,max=10"`
	Tags  []string        `validate:"required,min=1"`
	Price decimal.Decimal `validate:"gt=0"`
}

func valid() command {
	return command{Name: "phone", Tags: []string{"sale"}, Price: decimal.NewFromInt(5)}
}

func TestStructAcceptsValidInput(t *testing.T) {
	t.Parallel()

	if err := validation.Struct(valid()); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}
}

func TestStructReportsEveryBrokenRule(t *testing.T) {
	t.Parallel()

	err := validation.Struct(command{})

	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatalf("error = %v, want an *apperr.Error", err)
	}
	if appErr.Kind != apperr.KindValidation {
		t.Errorf("kind = %v, want KindValidation", appErr.Kind)
	}

	// A caller fixing input one round trip at a time is a poor experience;
	// all failures are reported together.
	for _, field := range []string{"Name", "Tags", "Price"} {
		if _, reported := appErr.Fields[field]; !reported {
			t.Errorf("field %s missing from %v", field, appErr.Fields)
		}
	}
}

func TestMessagesReadAsProse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input command
		field string
		want  string
	}{
		{"required", command{}, "Name", "Name is required"},
		{"min length", withName(valid(), "a"), "Name", "Name must be at least 2 characters"},
		{"max length", withName(valid(), "abcdefghijk"), "Name", "Name must be at most 10 characters"},
		{"greater than", withPrice(valid(), 0), "Price", "Price must be greater than 0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			appErr, ok := apperr.As(validation.Struct(tc.input))
			if !ok {
				t.Fatal("expected a validation error")
			}
			if got := appErr.Fields[tc.field]; got != tc.want {
				t.Errorf("message = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDecimalRulesApply guards the custom type registration: decimal.Decimal is
// a struct, so without it numeric tags would silently never fire.
func TestDecimalRulesApply(t *testing.T) {
	t.Parallel()

	if err := validation.Struct(withPrice(valid(), 0)); err == nil {
		t.Fatal("a zero price should fail gt=0")
	}
	if err := validation.Struct(withPrice(valid(), 1)); err != nil {
		t.Fatalf("a positive price should pass: %v", err)
	}
}

func withName(c command, name string) command {
	c.Name = name
	return c
}

func withPrice(c command, price int64) command {
	c.Price = decimal.NewFromInt(price)
	return c
}
