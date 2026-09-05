// Package health exposes the /health endpoint shared by every service.
//
// Each service registers the dependencies it needs (database, cache, broker)
// under a name, and the endpoint reports them individually so a failing
// dependency is identifiable rather than hidden behind a single boolean.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// CheckFunc reports whether one dependency is usable.
type CheckFunc func(ctx context.Context) error

// Status is the outcome of a single check.
type Status struct {
	Name  string `json:"name"`
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Response is the payload returned by the endpoint.
type Response struct {
	Ok     bool     `json:"ok"`
	Checks []Status `json:"checks,omitempty"`
}

// Registry collects the checks a service depends on.
type Registry struct {
	timeout time.Duration

	mu     sync.RWMutex
	names  []string // preserved so output order is stable
	checks map[string]CheckFunc
}

// NewRegistry creates an empty registry. A registry with no checks is healthy,
// which makes it a liveness probe until dependencies are added.
func NewRegistry() *Registry {
	return &Registry{
		timeout: 3 * time.Second,
		checks:  make(map[string]CheckFunc),
	}
}

// Register adds a named check. Registering the same name twice replaces it.
func (r *Registry) Register(name string, check CheckFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.checks[name]; !exists {
		r.names = append(r.names, name)
	}
	r.checks[name] = check
}

// Run executes every check and reports the combined result.
func (r *Registry) Run(ctx context.Context) Response {
	r.mu.RLock()
	names := append([]string(nil), r.names...)
	checks := make(map[string]CheckFunc, len(r.checks))
	for name, check := range r.checks {
		checks[name] = check
	}
	r.mu.RUnlock()

	response := Response{Ok: true}
	for _, name := range names {
		checkCtx, cancel := context.WithTimeout(ctx, r.timeout)
		err := checks[name](checkCtx)
		cancel()

		status := Status{Name: name, Ok: err == nil}
		if err != nil {
			status.Error = err.Error()
			response.Ok = false
		}
		response.Checks = append(response.Checks, status)
	}

	return response
}

// Handler serves the registry as JSON, returning 503 when any check fails so
// orchestrators can act on the status code alone.
func (r *Registry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		result := r.Run(req.Context())

		w.Header().Set("Content-Type", "application/json")
		if !result.Ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(result)
	}
}
