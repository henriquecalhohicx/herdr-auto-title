package state

import (
	"sync"
	"testing"

	"herdr-auto-title/internal/herdr"
)

func TestResetSeedsFromSnapshot(t *testing.T) {
	c := NewCache()
	c.Reset(herdr.Snapshot{
		Tabs: []herdr.TabInfo{
			{TabID: "wE:t1", WorkspaceID: "wE", Label: "1"},
			{TabID: "wE:t2", WorkspaceID: "wE", Label: "dashboard"},
		},
		Panes: []herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/work/api"},
			{PaneID: "wE:p2", TabID: "wE:t2", CWD: "/work/dashboard", Focused: true},
		},
	})

	if got := c.TabIDs(); len(got) != 2 || got[0] != "wE:t1" || got[1] != "wE:t2" {
		t.Fatalf("tab ids = %v, want [wE:t1 wE:t2]", got)
	}

	tab, ok := c.Tab("wE:t2")
	if !ok {
		t.Fatal("tab wE:t2 missing from the cache")
	}
	if tab.CurrentName != "dashboard" {
		t.Errorf("current name = %q, want %q", tab.CurrentName, "dashboard")
	}
	if pane := tab.Panes["wE:p2"]; pane == nil || pane.CWD != "/work/dashboard" {
		t.Errorf("pane wE:p2 = %+v, want cwd /work/dashboard", pane)
	}
}

func TestResetReplacesPreviousState(t *testing.T) {
	c := NewCache()
	c.Reset(herdr.Snapshot{
		Tabs:  []herdr.TabInfo{{TabID: "wE:t1", Label: "old"}},
		Panes: []herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1"}},
	})
	c.Reset(herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t2", Label: "new"}},
	})

	if _, ok := c.Tab("wE:t1"); ok {
		t.Error("stale tab survived a reset")
	}
	if tabID := c.RemovePane("wE:p1"); tabID != "" {
		t.Errorf("stale pane survived a reset, mapped to %q", tabID)
	}
}

func TestRemoveTabDropsItsPanes(t *testing.T) {
	c := NewCache()
	c.UpsertTab(herdr.TabInfo{TabID: "wE:t1", Label: "1"})
	c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1"})

	c.RemoveTab("wE:t1")

	if _, ok := c.Tab("wE:t1"); ok {
		t.Error("closed tab is still cached")
	}
	if tabID := c.RemovePane("wE:p1"); tabID != "" {
		t.Errorf("pane of a closed tab is still indexed, mapped to %q", tabID)
	}
}

func TestUpsertPaneCreatesMissingTab(t *testing.T) {
	c := NewCache()

	// Herdr can deliver a pane before the tab that owns it.
	tabID := c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/work/api"})
	if tabID != "wE:t1" {
		t.Fatalf("UpsertPane returned %q, want wE:t1", tabID)
	}

	c.UpsertTab(herdr.TabInfo{TabID: "wE:t1", Label: "1"})

	tab, ok := c.Tab("wE:t1")
	if !ok {
		t.Fatal("tab missing from the cache")
	}
	if tab.CurrentName != "1" {
		t.Errorf("current name = %q, want %q", tab.CurrentName, "1")
	}
	if len(tab.Panes) != 1 {
		t.Errorf("tab has %d panes, want 1", len(tab.Panes))
	}
}

func TestUpsertTabKeepsExistingPanes(t *testing.T) {
	c := NewCache()
	c.UpsertTab(herdr.TabInfo{TabID: "wE:t1", Label: "1"})
	c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/work/api"})
	c.UpsertTab(herdr.TabInfo{TabID: "wE:t1", Label: "renamed"})

	tab, _ := c.Tab("wE:t1")
	if len(tab.Panes) != 1 {
		t.Errorf("tab has %d panes after an update, want 1", len(tab.Panes))
	}
	if tab.CurrentName != "renamed" {
		t.Errorf("current name = %q, want %q", tab.CurrentName, "renamed")
	}
}

func TestUpsertPaneMovedBetweenTabs(t *testing.T) {
	c := NewCache()
	c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1"})
	c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t2"})

	first, _ := c.Tab("wE:t1")
	if len(first.Panes) != 0 {
		t.Errorf("pane left behind in its old tab: %d panes", len(first.Panes))
	}
	second, _ := c.Tab("wE:t2")
	if len(second.Panes) != 1 {
		t.Errorf("pane missing from its new tab: %d panes", len(second.Panes))
	}
}

func TestRemovePaneReportsItsTab(t *testing.T) {
	c := NewCache()
	c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1"})

	if tabID := c.RemovePane("wE:p1"); tabID != "wE:t1" {
		t.Errorf("RemovePane returned %q, want wE:t1", tabID)
	}
	if tabID := c.RemovePane("wE:p1"); tabID != "" {
		t.Errorf("removing a pane twice returned %q, want empty", tabID)
	}
}

func TestUpsertPaneIgnoresIncompleteIdentity(t *testing.T) {
	c := NewCache()

	if tabID := c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1"}); tabID != "" {
		t.Errorf("pane without a tab was accepted, mapped to %q", tabID)
	}
	if tabID := c.UpsertPane(herdr.PaneInfo{TabID: "wE:t1"}); tabID != "" {
		t.Errorf("pane without an id was accepted, mapped to %q", tabID)
	}
}

func TestCacheIsConcurrencySafe(t *testing.T) {
	c := NewCache()
	c.UpsertTab(herdr.TabInfo{TabID: "wE:t1", Label: "1"})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/work/api"})
				c.SetCurrentName("wE:t1", "api")
				_, _ = c.Tab("wE:t1")
				_ = c.TabIDs()
				c.RemovePane("wE:p1")
			}
		}(i)
	}
	wg.Wait()
}

func TestUpsertPaneRecordsAgentContext(t *testing.T) {
	c := NewCache()
	c.UpsertPane(herdr.PaneInfo{
		PaneID:       "wE:p1",
		TabID:        "wE:t1",
		Agent:        "claude",
		DisplayAgent: "Claude Code",
		AgentStatus:  herdr.AgentStatusWorking,
		Title:        "Implement OAuth scopes",
		AgentSession: &herdr.AgentSessionInfo{
			Source: "cli", Agent: "claude", Kind: "id", Value: "sess-42",
		},
	})

	tab, _ := c.Tab("wE:t1")
	pane := tab.Panes["wE:p1"]
	switch {
	case pane.Agent != "claude":
		t.Errorf("agent = %q, want claude", pane.Agent)
	case pane.DisplayAgent != "Claude Code":
		t.Errorf("display agent = %q, want %q", pane.DisplayAgent, "Claude Code")
	case pane.AgentTitle != "Implement OAuth scopes":
		t.Errorf("agent title = %q, want %q", pane.AgentTitle, "Implement OAuth scopes")
	case pane.AgentStatus != herdr.AgentStatusWorking:
		t.Errorf("agent status = %q, want working", pane.AgentStatus)
	case pane.AgentSession != "sess-42":
		t.Errorf("agent session = %q, want sess-42", pane.AgentSession)
	}
}

func TestUpsertPaneWithoutAnAgent(t *testing.T) {
	// Herdr sends null for every agent field of a pane running a plain shell.
	c := NewCache()
	c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", AgentStatus: herdr.AgentStatusUnknown})

	tab, _ := c.Tab("wE:t1")
	pane := tab.Panes["wE:p1"]
	if pane.HasAgent() || pane.AgentSession != "" {
		t.Fatalf("pane %+v reported an agent", pane)
	}
}

func TestSetPaneAgentDetectsAndReleases(t *testing.T) {
	c := NewCache()
	c.UpsertPane(herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/work/dashboard"})

	if got := c.SetPaneAgent(herdr.PaneAgentDetectedData{PaneID: "wE:p1", Agent: "claude"}); got != "wE:t1" {
		t.Fatalf("detection returned tab %q, want wE:t1", got)
	}
	tab, _ := c.Tab("wE:t1")
	if pane := tab.Panes["wE:p1"]; !pane.HasAgent() {
		t.Fatalf("pane %+v has no agent after detection", pane)
	}

	// The event carries nothing but the agent; the rest of the pane survives.
	if got := tab.Panes["wE:p1"].CWD; got != "/work/dashboard" {
		t.Errorf("cwd = %q, want /work/dashboard", got)
	}

	if got := c.SetPaneAgent(herdr.PaneAgentDetectedData{PaneID: "wE:p1", Released: true}); got != "wE:t1" {
		t.Fatalf("release returned tab %q, want wE:t1", got)
	}
	tab, _ = c.Tab("wE:t1")
	pane := tab.Panes["wE:p1"]
	if pane.HasAgent() || pane.AgentTitle != "" || pane.AgentStatus != herdr.AgentStatusUnknown {
		t.Fatalf("released pane still carries agent context: %+v", pane)
	}
}

func TestSetPaneAgentOnAnUnknownPane(t *testing.T) {
	c := NewCache()
	if got := c.SetPaneAgent(herdr.PaneAgentDetectedData{PaneID: "wE:p9", Agent: "claude"}); got != "" {
		t.Fatalf("returned tab %q for a pane that was never cached", got)
	}
}
