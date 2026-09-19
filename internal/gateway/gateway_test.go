package gateway_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/gateway"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/health"
)

// upstream stands in for a service behind the gateway and records what reached
// it.
type upstream struct {
	server *httptest.Server
	paths  chan string
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()

	up := &upstream{paths: make(chan string, 8)}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.paths <- r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]string{"forwarded": r.URL.Path})
	}))
	t.Cleanup(up.server.Close)

	return up
}

func (u *upstream) url(t *testing.T) *url.URL {
	t.Helper()

	target, err := url.Parse(u.server.URL)
	if err != nil {
		t.Fatalf("upstream url: %v", err)
	}

	return target
}

func newGateway(t *testing.T, routes []gateway.Route, limiter *gateway.FixedWindow) http.Handler {
	t.Helper()

	router, err := gateway.Router(routes, limiter, health.NewRegistry(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("building the gateway: %v", err)
	}

	return router
}

func get(t *testing.T, router http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder
}

func TestThePrefixIsTheGatewaysOwnAddressing(t *testing.T) {
	t.Parallel()

	catalog := newUpstream(t)
	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: catalog.url(t)},
	}, gateway.NewFixedWindow(100, time.Minute))

	for _, tc := range []struct{ asked, forwarded string }{
		{"/catalog-service/products", "/products"},
		{"/catalog-service/products/category/phones", "/products/category/phones"},
		// The service knows nothing about the prefix, so the bare form has to
		// arrive as a root path rather than as an empty one.
		{"/catalog-service", "/"},
	} {
		response := get(t, router, tc.asked)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tc.asked, response.Code)
		}

		select {
		case got := <-catalog.paths:
			if got != tc.forwarded {
				t.Errorf("%s arrived as %q, want %q", tc.asked, got, tc.forwarded)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s never reached the service", tc.asked)
		}
	}
}

func TestEachPrefixReachesItsOwnService(t *testing.T) {
	t.Parallel()

	catalog, basket := newUpstream(t), newUpstream(t)
	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: catalog.url(t)},
		{Prefix: "/basket-service", Target: basket.url(t)},
	}, gateway.NewFixedWindow(100, time.Minute))

	if response := get(t, router, "/basket-service/basket/swn01"); response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	select {
	case got := <-basket.paths:
		if got != "/basket/swn01" {
			t.Errorf("basket received %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("the request never reached basket")
	}

	select {
	case got := <-catalog.paths:
		t.Errorf("catalog received %q, which was addressed to basket", got)
	default:
	}
}

func TestAnUnknownPrefixIsNotRouted(t *testing.T) {
	t.Parallel()

	catalog := newUpstream(t)
	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: catalog.url(t)},
	}, gateway.NewFixedWindow(100, time.Minute))

	// Nothing is forwarded by default: only what is configured is exposed.
	if response := get(t, router, "/admin/secrets"); response.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", response.Code)
	}
}

func TestAnUnreachableServiceIsAGatewayError(t *testing.T) {
	t.Parallel()

	dead := newUpstream(t)
	target := dead.url(t)
	dead.server.Close()

	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: target},
	}, gateway.NewFixedWindow(100, time.Minute))

	response := get(t, router, "/catalog-service/products")

	// 502, not 500: the gateway is fine, the service behind it is not, and the
	// caller did nothing wrong.
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", response.Code)
	}
	if content := response.Header().Get("Content-Type"); !strings.Contains(content, "problem+json") {
		t.Errorf("content type = %q, want problem details", content)
	}

	body, _ := io.ReadAll(response.Body)
	// The reason belongs in the logs; a caller learns only that it is
	// unavailable.
	if strings.Contains(string(body), dead.server.URL) {
		t.Errorf("the response leaked the service address: %s", body)
	}
}

func TestACallerOverTheLimitIsTurnedAway(t *testing.T) {
	t.Parallel()

	catalog := newUpstream(t)
	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: catalog.url(t)},
	}, gateway.NewFixedWindow(3, time.Minute))

	for i := 1; i <= 3; i++ {
		if response := get(t, router, "/catalog-service/products"); response.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, response.Code)
		}
	}

	response := get(t, router, "/catalog-service/products")
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", response.Code)
	}
	// Tells the caller when it is worth trying again, rather than leaving them
	// to hammer.
	if response.Header().Get("Retry-After") == "" {
		t.Error("a 429 should say when to retry")
	}

	// Turned away at the edge: the service never saw the extra request.
	if len(catalog.paths) != 3 {
		t.Errorf("the service received %d requests, want 3", len(catalog.paths))
	}
}

func TestOneBusyCallerDoesNotBlockAnother(t *testing.T) {
	t.Parallel()

	catalog := newUpstream(t)
	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: catalog.url(t)},
	}, gateway.NewFixedWindow(1, time.Minute))

	from := func(address string) int {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/catalog-service/products", nil)
		request.RemoteAddr = address
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		return recorder.Code
	}

	if code := from("10.0.0.1:1234"); code != http.StatusOK {
		t.Fatalf("first caller: status = %d, want 200", code)
	}
	if code := from("10.0.0.1:5678"); code != http.StatusTooManyRequests {
		t.Errorf("same caller again: status = %d, want 429", code)
	}
	// A shared counter would let one busy client lock everyone else out.
	if code := from("10.0.0.2:1234"); code != http.StatusOK {
		t.Errorf("a different caller: status = %d, want 200", code)
	}
}

func TestTheLimitLiftsWhenTheWindowPasses(t *testing.T) {
	t.Parallel()

	catalog := newUpstream(t)
	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: catalog.url(t)},
	}, gateway.NewFixedWindow(1, 100*time.Millisecond))

	if response := get(t, router, "/catalog-service/products"); response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if response := get(t, router, "/catalog-service/products"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", response.Code)
	}

	time.Sleep(150 * time.Millisecond)

	if response := get(t, router, "/catalog-service/products"); response.Code != http.StatusOK {
		t.Errorf("after the window: status = %d, want 200", response.Code)
	}
}

func TestTheGatewayAnswersForItselfWhenServicesAreDown(t *testing.T) {
	t.Parallel()

	dead := newUpstream(t)
	target := dead.url(t)
	dead.server.Close()

	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: target},
	}, gateway.NewFixedWindow(100, time.Minute))

	// Its own health is about whether it can route, not about what it routes
	// to - otherwise one failing service would take the gateway out of the
	// load balancer along with it.
	if response := get(t, router, "/health"); response.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", response.Code)
	}
}

func TestHealthIsNotRateLimited(t *testing.T) {
	t.Parallel()

	catalog := newUpstream(t)
	router := newGateway(t, []gateway.Route{
		{Prefix: "/catalog-service", Target: catalog.url(t)},
	}, gateway.NewFixedWindow(1, time.Minute))

	if response := get(t, router, "/catalog-service/products"); response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if response := get(t, router, "/catalog-service/products"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want the caller to be over the limit", response.Code)
	}

	// A probe turned away because the gateway is busy would take it out of
	// the load balancer exactly when it is needed.
	for i := range 3 {
		if response := get(t, router, "/health"); response.Code != http.StatusOK {
			t.Fatalf("health probe %d: status = %d, want 200", i+1, response.Code)
		}
	}
}

func TestAGatewayWithNoRoutesIsRefused(t *testing.T) {
	t.Parallel()

	_, err := gateway.Router(nil, gateway.NewFixedWindow(1, time.Minute),
		health.NewRegistry(), slog.New(slog.DiscardHandler))

	if err == nil {
		t.Error("a gateway that routes nowhere should not start")
	}
}
