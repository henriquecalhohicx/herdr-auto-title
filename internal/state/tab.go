// Package state holds Auto Title's in-memory view of the Herdr session.
//
// The snapshot seeds it once; afterwards it is updated only from events.
package state

import (
	"sort"
	"time"

	"herdr-auto-title/internal/herdr"
)

// PaneState is the cached context of one pane.
type PaneState struct {
	ID            string
	TabID         string
	CWD           string
	ForegroundCWD string
	// TerminalTitle is Herdr's cleaned title; TerminalTitleRaw still carries
	// escapes and decorative prefixes and is only a fallback.
	TerminalTitle    string
	TerminalTitleRaw string

	// Agent is the agent Herdr recognized in the pane, empty when there is
	// none. AgentTitle is what that agent says it is working on; agents that
	// report nothing leave it empty, and many express the same thing through
	// the terminal title instead.
	Agent        string
	DisplayAgent string
	AgentTitle   string
	AgentStatus  string
	AgentSession string

	Focused   bool
	Revision  uint64
	UpdatedAt time.Time
}

// HasAgent reports whether Herdr recognizes an agent in the pane.
func (p *PaneState) HasAgent() bool {
	return p != nil && p.Agent != ""
}

// AgentIsActive reports whether the pane's agent is doing something the user
// would want to see named: running, or waiting on them. An idle or finished
// agent is no more interesting than any other pane.
func (p *PaneState) AgentIsActive() bool {
	if !p.HasAgent() {
		return false
	}
	switch p.AgentStatus {
	case herdr.AgentStatusWorking, herdr.AgentStatusBlocked:
		return true
	default:
		return false
	}
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
// The order is: the focused pane, then the pane running an active agent, then
// the most recently updated pane. Exactly one pane is chosen and the title is
// built from it alone, so two panes never blend into a name describing neither.
//
// Ties break on the most recent update and then on pane ID, so identical state
// always yields the same choice.
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

	// A split where the user left an agent running is about that agent, even
	// though the pane below it saw the last update.
	if agent := mostRecent(filter(panes, (*PaneState).AgentIsActive)); agent != nil {
		return agent
	}
	return mostRecent(panes)
}

func filter(panes []*PaneState, keep func(*PaneState) bool) []*PaneState {
	kept := make([]*PaneState, 0, len(panes))
	for _, p := range panes {
		if keep(p) {
			kept = append(kept, p)
		}
	}
	return kept
}

// mostRecent returns the last-updated pane of an ID-ordered slice. The strict
// comparison keeps the lowest ID when timestamps tie.
func mostRecent(panes []*PaneState) *PaneState {
	if len(panes) == 0 {
		return nil
	}
	best := panes[0]
	for _, p := range panes[1:] {
		if p.UpdatedAt.After(best.UpdatedAt) {
			best = p
		}
	}
	return best
}
