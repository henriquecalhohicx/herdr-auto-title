package state

import (
	"testing"
	"time"

	"herdr-auto-title/internal/herdr"
)

func TestSelectContextPanePrefersFocused(t *testing.T) {
	now := time.Now()
	tab := TabState{
		ID: "wE:t1",
		Panes: map[string]*PaneState{
			"wE:p1": {ID: "wE:p1", UpdatedAt: now},
			"wE:p2": {ID: "wE:p2", UpdatedAt: now.Add(time.Minute), Focused: true},
			"wE:p3": {ID: "wE:p3", UpdatedAt: now.Add(time.Hour)},
		},
	}

	if got := SelectContextPane(tab); got == nil || got.ID != "wE:p2" {
		t.Fatalf("selected %v, want the focused pane wE:p2", got)
	}
}

func TestSelectContextPaneFallsBackToMostRecent(t *testing.T) {
	now := time.Now()
	tab := TabState{
		ID: "wE:t1",
		Panes: map[string]*PaneState{
			"wE:p1": {ID: "wE:p1", UpdatedAt: now},
			"wE:p2": {ID: "wE:p2", UpdatedAt: now.Add(time.Hour)},
			"wE:p3": {ID: "wE:p3", UpdatedAt: now.Add(time.Minute)},
		},
	}

	if got := SelectContextPane(tab); got == nil || got.ID != "wE:p2" {
		t.Fatalf("selected %v, want the most recently updated pane wE:p2", got)
	}
}

func TestSelectContextPaneBreaksTiesOnID(t *testing.T) {
	stamp := time.Now()
	tab := TabState{
		ID: "wE:t1",
		Panes: map[string]*PaneState{
			"wE:p3": {ID: "wE:p3", UpdatedAt: stamp},
			"wE:p1": {ID: "wE:p1", UpdatedAt: stamp},
			"wE:p2": {ID: "wE:p2", UpdatedAt: stamp},
		},
	}

	// Map iteration order varies; the choice must not.
	for i := 0; i < 50; i++ {
		got := SelectContextPane(tab)
		if got == nil || got.ID != "wE:p1" {
			t.Fatalf("iteration %d selected %v, want wE:p1", i, got)
		}
	}
}

func TestSelectContextPaneWithoutPanes(t *testing.T) {
	if got := SelectContextPane(TabState{ID: "wE:t1"}); got != nil {
		t.Fatalf("selected %v, want nil", got)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	tab := &TabState{
		ID:          "wE:t1",
		CurrentName: "dashboard",
		Panes: map[string]*PaneState{
			"wE:p1": {ID: "wE:p1", CWD: "/work/dashboard"},
		},
	}

	clone := tab.Clone()
	clone.CurrentName = "changed"
	clone.Panes["wE:p1"].CWD = "/work/other"
	clone.Panes["wE:p2"] = &PaneState{ID: "wE:p2"}

	if tab.CurrentName != "dashboard" {
		t.Errorf("clone leaked its name into the original: %q", tab.CurrentName)
	}
	if tab.Panes["wE:p1"].CWD != "/work/dashboard" {
		t.Errorf("clone leaked pane state into the original: %q", tab.Panes["wE:p1"].CWD)
	}
	if len(tab.Panes) != 1 {
		t.Errorf("clone added a pane to the original: %d panes", len(tab.Panes))
	}
}

func TestSelectContextPanePrefersAnActiveAgent(t *testing.T) {
	now := time.Now()
	tab := TabState{
		ID: "wE:t1",
		Panes: map[string]*PaneState{
			// The agent runs in a split the user is not typing in, so a build
			// scrolling past in the pane below keeps winning on recency.
			"wE:p1": {ID: "wE:p1", UpdatedAt: now, Agent: "claude", AgentStatus: herdr.AgentStatusWorking},
			"wE:p2": {ID: "wE:p2", UpdatedAt: now.Add(time.Hour)},
		},
	}

	if got := SelectContextPane(tab); got == nil || got.ID != "wE:p1" {
		t.Fatalf("selected %v, want the agent pane wE:p1", got)
	}
}

func TestSelectContextPaneIgnoresAnIdleAgent(t *testing.T) {
	now := time.Now()
	for _, status := range []string{herdr.AgentStatusIdle, herdr.AgentStatusDone, herdr.AgentStatusUnknown} {
		t.Run(status, func(t *testing.T) {
			tab := TabState{
				ID: "wE:t1",
				Panes: map[string]*PaneState{
					"wE:p1": {ID: "wE:p1", UpdatedAt: now, Agent: "claude", AgentStatus: status},
					"wE:p2": {ID: "wE:p2", UpdatedAt: now.Add(time.Hour)},
				},
			}

			if got := SelectContextPane(tab); got == nil || got.ID != "wE:p2" {
				t.Fatalf("selected %v, want the most recently updated pane wE:p2", got)
			}
		})
	}
}

func TestSelectContextPanePrefersTheFocusedPaneOverAnAgent(t *testing.T) {
	now := time.Now()
	tab := TabState{
		ID: "wE:t1",
		Panes: map[string]*PaneState{
			"wE:p1": {ID: "wE:p1", UpdatedAt: now, Agent: "claude", AgentStatus: herdr.AgentStatusWorking},
			"wE:p2": {ID: "wE:p2", UpdatedAt: now, Focused: true},
		},
	}

	if got := SelectContextPane(tab); got == nil || got.ID != "wE:p2" {
		t.Fatalf("selected %v, want the focused pane wE:p2", got)
	}
}

func TestSelectContextPaneAmongSeveralAgents(t *testing.T) {
	now := time.Now()
	tab := TabState{
		ID: "wE:t1",
		Panes: map[string]*PaneState{
			"wE:p1": {ID: "wE:p1", UpdatedAt: now, Agent: "claude", AgentStatus: herdr.AgentStatusWorking},
			"wE:p2": {ID: "wE:p2", UpdatedAt: now.Add(time.Hour), Agent: "claude", AgentStatus: herdr.AgentStatusBlocked},
			"wE:p3": {ID: "wE:p3", UpdatedAt: now.Add(2 * time.Hour)},
		},
	}

	if got := SelectContextPane(tab); got == nil || got.ID != "wE:p2" {
		t.Fatalf("selected %v, want the most recently updated agent pane wE:p2", got)
	}
}

func TestAgentIsActiveOnANilPane(t *testing.T) {
	var pane *PaneState
	if pane.HasAgent() || pane.AgentIsActive() {
		t.Fatal("a nil pane reported an agent")
	}
}
