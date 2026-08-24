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
