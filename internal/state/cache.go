package state

import (
	"sort"
	"sync"
	"time"

	"herdr-auto-title/internal/herdr"
)

// Cache is the concurrency-safe store of tab and pane state.
//
// Panes are indexed by tab so a closing tab takes its panes with it, and
// separately by pane ID because pane_closed events do not name the tab.
type Cache struct {
	mu      sync.RWMutex
	tabs    map[string]*TabState
	paneTab map[string]string
	now     func() time.Time
}

// NewCache returns an empty cache.
func NewCache() *Cache {
	return &Cache{
		tabs:    make(map[string]*TabState),
		paneTab: make(map[string]string),
		now:     time.Now,
	}
}

// Reset replaces the entire cache with a session snapshot. The snapshot is the
// initial state only; every later change arrives as an event.
func (c *Cache) Reset(snap herdr.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tabs = make(map[string]*TabState, len(snap.Tabs))
	c.paneTab = make(map[string]string, len(snap.Panes))

	for _, tab := range snap.Tabs {
		c.tabs[tab.TabID] = &TabState{
			ID:          tab.TabID,
			WorkspaceID: tab.WorkspaceID,
			CurrentName: tab.Label,
			Panes:       make(map[string]*PaneState),
		}
	}
	for _, pane := range snap.Panes {
		c.putPaneLocked(pane)
	}
}

// UpsertTab records a tab, preserving the state of one already cached.
func (c *Cache) UpsertTab(tab herdr.TabInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.tabs[tab.TabID]
	if !ok {
		c.tabs[tab.TabID] = &TabState{
			ID:          tab.TabID,
			WorkspaceID: tab.WorkspaceID,
			CurrentName: tab.Label,
			Panes:       make(map[string]*PaneState),
		}
		return
	}
	existing.WorkspaceID = tab.WorkspaceID
	existing.CurrentName = tab.Label
}

// RemoveTab drops a tab and every pane belonging to it.
func (c *Cache) RemoveTab(tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tab, ok := c.tabs[tabID]
	if !ok {
		return
	}
	for paneID := range tab.Panes {
		delete(c.paneTab, paneID)
	}
	delete(c.tabs, tabID)
}

// UpsertPane records pane context and returns the tab it belongs to.
func (c *Cache) UpsertPane(pane herdr.PaneInfo) (tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.putPaneLocked(pane)
}

func (c *Cache) putPaneLocked(pane herdr.PaneInfo) string {
	if pane.TabID == "" || pane.PaneID == "" {
		return ""
	}

	// A pane can move between tabs; drop it from the old one first.
	if oldTabID, ok := c.paneTab[pane.PaneID]; ok && oldTabID != pane.TabID {
		if old, ok := c.tabs[oldTabID]; ok {
			delete(old.Panes, pane.PaneID)
		}
	}

	tab, ok := c.tabs[pane.TabID]
	if !ok {
		// Herdr can deliver a pane before its tab; the tab event fills in the
		// label later.
		tab = &TabState{
			ID:          pane.TabID,
			WorkspaceID: pane.WorkspaceID,
			Panes:       make(map[string]*PaneState),
		}
		c.tabs[pane.TabID] = tab
	}

	tab.Panes[pane.PaneID] = &PaneState{
		ID:               pane.PaneID,
		TabID:            pane.TabID,
		CWD:              pane.CWD,
		ForegroundCWD:    pane.ForegroundCWD,
		TerminalTitle:    pane.TerminalTitleStripped,
		TerminalTitleRaw: pane.TerminalTitle,
		Agent:            pane.Agent,
		AgentStatus:      pane.AgentStatus,
		Focused:          pane.Focused,
		Revision:         pane.Revision,
		UpdatedAt:        c.now(),
	}
	tab.Revision++
	c.paneTab[pane.PaneID] = pane.TabID
	return pane.TabID
}

// RemovePane drops a pane and returns the tab it belonged to.
func (c *Cache) RemovePane(paneID string) (tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tabID, ok := c.paneTab[paneID]
	if !ok {
		return ""
	}
	delete(c.paneTab, paneID)
	if tab, ok := c.tabs[tabID]; ok {
		delete(tab.Panes, paneID)
		tab.Revision++
	}
	return tabID
}

// Tab returns an independent copy of a tab's state.
func (c *Cache) Tab(tabID string) (TabState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tab, ok := c.tabs[tabID]
	if !ok {
		return TabState{}, false
	}
	return tab.Clone(), true
}

// TabIDs lists the cached tabs in a stable order.
func (c *Cache) TabIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.tabs))
	for id := range c.tabs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// SetCurrentName records the label a tab now carries. Keeping this current is
// what lets reconciliation skip a rename that would be a no-op.
func (c *Cache) SetCurrentName(tabID, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if tab, ok := c.tabs[tabID]; ok {
		tab.CurrentName = name
	}
}

// SetManualName marks or clears a tab as renamed by hand. A tab carrying a
// manual name is left alone by reconciliation.
func (c *Cache) SetManualName(tabID string, manual bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if tab, ok := c.tabs[tabID]; ok {
		tab.ManualName = manual
	}
}

// MarkReconciled stamps the time a tab was last reconciled.
func (c *Cache) MarkReconciled(tabID string, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if tab, ok := c.tabs[tabID]; ok {
		tab.LastReconciledAt = at
	}
}
