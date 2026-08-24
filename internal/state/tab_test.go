package state

import (
	"testing"
	"time"
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
