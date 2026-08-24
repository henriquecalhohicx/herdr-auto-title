// Package debounce collapses bursts of events into a single delayed action.
package debounce

import (
	"sync"
	"time"
)

// DefaultDelay is how long a key stays quiet before its action runs.
const DefaultDelay = 200 * time.Millisecond

// DefaultMaxWait caps how long rearming can hold an action back.
//
// A pane running an agent emits updates every ~100ms for as long as the agent
// is working. Pure debouncing would rearm that tab's timer forever and it would
// never be titled at all, so an action always runs within MaxWait of the first
// event in a burst.
const DefaultMaxWait = time.Second

// Manager keeps one independent timer per key. Rearming a key cancels its
// pending timer, so a burst of events on one key produces exactly one action
// once the burst settles. Keys never interfere with each other, and at most one
// timer exists per key, which bounds the goroutines in flight by the number of
// live keys.
type Manager struct {
	delay   time.Duration
	maxWait time.Duration
	fire    func(key string)

	mu     sync.Mutex
	timers map[string]*entry
	closed bool

	inflight sync.WaitGroup
}

// entry is one key's armed timer and the start of its current burst.
type entry struct {
	timer      *time.Timer
	burstStart time.Time
}

// New returns a manager that calls fire(key) once a key has been quiet for
// delay, and in any case within maxWait of the first event in a burst. A
// non-positive maxWait disables the cap and debounces indefinitely.
func New(delay, maxWait time.Duration, fire func(key string)) *Manager {
	if delay <= 0 {
		delay = DefaultDelay
	}
	return &Manager{
		delay:   delay,
		maxWait: maxWait,
		fire:    fire,
		timers:  make(map[string]*entry),
	}
}

// Schedule arms or rearms the timer for key.
func (m *Manager) Schedule(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	if existing, ok := m.timers[key]; ok && existing.timer.Stop() {
		existing.timer.Reset(m.wait(existing.burstStart))
		return
	}

	// Either there was no timer, or it had already fired and its entry is
	// stale; a fresh timer starts a new burst.
	e := &entry{burstStart: time.Now()}
	e.timer = time.AfterFunc(m.delay, func() {
		m.mu.Lock()
		if m.closed || m.timers[key] != e {
			m.mu.Unlock()
			return
		}
		delete(m.timers, key)
		m.inflight.Add(1)
		m.mu.Unlock()

		defer m.inflight.Done()
		m.fire(key)
	})
	m.timers[key] = e
}

// wait returns how long to hold a rearmed key: the quiet window, shortened so
// the burst cannot outlast maxWait.
func (m *Manager) wait(burstStart time.Time) time.Duration {
	if m.maxWait <= 0 {
		return m.delay
	}
	remaining := m.maxWait - time.Since(burstStart)
	switch {
	case remaining <= 0:
		// The cap has already passed; fire on the next tick of the clock.
		return time.Nanosecond
	case remaining < m.delay:
		return remaining
	default:
		return m.delay
	}
}

// Cancel disarms the timer for key, if any. A closed tab must not fire.
func (m *Manager) Cancel(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.timers[key]; ok {
		e.timer.Stop()
		delete(m.timers, key)
	}
}

// Pending reports whether key currently has an armed timer.
func (m *Manager) Pending(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.timers[key]
	return ok
}

// Close disarms every timer and waits for actions already running. Nothing
// fires afterwards.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.inflight.Wait()
		return
	}
	m.closed = true
	for key, e := range m.timers {
		e.timer.Stop()
		delete(m.timers, key)
	}
	m.mu.Unlock()

	m.inflight.Wait()
}
