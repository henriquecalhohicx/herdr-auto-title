package state

import (
	"sort"
	"sync"
	"time"
)

// Cache is the concurrency-safe index of what the session contains.
//
// It holds identity, not content: which panes belong to which tab, when each
// pane last changed, and which tabs the user has renamed by hand. Titles and
// directories are deliberately absent — they are read from Herdr at the moment
// a tab is reconciled, so a replayed event cannot make a decision out of a past
// moment.
//
// Panes are indexed both ways, because pane_closed names only the pane.
type Cache struct {
	mu      sync.RWMutex
	tabs    map[string]*tabEntry
	paneTab map[string]string
	now     func() time.Time
}

// tabEntry is one tab's place in the index.
type tabEntry struct {
	panes map[string]*paneEntry
	// manual marks a tab the user renamed by hand. Manual rename detection is
	// not implemented yet; the flag is honoured wherever it is read.
	manual bool
}

type paneEntry struct {
	// revision is the highest revision seen for this pane. Herdr's revisions
	// are monotonic, which is what makes a replayed event recognizable.
	revision  uint64
	changedAt time.Time
}

// NewCache returns an empty index.
func NewCache() *Cache {
	return &Cache{
		tabs:    make(map[string]*tabEntry),
		paneTab: make(map[string]string),
		now:     time.Now,
	}
}

// Reset replaces the index with a session snapshot. The snapshot is the initial
// state only; every later change arrives as an event.
func (c *Cache) Reset(tabIDs []string, panes []PaneRef) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tabs = make(map[string]*tabEntry, len(tabIDs))
	c.paneTab = make(map[string]string, len(panes))

	for _, tabID := range tabIDs {
		c.tabs[tabID] = newTabEntry()
	}
	for _, pane := range panes {
		c.trackLocked(pane)
	}
}

// PaneRef is a pane's identity and revision, which is all the index keeps.
type PaneRef struct {
	PaneID   string
	TabID    string
	Revision uint64
}

// AddTab records a tab that has no panes yet.
func (c *Cache) AddTab(tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.tabs[tabID]; !ok {
		c.tabs[tabID] = newTabEntry()
	}
}

// RemoveTab drops a tab and every pane belonging to it.
func (c *Cache) RemoveTab(tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tab, ok := c.tabs[tabID]
	if !ok {
		return
	}
	for paneID := range tab.panes {
		delete(c.paneTab, paneID)
	}
	delete(c.tabs, tabID)
}

// TrackPane records that a pane belongs to a tab and changed just now, and
// returns the tab that should be reconciled.
//
// It returns "" for an update that is older than one already seen. Subscribing
// replays a backlog of pane updates — roughly the last hundred revisions of
// each pane, paced at about ten a second — and reconciling every one of them
// would spend a read on a moment that has already passed.
func (c *Cache) TrackPane(pane PaneRef) (tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.trackLocked(pane)
}

func (c *Cache) trackLocked(pane PaneRef) string {
	if pane.PaneID == "" || pane.TabID == "" {
		return ""
	}

	// A pane can move between tabs; drop it from the old one first.
	if oldTabID, ok := c.paneTab[pane.PaneID]; ok && oldTabID != pane.TabID {
		if old, ok := c.tabs[oldTabID]; ok {
			delete(old.panes, pane.PaneID)
		}
	}

	tab, ok := c.tabs[pane.TabID]
	if !ok {
		// Herdr can deliver a pane before its tab.
		tab = newTabEntry()
		c.tabs[pane.TabID] = tab
	}

	c.paneTab[pane.PaneID] = pane.TabID

	entry, ok := tab.panes[pane.PaneID]
	switch {
	case !ok:
		tab.panes[pane.PaneID] = &paneEntry{revision: pane.Revision, changedAt: c.now()}
	case pane.Revision < entry.revision:
		return ""
	case pane.Revision > entry.revision:
		entry.revision = pane.Revision
		entry.changedAt = c.now()
	}
	// A repeat of the revision already held is the tail of a replay. It is
	// worth reconciling once, but nothing about the pane changed, so the time
	// it last changed must not move.
	return pane.TabID
}

// Sync reconciles the index with a session snapshot: tabs and panes the index
// has never seen are added, ones the session no longer holds are dropped, and
// everything else keeps the history the index accumulated.
//
// Sweeping exists because the event stream runs behind. Herdr replays a backlog
// on subscribe and delivers live events only once it has drained, so a tab
// opened in the meantime is invisible to events for as long as the replay
// lasts. A snapshot always describes the present.
func (c *Cache) Sync(tabIDs []string, panes []PaneRef) {
	c.mu.Lock()
	defer c.mu.Unlock()

	live := make(map[string]struct{}, len(tabIDs))
	for _, tabID := range tabIDs {
		live[tabID] = struct{}{}
		if _, ok := c.tabs[tabID]; !ok {
			c.tabs[tabID] = newTabEntry()
		}
	}

	livePanes := make(map[string]struct{}, len(panes))
	for _, pane := range panes {
		livePanes[pane.PaneID] = struct{}{}
		// A pane the snapshot places in a tab the list did not mention still
		// belongs somewhere; trackLocked creates the tab for it.
		live[pane.TabID] = struct{}{}
		c.trackLocked(pane)
	}

	for tabID, tab := range c.tabs {
		if _, ok := live[tabID]; !ok {
			for paneID := range tab.panes {
				delete(c.paneTab, paneID)
			}
			delete(c.tabs, tabID)
			continue
		}
		for paneID := range tab.panes {
			if _, ok := livePanes[paneID]; !ok {
				delete(tab.panes, paneID)
				delete(c.paneTab, paneID)
			}
		}
	}
}

// TouchPane records that a pane changed without saying how, which is what the
// agent events carry. It returns the tab the pane belongs to.
func (c *Cache) TouchPane(paneID string) (tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tabID, ok := c.paneTab[paneID]
	if !ok {
		return ""
	}
	tab, ok := c.tabs[tabID]
	if !ok {
		return ""
	}
	entry, ok := tab.panes[paneID]
	if !ok {
		return ""
	}
	entry.changedAt = c.now()
	return tabID
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
		delete(tab.panes, paneID)
	}
	return tabID
}

// Panes lists a tab's panes, each with the time it last changed, in a stable
// order. The second result is false when the tab is not in the index.
func (c *Cache) Panes(tabID string) ([]PaneChange, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tab, ok := c.tabs[tabID]
	if !ok {
		return nil, false
	}

	panes := make([]PaneChange, 0, len(tab.panes))
	for paneID, entry := range tab.panes {
		panes = append(panes, PaneChange{PaneID: paneID, ChangedAt: entry.changedAt})
	}
	sort.Slice(panes, func(i, j int) bool { return panes[i].PaneID < panes[j].PaneID })
	return panes, true
}

// PaneChange is a pane and when Auto Title last saw it change.
type PaneChange struct {
	PaneID    string
	ChangedAt time.Time
}

// PaneChangedAt reports when a pane was last seen to change, or the zero time
// for a pane the index does not hold.
func (c *Cache) PaneChangedAt(paneID string) time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tabID, ok := c.paneTab[paneID]
	if !ok {
		return time.Time{}
	}
	tab, ok := c.tabs[tabID]
	if !ok {
		return time.Time{}
	}
	if entry, ok := tab.panes[paneID]; ok {
		return entry.changedAt
	}
	return time.Time{}
}

// TabIDs lists the indexed tabs in a stable order.
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

// SetManualName marks or clears a tab as renamed by hand. A tab carrying a
// manual name is left alone by reconciliation.
func (c *Cache) SetManualName(tabID string, manual bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if tab, ok := c.tabs[tabID]; ok {
		tab.manual = manual
	}
}

// HasManualName reports whether the user renamed this tab by hand.
func (c *Cache) HasManualName(tabID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tab, ok := c.tabs[tabID]
	return ok && tab.manual
}

func newTabEntry() *tabEntry {
	return &tabEntry{panes: make(map[string]*paneEntry)}
}
