// Package state turns a Herdr session snapshot into the shape the resolver
// names tabs from. Each poll builds what it needs and throws it away again.
package state

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kryptamine/herdr-auto-title/internal/git"
	"github.com/kryptamine/herdr-auto-title/internal/herdr"
)

// PaneState is one pane's context as Herdr reported it when it was last read.
type PaneState struct {
	ID string

	// Dir is the directory this pane speaks for, already chosen between the
	// shell's and the foreground process's.
	Dir string
	// TerminalTitle is Herdr's cleaned title; TerminalTitleRaw still carries
	// escapes and decorative prefixes and is only a fallback.
	TerminalTitle    string
	TerminalTitleRaw string

	// Agent is the agent Herdr recognized, empty when there is none.
	// AgentTitle is what it says it is working on, which many agents leave
	// empty and report through the terminal title instead.
	Agent        string
	DisplayAgent string
	AgentTitle   string
	AgentStatus  string

	// Processes are the pane's foreground process and its descendants.
	Processes []Process

	// Git is what the repository holding the pane's directory has checked out,
	// zero outside a repository.
	Git git.Checkout

	Focused bool
	// ChangedAt is when a poll last saw this pane's revision advance.
	// Snapshots carry no timestamp, so it is the only ordering available.
	ChangedAt time.Time
}

// Process is one command running in a pane. Args is the whole argument vector,
// program name included, and may be empty.
type Process struct {
	Name string
	Args []string
}

// PaneFrom builds pane context from what a read returned.
func PaneFrom(info herdr.PaneInfo, processes []herdr.PaneProcessInfoProcess,
	checkout git.Checkout, changedAt time.Time,
) *PaneState {
	return &PaneState{
		ID:               info.PaneID,
		Dir:              info.Dir(),
		TerminalTitle:    info.TerminalTitleStripped,
		TerminalTitleRaw: info.TerminalTitle,
		Agent:            info.Agent,
		DisplayAgent:     info.DisplayAgent,
		AgentTitle:       info.Title,
		AgentStatus:      info.AgentStatus,
		Processes:        processesFrom(processes),
		Git:              checkout,
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

// AgentIsActive reports whether the pane's agent is running or waiting on the
// user. An idle or finished one is no more interesting than any other pane.
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
	// CurrentName lets a poll skip a rename that would change nothing.
	CurrentName string
	// WorkspaceName is the label Herdr shows above this tab.
	WorkspaceName string
	// DefaultName is the label Herdr gives an unnamed tab: its place in the
	// workspace, counted from one. Not TabInfo.number, which is a counter that
	// never repeats — see docs/architecture/herdr-socket-api.md.
	DefaultName string
	Panes       map[string]*PaneState
}

// TabFrom builds tab state from what a read returned. Position is the tab's
// place in its workspace, counted from one.
func TabFrom(info herdr.TabInfo, workspaceName string, position int, panes []*PaneState) TabState {
	tab := TabState{
		ID:            info.TabID,
		CurrentName:   info.Label,
		WorkspaceName: workspaceName,
		DefaultName:   strconv.Itoa(position),
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
	slices.SortFunc(panes, func(a, b *PaneState) int { return strings.Compare(a.ID, b.ID) })
	return panes
}

// SelectContextPane picks the one pane a title is built from: the focused one,
// then one running an active agent, then whichever changed last. Ties break on
// pane ID, so identical state always yields the same choice.
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

	// A split with an agent running is about that agent, whatever moved last.
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

// mostRecent returns the last-changed pane of an ID-ordered slice; the strict
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
