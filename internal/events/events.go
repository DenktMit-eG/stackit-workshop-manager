// Package events is a tiny in-process broadcaster used to push state changes to web clients.
package events

import "sync"

// Event names something that changed.
type Event struct {
	Kind string // "users", "mail", "job"
	Slug string // affected user, "" for all
}

// Bus fans out events to subscribers. Slow subscribers drop events (they re-render anyway).
type Bus struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// New creates a Bus.
func New() *Bus { return &Bus{subs: map[chan Event]struct{}{}} }

// Subscribe returns a channel receiving events and a function to unsubscribe.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Publish delivers e to all subscribers without blocking.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
