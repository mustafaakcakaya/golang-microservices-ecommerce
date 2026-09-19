package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DefaultLimit and DefaultWindow are the shape of the limit unless configured
// otherwise: a burst of five in ten seconds.
const (
	DefaultLimit  = 5
	DefaultWindow = 10 * time.Second
)

// FixedWindow allows a number of requests per caller per window.
//
// Per caller rather than per gateway: one shared counter would let a single
// busy client lock everyone else out, which is the opposite of what a limit is
// for. The window is fixed rather than sliding - simple to explain and cheap to
// keep - at the cost of allowing up to twice the limit across a boundary.
type FixedWindow struct {
	limit  int
	window time.Duration

	mu        sync.Mutex
	callers   map[string]*counter
	lastSweep time.Time
}

// counter is one caller's use of the current window.
type counter struct {
	count     int
	windowEnd time.Time
}

// NewFixedWindow wires the limiter. A non-positive limit or window falls back
// to the defaults.
func NewFixedWindow(limit int, window time.Duration) *FixedWindow {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if window <= 0 {
		window = DefaultWindow
	}

	return &FixedWindow{
		limit:     limit,
		window:    window,
		callers:   make(map[string]*counter),
		lastSweep: time.Now(),
	}
}

// Middleware turns away callers that are over the limit.
func (f *FixedWindow) Middleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			caller := callerOf(r)

			if allowed, retryAfter := f.allow(caller, time.Now()); !allowed {
				log.WarnContext(r.Context(), "a caller is over the rate limit",
					"caller", caller, "path", r.URL.Path)

				// Says when it is worth trying again, so a client can wait
				// rather than hammer.
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds()+0.5)))
				problem(w, r, http.StatusTooManyRequests, "TooManyRequests",
					"too many requests; slow down and try again shortly")

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// allow records one request and reports whether it may proceed.
func (f *FixedWindow) allow(caller string, now time.Time) (bool, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sweep(now)

	current, seen := f.callers[caller]
	if !seen || now.After(current.windowEnd) {
		f.callers[caller] = &counter{count: 1, windowEnd: now.Add(f.window)}
		return true, 0
	}

	if current.count >= f.limit {
		return false, current.windowEnd.Sub(now)
	}

	current.count++

	return true, 0
}

// sweep drops callers whose window has passed.
//
// Without it the map grows with every address ever seen, which is a slow leak
// on a public edge and a cheap way for one caller to spend the gateway's
// memory.
func (f *FixedWindow) sweep(now time.Time) {
	if now.Sub(f.lastSweep) < f.window {
		return
	}

	for caller, current := range f.callers {
		if now.After(current.windowEnd) {
			delete(f.callers, caller)
		}
	}

	f.lastSweep = now
}

// callerOf identifies the client by the address it connected from.
//
// Deliberately not X-Forwarded-For: the gateway is the edge, and a caller can
// put anything in that header - trusting it would let anyone raise their own
// limit by inventing a new address per request.
func callerOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// writeJSON encodes a response body.
func writeJSON(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}
