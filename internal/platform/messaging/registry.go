package messaging

import (
	"fmt"
	"sync"
)

// Registry resolves a stored (event type, version) pair back to the contract
// that can hold it.
//
// Contracts register themselves explicitly rather than being discovered, so the
// set a build understands is visible in the wiring instead of depending on what
// happens to be linked in. A pair that is not registered is refused: a worker
// that forwards a message it cannot read would turn a deployment gap into
// corrupt data downstream, where it is far harder to see.
type Registry struct {
	mu        sync.RWMutex
	factories map[Contract]func() Event
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[Contract]func() Event)}
}

// Register adds a contract. The factory must return a pointer, since decoding
// writes into the value it returns.
//
// It panics on a duplicate contract: two types claiming one wire identity is a
// wiring mistake, and failing at startup beats resolving to whichever of them
// registered last.
func (r *Registry) Register(factories ...func() Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, factory := range factories {
		contract := factory().Contract()

		if _, exists := r.factories[contract]; exists {
			panic(fmt.Sprintf("messaging: contract %s is already registered", contract))
		}

		r.factories[contract] = factory
	}
}

// Knows reports whether the registry can read the contract.
func (r *Registry) Knows(contract Contract) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.factories[contract]
	return exists
}

// Decode reads an envelope's payload into its contract.
//
// The decoded value is rarely needed - a publisher usually forwards the bytes
// untouched - but decoding proves the stored payload actually matches the
// contract it claims, which turns a malformed message into a clear failure at
// the publisher rather than a puzzling one at the consumer.
func (r *Registry) Decode(envelope Envelope) (Event, error) {
	r.mu.RLock()
	factory, exists := r.factories[envelope.Contract]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf(
			"no contract registered for %s; deploy a build that knows it, "+
				"or the message will be poisoned after its retries", envelope.Contract)
	}

	event := factory()
	if err := Unmarshal(envelope.Payload, event); err != nil {
		return nil, err
	}

	return event, nil
}
