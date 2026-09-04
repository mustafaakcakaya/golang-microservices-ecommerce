package cqrs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
)

func TestChainRunsMiddlewareOutermostFirst(t *testing.T) {
	t.Parallel()

	var order []string

	record := func(name string) cqrs.Middleware[string, string] {
		return func(next cqrs.HandlerFunc[string, string]) cqrs.HandlerFunc[string, string] {
			return func(ctx context.Context, in string) (string, error) {
				order = append(order, "enter:"+name)
				out, err := next(ctx, in)
				order = append(order, "exit:"+name)
				return out, err
			}
		}
	}

	handler := cqrs.HandlerFunc[string, string](func(_ context.Context, in string) (string, error) {
		order = append(order, "handler")
		return in + "!", nil
	})

	got, err := cqrs.Chain[string, string](handler, record("first"), record("second"))(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hi!" {
		t.Errorf("result = %q, want %q", got, "hi!")
	}

	want := []string{"enter:first", "enter:second", "handler", "exit:second", "exit:first"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestChainWithoutMiddlewareCallsHandler(t *testing.T) {
	t.Parallel()

	handler := cqrs.HandlerFunc[int, int](func(_ context.Context, in int) (int, error) {
		return in * 2, nil
	})

	got, err := cqrs.Chain[int, int](handler)(context.Background(), 21)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Errorf("result = %d, want 42", got)
	}
}

func TestChainPropagatesHandlerError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	handler := cqrs.HandlerFunc[string, string](func(context.Context, string) (string, error) {
		return "", wantErr
	})

	passthrough := func(next cqrs.HandlerFunc[string, string]) cqrs.HandlerFunc[string, string] {
		return next
	}

	_, err := cqrs.Chain[string, string](handler, passthrough)(context.Background(), "x")
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want it to wrap %v", err, wantErr)
	}
}
