package state

import (
	"sync"
	"testing"
	"time"
)

func ref(paneID, tabID string, revision uint64) PaneRef {
	return PaneRef{PaneID: paneID, TabID: tabID, Revision: revision}
}

func paneIDs(t *testing.T, c *Cache, tabID string) []string {
	t.Helper()
	panes, ok := c.Panes(tabID)
	if !ok {
		t.Fatalf("tab %q is not in the index", tabID)
	}
	ids := make([]string, 0, len(panes))
	for _, pane := range panes {
		ids = append(ids, pane.PaneID)
	}
	return ids
}

func TestResetSeedsFromSnapshot(t *testing.T) {
	c := NewCache()
	c.Reset([]string{"wE:t1", "wE:t2"}, []PaneRef{
		ref("wE:p1", "wE:t1", 1),
		ref("wE:p2", "wE:t2", 1),
	})

	if got := c.TabIDs(); len(got) != 2 || got[0] != "wE:t1" || got[1] != "wE:t2" {
		t.Fatalf("tab ids = %v, want [wE:t1 wE:t2]", got)
	}
	if got := paneIDs(t, c, "wE:t2"); len(got) != 1 || got[0] != "wE:p2" {
		t.Errorf("panes of wE:t2 = %v, want [wE:p2]", got)
	}
}

func TestResetReplacesPreviousState(t *testing.T) {
	c := NewCache()
	c.Reset([]string{"wE:t1"}, []PaneRef{ref("wE:p1", "wE:t1", 1)})
	c.Reset([]string{"wE:t2"}, []PaneRef{ref("wE:p2", "wE:t2", 1)})

	if got := c.TabIDs(); len(got) != 1 || got[0] != "wE:t2" {
		t.Errorf("tab ids = %v, want [wE:t2]", got)
	}
	if got := c.TouchPane("wE:p1"); got != "" {
		t.Errorf("pane wE:p1 survived the reset as tab %q", got)
	}
}

func TestTrackPaneCreatesAMissingTab(t *testing.T) {
	// Herdr can deliver a pane before its tab.
	c := NewCache()
	if got := c.TrackPane(ref("wE:p1", "wE:t1", 1)); got != "wE:t1" {
		t.Fatalf("tracked into tab %q, want wE:t1", got)
	}
	if got := paneIDs(t, c, "wE:t1"); len(got) != 1 || got[0] != "wE:p1" {
		t.Errorf("panes = %v, want [wE:p1]", got)
	}
}

func TestTrackPaneIgnoresIncompleteIdentity(t *testing.T) {
	c := NewCache()
	if got := c.TrackPane(ref("", "wE:t1", 1)); got != "" {
		t.Errorf("a pane with no id was tracked into %q", got)
	}
	if got := c.TrackPane(ref("wE:p1", "", 1)); got != "" {
		t.Errorf("a pane with no tab was tracked into %q", got)
	}
}

func TestPaneMovingBetweenTabsLeavesTheOldOne(t *testing.T) {
	c := NewCache()
	c.TrackPane(ref("wE:p1", "wE:t1", 1))
	c.TrackPane(ref("wE:p1", "wE:t2", 2))

	if got := paneIDs(t, c, "wE:t1"); len(got) != 0 {
		t.Errorf("panes of the old tab = %v, want none", got)
	}
	if got := paneIDs(t, c, "wE:t2"); len(got) != 1 || got[0] != "wE:p1" {
		t.Errorf("panes of the new tab = %v, want [wE:p1]", got)
	}
}

func TestReplayedPaneUpdatesAreDropped(t *testing.T) {
	// Subscribing replays a backlog of old revisions. Reconciling each one
	// would spend a read on a moment that has already passed.
	c := NewCache()
	c.Reset([]string{"wE:t1"}, []PaneRef{ref("wE:p1", "wE:t1", 1122)})

	for _, revision := range []uint64{1028, 1064, 1121} {
		if got := c.TrackPane(ref("wE:p1", "wE:t1", revision)); got != "" {
			t.Errorf("revision %d scheduled tab %q, want no reconciliation", revision, got)
		}
	}
	if got := c.TrackPane(ref("wE:p1", "wE:t1", 1122)); got != "wE:t1" {
		t.Errorf("the current revision scheduled %q, want wE:t1", got)
	}
	if got := c.TrackPane(ref("wE:p1", "wE:t1", 1123)); got != "wE:t1" {
		t.Errorf("a newer revision scheduled %q, want wE:t1", got)
	}
}

func TestRemoveTabTakesItsPanes(t *testing.T) {
	c := NewCache()
	c.TrackPane(ref("wE:p1", "wE:t1", 1))
	c.RemoveTab("wE:t1")

	if _, ok := c.Panes("wE:t1"); ok {
		t.Error("the tab is still in the index")
	}
	if got := c.TouchPane("wE:p1"); got != "" {
		t.Errorf("pane wE:p1 survived its tab as %q", got)
	}
}

func TestRemovePaneNamesItsTab(t *testing.T) {
	// pane_closed carries no tab id, which is why panes are indexed both ways.
	c := NewCache()
	c.TrackPane(ref("wE:p1", "wE:t1", 1))

	if got := c.RemovePane("wE:p1"); got != "wE:t1" {
		t.Fatalf("removal returned %q, want wE:t1", got)
	}
	if got := c.RemovePane("wE:p1"); got != "" {
		t.Errorf("removing it twice returned %q, want empty", got)
	}
}

func TestTouchPaneRecordsAChange(t *testing.T) {
	// The agent events say a pane changed without saying how.
	c := NewCache()
	c.TrackPane(ref("wE:p1", "wE:t1", 1))
	before, _ := c.Panes("wE:t1")

	c.now = func() time.Time { return before[0].ChangedAt.Add(time.Minute) }
	if got := c.TouchPane("wE:p1"); got != "wE:t1" {
		t.Fatalf("touch returned %q, want wE:t1", got)
	}

	after, _ := c.Panes("wE:t1")
	if !after[0].ChangedAt.After(before[0].ChangedAt) {
		t.Error("the change was not recorded")
	}
	if got := c.TouchPane("wE:p9"); got != "" {
		t.Errorf("touching an unknown pane returned %q", got)
	}
}

func TestManualNamesAreRemembered(t *testing.T) {
	c := NewCache()
	c.AddTab("wE:t1")

	if c.HasManualName("wE:t1") {
		t.Error("a fresh tab is marked manual")
	}
	c.SetManualName("wE:t1", true)
	if !c.HasManualName("wE:t1") {
		t.Error("the manual mark was not kept")
	}
	if c.HasManualName("wE:t9") {
		t.Error("an unknown tab reports a manual name")
	}
}

func TestCacheIsSafeUnderConcurrentUse(t *testing.T) {
	c := NewCache()
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				c.TrackPane(ref("wE:p1", "wE:t1", uint64(n)))
				c.Panes("wE:t1")
				c.TabIDs()
				c.TouchPane("wE:p1")
				c.HasManualName("wE:t1")
			}
		}()
	}
	wg.Wait()
}

func TestSyncAddsWhatEventsNeverDelivered(t *testing.T) {
	c := NewCache()
	c.Reset([]string{"wE:t1"}, []PaneRef{ref("wE:p1", "wE:t1", 1)})

	c.Sync([]string{"wE:t1", "wE:t2"}, []PaneRef{
		ref("wE:p1", "wE:t1", 1),
		ref("wE:p2", "wE:t2", 1),
	})

	if got := c.TabIDs(); len(got) != 2 || got[1] != "wE:t2" {
		t.Fatalf("tab ids = %v, want [wE:t1 wE:t2]", got)
	}
	if got := paneIDs(t, c, "wE:t2"); len(got) != 1 || got[0] != "wE:p2" {
		t.Errorf("panes of wE:t2 = %v, want [wE:p2]", got)
	}
}

func TestSyncDropsWhatTheSessionNoLongerHolds(t *testing.T) {
	c := NewCache()
	c.Reset([]string{"wE:t1", "wE:t2"}, []PaneRef{
		ref("wE:p1", "wE:t1", 1),
		ref("wE:p2", "wE:t1", 1),
		ref("wE:p3", "wE:t2", 1),
	})

	c.Sync([]string{"wE:t1"}, []PaneRef{ref("wE:p1", "wE:t1", 1)})

	if got := c.TabIDs(); len(got) != 1 || got[0] != "wE:t1" {
		t.Errorf("tab ids = %v, want [wE:t1]", got)
	}
	if got := paneIDs(t, c, "wE:t1"); len(got) != 1 || got[0] != "wE:p1" {
		t.Errorf("panes = %v, want [wE:p1]", got)
	}
	if got := c.TouchPane("wE:p3"); got != "" {
		t.Errorf("a pane of a closed tab survived as %q", got)
	}
}

func TestSyncKeepsWhatTheIndexAccumulated(t *testing.T) {
	c := NewCache()
	c.Reset([]string{"wE:t1"}, []PaneRef{ref("wE:p1", "wE:t1", 7)})
	c.SetManualName("wE:t1", true)
	before := c.PaneChangedAt("wE:p1")

	// A sweep that finds nothing new must not look like a change.
	c.now = func() time.Time { return before.Add(time.Hour) }
	c.Sync([]string{"wE:t1"}, []PaneRef{ref("wE:p1", "wE:t1", 7)})

	if !c.HasManualName("wE:t1") {
		t.Error("the manual mark was lost")
	}
	if got := c.PaneChangedAt("wE:p1"); !got.Equal(before) {
		t.Errorf("changed at moved to %v, want %v", got, before)
	}

	// A revision that advanced is a real change.
	c.Sync([]string{"wE:t1"}, []PaneRef{ref("wE:p1", "wE:t1", 8)})
	if got := c.PaneChangedAt("wE:p1"); !got.After(before) {
		t.Error("an advanced revision was not recorded as a change")
	}
}
