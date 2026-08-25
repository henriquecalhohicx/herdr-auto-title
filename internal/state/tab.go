// Package state turns a Herdr session snapshot into the shape the resolver
// names tabs from.
//
// Almost nothing is kept between polls. A snapshot describes the whole session
// as it is right now, so each poll builds the tabs it needs and throws them
// away again; the only thing carried forward is when each pane last changed.
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

	// Processes are the commands running in the pane, as Herdr reports them:
	// its foreground process and that process's descendants.
	Processes []Process

	Focused bool
	// ChangedAt is when a poll last saw this pane's revision advance. Snapshots
	// carry no timestamp, so this is the only ordering available when a tab
	// holds several panes and none is focused.
	ChangedAt time.Time
}

// Process is one command running in a pane. Args is its whole argument vector,
// program name included, and may be empty when Herdr could not read it.
type Process struct {
	Name string
	Args []string
}

// PaneFrom builds pane context from what a read returned.
func PaneFrom(info herdr.PaneInfo, processes []herdr.PaneProcessInfoProcess, changedAt time.Time) *PaneState {
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
		Processes:        processesFrom(processes),
		Focused:          info.Focused,
		ChangedAt:        changedAt,
	}
}

func processesFrom(processes []herdr.PaneProcessInfoProcess) []Process {
	if len(processes) == 0 {
		return nil
	}
	out := make([]Process, 0, len(processes))
	for _, p := range processes {
		out = append(out, Process{Name: p.Name, Args: p.Argv})
	}
	return out
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
	// a poll skip a rename that would change nothing.
	CurrentName string
	// WorkspaceName is the label Herdr shows above this tab. A tab does not
	// need to repeat what the workspace already says.
	WorkspaceName string
	Panes         map[string]*PaneState
}

// TabFrom builds tab state from what a read returned.
func TabFrom(info herdr.TabInfo, workspaceName string, panes []*PaneState) TabState {
	tab := TabState{
		ID:            info.TabID,
		CurrentName:   info.Label,
		WorkspaceName: workspaceName,
		Panes:         make(map[string]*PaneState, len(panes)),
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
