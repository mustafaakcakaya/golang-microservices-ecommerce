// Package httpx contains the HTTP server bootstrap shared by every service.
package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// ShutdownTimeout bounds how long in-flight requests may finish during shutdown.
const ShutdownTimeout = 10 * time.Second

// Run serves handler on addr until ctx is cancelled, then drains in-flight
// requests before returning. It returns nil on a clean shutdown so callers can
// treat cancellation as success rather than failure.
func Run(ctx context.Context, addr string, handler http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", addr)
		// ErrServerClosed only means Shutdown was called; it is not a failure.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	log.Info("http server shutting down")

	// A fresh context: the parent is already cancelled, and shutdown still needs
	// a window to drain.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	return <-serveErr
}
