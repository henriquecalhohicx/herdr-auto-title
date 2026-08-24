package app

import "sync"

// reconcileQueue hands debounced tabs to a fixed set of workers.
//
// A tab already waiting is not queued twice: the timer has already collapsed
// the burst, and a duplicate entry would only produce a second resolution of
// the same state. A tab is forgotten as soon as a worker takes it, so changes
// arriving during reconciliation do queue it again.
type reconcileQueue struct {
	mu      sync.Mutex
	pending map[string]struct{}
	closed  bool

	ch   chan string
	done chan struct{}
}

func newReconcileQueue(buffer int) *reconcileQueue {
	return &reconcileQueue{
		pending: make(map[string]struct{}),
		ch:      make(chan string, buffer),
		done:    make(chan struct{}),
	}
}

func (q *reconcileQueue) out() <-chan string { return q.ch }

func (q *reconcileQueue) push(key string) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	if _, duplicate := q.pending[key]; duplicate {
		q.mu.Unlock()
		return
	}
	q.pending[key] = struct{}{}
	q.mu.Unlock()

	select {
	case q.ch <- key:
	case <-q.done:
		q.forget(key)
	}
}

func (q *reconcileQueue) forget(key string) {
	q.mu.Lock()
	delete(q.pending, key)
	q.mu.Unlock()
}

// close stops accepting work and lets the workers drain what is already queued.
// It must be called only after every producer has stopped.
func (q *reconcileQueue) close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()

	close(q.done)
	close(q.ch)
}
