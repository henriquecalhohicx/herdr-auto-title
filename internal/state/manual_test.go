package state

import (
	"os"
	"path/filepath"
	"testing"
)

func newManual(t *testing.T) *Manual {
	t.Helper()
	return LoadManual(filepath.Join(t.TempDir(), "manual-names.json"))
}

func TestTheFirstSightingNeverLocks(t *testing.T) {
	// The trap this rule exists for: on the first poll almost every tab carries
	// a label that is not yet what the resolver would produce. Locking on that
	// would claim the whole session the moment the plugin starts.
	m := newManual(t)

	if m.Observe("wE:t1", "1", "dashboard") {
		t.Error("a tab was locked on the poll that first saw it")
	}
	if m.Locked("wE:t1") {
		t.Error("the tab is locked")
	}
}

func TestARenameByTheUserLocksTheTab(t *testing.T) {
	m := newManual(t)
	m.Observe("wE:t1", "1", "dashboard")
	m.Applied("wE:t1", "dashboard")

	if !m.Observe("wE:t1", "Important work", "dashboard") {
		t.Fatal("a label the plugin neither set nor wanted was not read as the user's")
	}
	if !m.Locked("wE:t1") {
		t.Error("the tab is not locked")
	}
}

func TestARenameByThePluginDoesNotLock(t *testing.T) {
	m := newManual(t)
	m.Observe("wE:t1", "1", "dashboard")
	m.Applied("wE:t1", "dashboard")

	if m.Observe("wE:t1", "dashboard", "dashboard") {
		t.Error("the plugin's own rename was read as the user's")
	}
}

func TestALabelThatHasNotMovedIsNobodysDoing(t *testing.T) {
	m := newManual(t)
	m.Observe("wE:t1", "Important work", "dashboard")

	// Same label on the next poll: nothing happened, whatever it says.
	if m.Observe("wE:t1", "Important work", "dashboard") {
		t.Error("an unchanged label was read as a rename")
	}
}

func TestALabelMatchingWhatWeWouldSetDoesNotLock(t *testing.T) {
	// Indistinguishable from the plugin's own work, and harmless either way.
	m := newManual(t)
	m.Observe("wE:t1", "1", "dashboard")

	if m.Observe("wE:t1", "dashboard", "dashboard") {
		t.Error("a label matching the resolved one locked the tab")
	}
}

func TestLocksSurviveAReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual-names.json")

	m := LoadManual(path)
	m.Observe("wE:t1", "1", "dashboard")
	if !m.Observe("wE:t1", "Important work", "dashboard") {
		t.Fatal("the tab was not locked")
	}

	if !LoadManual(path).Locked("wE:t1") {
		t.Error("the lock did not survive a restart")
	}
}

func TestAReloadedLockIsReleasedWhenTheLabelMovedOn(t *testing.T) {
	// Herdr's tab ids belong to a session, so a stored wE:t1 may be an
	// unrelated tab by the time it is read back. Only the label makes it the
	// same tab.
	path := filepath.Join(t.TempDir(), "manual-names.json")

	m := LoadManual(path)
	m.Observe("wE:t1", "1", "dashboard")
	m.Observe("wE:t1", "Important work", "dashboard")

	reloaded := LoadManual(path)
	reloaded.Retain(map[string]string{"wE:t1": "2"})
	if reloaded.Locked("wE:t1") {
		t.Error("a lock was kept for a tab that no longer carries its name")
	}
	if LoadManual(path).Locked("wE:t1") {
		t.Error("the released lock was not written out")
	}
}

func TestRetainDropsTabsTheSessionNoLongerHolds(t *testing.T) {
	m := newManual(t)
	m.Observe("wE:t1", "1", "dashboard")
	m.Observe("wE:t1", "Important work", "dashboard")

	m.Retain(map[string]string{})
	if m.Locked("wE:t1") {
		t.Error("a closed tab is still locked")
	}

	// Its baseline went too, so a tab reusing the id starts clean.
	if m.Observe("wE:t1", "something else", "dashboard") {
		t.Error("a tab reusing the id was locked without a baseline")
	}
}

func TestAnUnreadableStoreIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manual-names.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m := LoadManual(path)
	if m.Locked("wE:t1") {
		t.Error("a corrupt store produced a lock")
	}
	// And it still works from there.
	m.Observe("wE:t1", "1", "dashboard")
	if !m.Observe("wE:t1", "Important work", "dashboard") {
		t.Error("locking stopped working after a corrupt store")
	}
}

func TestWithoutAPathLocksStayInMemory(t *testing.T) {
	m := LoadManual("")
	m.Observe("wE:t1", "1", "dashboard")

	if !m.Observe("wE:t1", "Important work", "dashboard") {
		t.Error("locking needs a file")
	}
	if !m.Locked("wE:t1") {
		t.Error("the lock was not kept")
	}
}
