package domain

// Event is something that happened inside the domain.
//
// Events stay in-process: they carry aggregates and are not a wire contract.
// What leaves the service is an explicit, versioned integration event mapped
// from these in the application layer.
type Event interface {
	eventName() string
}

// OrderCreated is raised when an order is first created.
type OrderCreated struct {
	Order *Order
}

func (OrderCreated) eventName() string { return "OrderCreated" }

// OrderUpdated is raised when an existing order changes.
type OrderUpdated struct {
	Order *Order
}

func (OrderUpdated) eventName() string { return "OrderUpdated" }

// aggregate collects the events raised while handling a command.
//
// They are held rather than published so the caller decides when they leave:
// the repository writes them alongside the aggregate in one transaction, which
// is what makes the outbox atomic.
type aggregate struct {
	events []Event
}

// raise records an event.
func (a *aggregate) raise(event Event) {
	a.events = append(a.events, event)
}

// Events returns the events raised so far.
func (a *aggregate) Events() []Event {
	return a.events
}

// PullEvents returns the recorded events and clears them, so the same event
// cannot be published twice.
func (a *aggregate) PullEvents() []Event {
	pulled := a.events
	a.events = nil
	return pulled
}
