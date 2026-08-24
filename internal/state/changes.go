package state

import (
	"sync"
	"time"

	"herdr-auto-title/internal/herdr"
)

// Changes remembers when each pane last changed.
//
// This is the one thing a snapshot cannot say. It reports what every pane holds
// right now but not when that became true, and a tab with several panes and no
// focus is named after whichever pane changed most recently. Herdr's pane
// revisions are monotonic, so comparing one poll's revisions with the last
// says which panes moved.
type Changes struct {
	mu    sync.Mutex
	panes map[string]paneChange
	now   func() time.Time
}

type paneChange struct {
	revision uint64
	at       time.Time
}

// NewChanges returns an empty history.
func NewChanges() *Changes {
	return &Changes{
		panes: make(map[string]paneChange),
		now:   time.Now,
	}
}

// Observe records a poll: panes whose revision advanced changed just now, and
// panes the session no longer holds are forgotten.
func (c *Changes) Observe(panes []herdr.PaneInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	seen := make(map[string]paneChange, len(panes))
	for _, pane := range panes {
		previous, known := c.panes[pane.PaneID]
		switch {
		case !known, pane.Revision > previous.revision:
			seen[pane.PaneID] = paneChange{revision: pane.Revision, at: now}
		default:
			seen[pane.PaneID] = previous
		}
	}
	c.panes = seen
}

// ChangedAt reports when a pane was last seen to change, or the zero time for
// a pane no poll has covered yet.
func (c *Changes) ChangedAt(paneID string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.panes[paneID].at
}
