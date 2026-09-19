package messaging_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
)

// droppingSubscriber ends its subscription a few times before staying up, the
// way a broker that is restarting behaves.
type droppingSubscriber struct {
	mu        sync.Mutex
	calls     int
	dropUntil int
	blocked   chan struct{}
}

func (s *droppingSubscriber) Subscribe(ctx context.Context, _ messaging.Delivery) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()

	if call <= s.dropUntil {
		return errors.New("the broker closed the subscription")
	}

	// Stays subscribed until the service shuts down.
	close(s.blocked)
	<-ctx.Done()

	return nil
}

func (s *droppingSubscriber) Close() error { return nil }

func (s *droppingSubscriber) attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls
}

func TestASubscriptionIsReopenedAfterItDrops(t *testing.T) {
	t.Parallel()

	subscriber := &droppingSubscriber{dropUntil: 2, blocked: make(chan struct{})}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- messaging.KeepSubscribed(ctx, subscriber, nothing, slog.New(slog.DiscardHandler))
	}()

	select {
	case <-subscriber.blocked:
	case <-time.After(30 * time.Second):
		t.Fatal("the subscription was never reopened")
	}

	// A broker outage is not a reason for a service to stop: it keeps serving
	// what it can and picks the messages up when the broker is back.
	if got := subscriber.attempts(); got != 3 {
		t.Errorf("subscribed %d times, want it retried until it held", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("stopping returned %v, want a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("it did not stop when cancelled")
	}
}

func TestShuttingDownDoesNotReopenTheSubscription(t *testing.T) {
	t.Parallel()

	// Always fails; only the cancelled context should end the loop.
	subscriber := &droppingSubscriber{dropUntil: 1 << 30, blocked: make(chan struct{})}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- messaging.KeepSubscribed(ctx, subscriber, nothing, slog.New(slog.DiscardHandler))
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("stopping returned %v, want a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("it did not stop when cancelled")
	}
}

func nothing(context.Context, messaging.Envelope) error { return nil }
