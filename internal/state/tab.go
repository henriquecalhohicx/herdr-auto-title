// Package state holds Auto Title's in-memory view of the Herdr session.
//
// The snapshot seeds it once; afterwards it is updated only from events.
package state

import (
	"sort"
	"time"
)

// PaneState is the cached context of one pane.
type PaneState struct {
	ID            string
	TabID         string
	CWD           string
	ForegroundCWD string
	TerminalTitle string
	Agent         string
	AgentSession  string
	AgentStatus   string
	Focused       bool
	Revision      uint64
	UpdatedAt     time.Time
}

// Clone returns an independent copy, so callers can read pane context without
// holding the cache lock.
func (p *PaneState) Clone() *PaneState {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// TabState is the cached state of one tab.
type TabState struct {
	ID          string
	WorkspaceID string
	CurrentName string
	Panes       map[string]*PaneState

	// ManualName marks a tab the user renamed by hand. Manual rename detection
	// is not implemented yet; the flag is honoured wherever it is read.
	ManualName bool

	Revision         uint64
	LastReconciledAt time.Time
}

// Clone returns a deep copy of the tab and its panes.
func (t *TabState) Clone() TabState {
	c := *t
	c.Panes = make(map[string]*PaneState, len(t.Panes))
	for id, p := range t.Panes {
		c.Panes[id] = p.Clone()
	}
	return c
}

// SortedPanes returns the tab's panes ordered by ID, giving every traversal a
// stable order regardless of map iteration.
func (t TabState) SortedPanes() []*PaneState {
	panes := make([]*PaneState, 0, len(t.Panes))
	for _, p := range t.Panes {
		panes = append(panes, p)
	}
	sort.Slice(panes, func(i, j int) bool { return panes[i].ID < panes[j].ID })
	return panes
}

// SelectContextPane picks the pane that provides the tab's primary context.
//
// The order is: the focused pane, then the most recently updated pane, then the
// first pane by ID. Ties break on pane ID so that identical state always yields
// the same choice. A later slice inserts "the active pane running an agent"
// between the first two rungs.
func SelectContextPane(tab TabState) *PaneState {
	panes := tab.SortedPanes()
	if len(panes) == 0 {
		return nil
	}

	for _, p := range panes {
		if p.Focused {
			return p
		}
	}

	best := panes[0]
	for _, p := range panes[1:] {
		if p.UpdatedAt.After(best.UpdatedAt) {
			best = p
		}
	}
	return best
}
