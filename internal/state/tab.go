// Package state holds Auto Title's view of the Herdr session.
//
// The session snapshot seeds an index of which panes belong to which tab, and
// events keep that index current. The content of a tab — titles, directories,
// agents — is never cached: it is read back from Herdr when a tab is about to
// be renamed, because Herdr replays a backlog of events on subscribe and a
// payload therefore describes some past moment rather than the present.
package state

import (
	"sort"
	"time"

	"herdr-auto-title/internal/herdr"
)

// PaneState is one pane's context as Herdr reported it when it was last read.
type PaneState struct {
	ID string

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

	Focused bool
	// ChangedAt is when Auto Title last saw an event for this pane. Herdr's
	// reads carry no timestamp, so this is the only ordering available when a
	// tab holds several panes and none is focused.
	ChangedAt time.Time
}

// PaneFrom builds pane context from what a read returned.
func PaneFrom(info herdr.PaneInfo, changedAt time.Time) *PaneState {
	return &PaneState{
		ID:               info.PaneID,
		CWD:              info.CWD,
		ForegroundCWD:    info.ForegroundCWD,
		TerminalTitle:    info.TerminalTitleStripped,
		TerminalTitleRaw: info.TerminalTitle,
		Agent:            info.Agent,
		DisplayAgent:     info.DisplayAgent,
		AgentTitle:       info.Title,
		AgentStatus:      info.AgentStatus,
		Focused:          info.Focused,
		ChangedAt:        changedAt,
	}
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

// TabState is one tab as it was last read: its current label and its panes.
type TabState struct {
	ID string
	// CurrentName is the label the tab carries right now, which is what lets
	// reconciliation skip a rename that would change nothing.
	CurrentName string
	Panes       map[string]*PaneState
}

// TabFrom builds tab state from what a read returned.
func TabFrom(info herdr.TabInfo, panes []*PaneState) TabState {
	tab := TabState{
		ID:          info.TabID,
		CurrentName: info.Label,
		Panes:       make(map[string]*PaneState, len(panes)),
	}
	for _, pane := range panes {
		tab.Panes[pane.ID] = pane
	}
	return tab
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
// the pane that changed most recently. Exactly one pane is chosen and the title
// is built from it alone, so two panes never blend into a name describing
// neither.
//
// Ties break on the most recent change and then on pane ID, so identical state
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

// mostRecent returns the last-changed pane of an ID-ordered slice. The strict
// comparison keeps the lowest ID when timestamps tie.
func mostRecent(panes []*PaneState) *PaneState {
	if len(panes) == 0 {
		return nil
	}
	best := panes[0]
	for _, p := range panes[1:] {
		if p.ChangedAt.After(best.ChangedAt) {
			best = p
		}
	}
	return best
}
