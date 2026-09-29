package bridge

import (
	"encoding/json"
	"sync"
)

// eventQueueCapacity bounds how many undelivered events a node may buffer.
// If the GUI stops polling (backgrounded app, frozen isolate), old events are
// dropped instead of growing memory without limit.
const eventQueueCapacity = 1024

// eventQueue is a bounded FIFO of pre-marshaled events with an oldest-first
// drop policy and a running drop counter. Safe for concurrent use: one
// producer (the node's message handler) and one consumer (poll).
type eventQueue struct {
	mu      sync.Mutex
	cap     int
	events  []map[string]any
	dropped int
}

func newEventQueue(capacity int) *eventQueue {
	return &eventQueue{cap: capacity}
}

func (q *eventQueue) push(ev map[string]any) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.events) >= q.cap {
		q.events = q.events[1:]
		q.dropped++
	}
	q.events = append(q.events, ev)
}

// drain returns every queued event (as JSON) and how many were dropped since
// the previous drain.
func (q *eventQueue) drain() ([]json.RawMessage, int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]json.RawMessage, 0, len(q.events))
	for _, ev := range q.events {
		b, err := json.Marshal(ev)
		if err != nil {
			q.dropped++
			continue
		}
		out = append(out, b)
	}
	q.events = q.events[:0]
	dropped := q.dropped
	q.dropped = 0
	return out, dropped
}

func (q *eventQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.events)
}
