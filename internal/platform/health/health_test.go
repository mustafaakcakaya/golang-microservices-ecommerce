package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
)

func TestHandlerReportsHealthyWhenNoChecksRegistered(t *testing.T) {
	t.Parallel()

	rec := serve(t, health.NewRegistry())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body health.Response
	decode(t, rec, &body)

	if !body.Ok {
		t.Error("empty registry should report ok, so /health works as a liveness probe")
	}
}

func TestHandlerReportsEachCheckSeparately(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry()
	registry.Register("database", func(context.Context) error { return nil })
	registry.Register("cache", func(context.Context) error { return errors.New("connection refused") })

	rec := serve(t, registry)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d when a check fails", rec.Code, http.StatusServiceUnavailable)
	}

	var body health.Response
	decode(t, rec, &body)

	if body.Ok {
		t.Error("ok = true, want false when a check fails")
	}
	if len(body.Checks) != 2 {
		t.Fatalf("got %d checks, want 2", len(body.Checks))
	}

	// Order must follow registration so output is stable across requests.
	if body.Checks[0].Name != "database" || !body.Checks[0].Ok {
		t.Errorf("first check = %+v, want healthy database", body.Checks[0])
	}
	if body.Checks[1].Name != "cache" || body.Checks[1].Ok {
		t.Errorf("second check = %+v, want failing cache", body.Checks[1])
	}
	if body.Checks[1].Error != "connection refused" {
		t.Errorf("error = %q, want the underlying reason to be reported", body.Checks[1].Error)
	}
}

func TestRegisterReplacesCheckWithSameName(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry()
	registry.Register("database", func(context.Context) error { return errors.New("stale") })
	registry.Register("database", func(context.Context) error { return nil })

	result := registry.Run(context.Background())

	if len(result.Checks) != 1 {
		t.Fatalf("got %d checks, want 1 after replacing by name", len(result.Checks))
	}
	if !result.Ok {
		t.Error("replacement check should decide the outcome")
	}
}

func serve(t *testing.T, registry *health.Registry) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	registry.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.NewDecoder(rec.Body).Decode(target); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
}
