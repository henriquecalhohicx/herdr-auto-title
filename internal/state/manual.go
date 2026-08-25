package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Manual remembers which tabs the user renamed by hand, so Auto Title stops
// naming them.
//
// There is nothing to correlate a rename with: the plugin polls rather than
// subscribing, so a rename is not an event that arrives but a label that has
// changed between two polls. A tab is the user's work when its label moved to
// something Auto Title neither set nor would have set.
//
// The first sighting of a tab never locks it. Without that rule, starting the
// plugin locks every tab whose label does not already match what the resolver
// would produce — which is most of them, and the whole session at once.
type Manual struct {
	mu   sync.Mutex
	path string
	// seen is the label each tab carried when it was last looked at, whether
	// Auto Title set it or not.
	seen map[string]string
	// locked is the label a tab carried when the user claimed it. Keeping the
	// label, rather than just the id, is what makes the locks safe to reload:
	// Herdr's tab ids are reused by the next session.
	locked map[string]string
}

// manualFile is the on-disk form. Locks outlive the process because Herdr can
// restart a plugin mid-session, and losing every manual name to that would be a
// worse surprise than the plugin briefly stopping.
type manualFile struct {
	Locked map[string]string `json:"locked_tabs"`
}

// LoadManual reads persisted locks from path, which may not exist. A file that
// cannot be read or parsed yields an empty set rather than an error: manual
// names are a convenience, and refusing to start over them would not be.
func LoadManual(path string) *Manual {
	m := &Manual{
		path:   path,
		seen:   make(map[string]string),
		locked: make(map[string]string),
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	var stored manualFile
	if json.Unmarshal(raw, &stored) != nil {
		return m
	}
	for tabID, label := range stored.Locked {
		m.locked[tabID] = label
	}
	return m
}

// DefaultManualPath is where locks are kept when nothing says otherwise.
func DefaultManualPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "herdr-auto-title", "manual-names.json")
}

// Locked reports whether the user has claimed this tab.
func (m *Manual) Locked(tabID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, locked := m.locked[tabID]
	return locked
}

// Observe records the label a poll found and reports whether the user put it
// there.
//
// It is the user's if the label moved since the last look and is not what the
// resolver would now produce. Both halves are needed: a label that has not
// moved is nobody's doing, and a label that matches what Auto Title would set
// is indistinguishable from its own work — and harmless either way.
func (m *Manual) Observe(tabID, current, desired string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	previous, known := m.seen[tabID]
	m.seen[tabID] = current

	if !known || current == previous || current == desired {
		return false
	}

	m.locked[tabID] = current
	m.saveLocked()
	return true
}

// Applied records a label Auto Title has just set, so the next poll does not
// read its own work as the user's.
func (m *Manual) Applied(tabID, label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen[tabID] = label
}

// Retain drops everything about tabs the session no longer holds, and releases
// a lock whose tab now carries a different label.
//
// The second half is what makes reloading locks safe. Herdr's tab ids belong to
// a session, so a stored `wE:t2` may be an unrelated tab by the time it is read
// back; a lock only survives if the tab still carries the name it was locked
// with.
func (m *Manual) Retain(live map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	changed := false
	for tabID, label := range m.locked {
		if current, alive := live[tabID]; !alive || current != label {
			delete(m.locked, tabID)
			changed = true
		}
	}
	for tabID := range m.seen {
		if _, alive := live[tabID]; !alive {
			delete(m.seen, tabID)
		}
	}
	if changed {
		m.saveLocked()
	}
}

// saveLocked writes the locks out. The caller holds the mutex.
//
// Writing goes through a temporary file so a crash mid-write cannot leave a
// half-written file behind. A failure is silent for the same reason a failed
// read is: this is a convenience, not the plugin's purpose.
func (m *Manual) saveLocked() {
	if m.path == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(m.path), 0o755) != nil {
		return
	}

	// Sorted keys keep the file stable, so it is readable and diffable.
	ids := make([]string, 0, len(m.locked))
	for id := range m.locked {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	stored := manualFile{Locked: make(map[string]string, len(ids))}
	for _, id := range ids {
		stored.Locked[id] = m.locked[id]
	}

	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) != nil {
		return
	}
	if os.Rename(tmp, m.path) != nil {
		_ = os.Remove(tmp)
	}
}
