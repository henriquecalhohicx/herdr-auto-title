package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kryptamine/herdr-auto-title/internal/herdr"
	"github.com/kryptamine/herdr-auto-title/internal/resolver"
)

const testPoll = 10 * time.Millisecond

func testConfig() Config {
	return Config{Poll: testPoll, MaxLength: resolver.DefaultMaxLength}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// harness runs an App against a stubbed Herdr session.
type harness struct {
	t      *testing.T
	client *herdr.StubClient
	done   chan struct{}
	cancel context.CancelFunc

	stopped bool
}

func start(t *testing.T, tabs []herdr.TabInfo, panes []herdr.PaneInfo) *harness {
	t.Helper()
	return startWith(t, herdr.NewStub(tabs, panes))
}

// startWith runs an App against a stub the test has already prepared, which is
// how a test arranges for the very first poll to fail.
func startWith(t *testing.T, client *herdr.StubClient) *harness {
	t.Helper()

	app := New(testConfig(), discardLogger(), resolver.Default(resolver.DefaultMaxLength))

	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, client: client, done: make(chan struct{}), cancel: cancel}
	go func() { app.Run(ctx, client); close(h.done) }()

	t.Cleanup(func() { h.stop() })
	return h
}

// stop cancels the run and waits for it, failing the test if it does not
// return. Safe to call twice, so a test can stop early and leave the cleanup.
func (h *harness) stop() {
	if h.stopped {
		return
	}
	h.stopped = true

	h.cancel()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		h.t.Fatal("Run did not return after its context was cancelled")
	}
}

// awaitRenames blocks until at least n renames have been issued.
func (h *harness) awaitRenames(n int) []herdr.RenameCall {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if renames := h.client.Renames(); len(renames) >= n {
			return renames
		}
		time.Sleep(time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %d renames, saw %v", n, h.client.Renames())
	return nil
}

// awaitPolls blocks until at least n polls have happened, which is how a test
// waits for "the loop has had its chance and did nothing".
func (h *harness) awaitPolls(n int) {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.client.Polls() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %d polls, saw %d", n, h.client.Polls())
}

func TestTabsAreNamedFromTheFirstPoll(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)

	renames := h.awaitRenames(1)
	if renames[0] != (herdr.RenameCall{TabID: "wE:t1", Label: "dashboard"}) {
		t.Errorf("rename = %+v, want {wE:t1 dashboard}", renames[0])
	}
}

func TestATabAppearingLaterIsNamed(t *testing.T) {
	// Nothing announces it; the next poll simply finds it.
	h := start(t, nil, nil)
	h.awaitPolls(1)

	// A tab Herdr has just made carries its position and nothing else.
	h.client.SetTab(herdr.TabInfo{TabID: "wE:t1", Label: "1"})
	h.client.SetPane(herdr.PaneInfo{
		PaneID: "wE:p1", TabID: "wE:t1", Focused: true,
		CWD:                   "/Users/dev/work/dashboard",
		TerminalTitleStripped: "Fix OAuth redirect",
	})

	renames := h.awaitRenames(1)
	if want := "dashboard › Fix OAuth redirect"; renames[0].Label != want {
		t.Errorf("rename = %q, want %q", renames[0].Label, want)
	}
}

func TestChangedContextRetitlesTheTab(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)

	h.client.SetPane(herdr.PaneInfo{
		PaneID: "wE:p1", TabID: "wE:t1", Focused: true, Revision: 2,
		CWD: "/Users/dev/work/api",
	})

	renames := h.awaitRenames(2)
	if renames[1].Label != "api" {
		t.Errorf("rename = %q, want api", renames[1].Label)
	}
}

func TestAnUnchangedSessionIsRenamedOnce(t *testing.T) {
	// Polling would be unusable if every tick renamed. Deduplication against
	// the label the snapshot reports is what keeps the loop quiet.
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)
	h.awaitPolls(10)

	if renames := h.client.Renames(); len(renames) != 1 {
		t.Errorf("issued %v, want exactly one rename", renames)
	}
}

func TestATabAlreadyCorrectlyNamedIsLeftAlone(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "dashboard"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitPolls(5)

	if renames := h.client.Renames(); len(renames) != 0 {
		t.Errorf("issued %v, want no rename", renames)
	}
}

func TestATabWithNoContextGetsTheFallback(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", Focused: true}},
	)

	if got := h.awaitRenames(1)[0].Label; got != resolver.GenericFallback {
		t.Errorf("rename = %q, want %q", got, resolver.GenericFallback)
	}
}

func TestATabClosingMidPollIsNotFatal(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{
			{TabID: "wE:t1", Label: "1"},
			{TabID: "wE:t2", Label: "2"},
		},
		[]herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
			{PaneID: "wE:p2", TabID: "wE:t2", CWD: "/Users/dev/work/api", Focused: true},
		},
	)
	h.awaitRenames(2)

	h.client.CloseTab("wE:t1")
	h.client.ClosePane("wE:p1")
	h.client.SetPane(herdr.PaneInfo{
		PaneID: "wE:p2", TabID: "wE:t2", Focused: true, Revision: 2,
		CWD: "/Users/dev/work/billing",
	})

	renames := h.awaitRenames(3)
	if renames[2].Label != "billing" {
		t.Errorf("rename = %q, want billing", renames[2].Label)
	}
}

func TestFailedRenameIsRetriedOnTheNextPoll(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)

	h.client.SetRenameError(errors.New("herdr is busy"))
	h.awaitPolls(3)
	if renames := h.client.Renames(); len(renames) != 0 {
		t.Fatalf("issued %v while renaming was failing", renames)
	}

	h.client.SetRenameError(nil)
	if got := h.awaitRenames(1)[0].Label; got != "dashboard" {
		t.Errorf("rename = %q, want dashboard", got)
	}
}

func TestAFailedPollDoesNotStopTheLoop(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)

	h.client.SetCallError(errors.New("socket hiccup"))
	time.Sleep(10 * testPoll)
	h.client.SetCallError(nil)

	h.client.SetPane(herdr.PaneInfo{
		PaneID: "wE:p1", TabID: "wE:t1", Focused: true, Revision: 2,
		CWD: "/Users/dev/work/api",
	})

	if got := h.awaitRenames(2)[1].Label; got != "api" {
		t.Errorf("rename = %q, want api", got)
	}
}

func TestAFailingFirstPollDoesNotStopTheRun(t *testing.T) {
	// Herdr's socket can be a moment behind the plugin it launched, and a
	// plugin that gives up stays dead: the startup hook is a one-shot launch,
	// not a supervised daemon. So the first poll is treated like every other.
	client := herdr.NewStub(
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	client.SetCallError(errors.New("no such socket"))
	h := startWith(t, client)

	// Long enough for several ticks to have found the socket still shut.
	time.Sleep(10 * testPoll)
	client.SetCallError(nil)

	if got := h.awaitRenames(1)[0].Label; got != "dashboard" {
		t.Errorf("rename = %q, want dashboard once the session answered", got)
	}
}

func TestARunOfFailuresIsLoggedOnABackoff(t *testing.T) {
	// Polls run twice a second, so an hour of Herdr being down is seven
	// thousand identical warnings unless the run is allowed to double.
	var failures failureLog

	var logged []int
	for range 2000 {
		if run := failures.failed(); run > 0 {
			logged = append(logged, run)
		}
	}
	want := []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024}
	if len(logged) != len(want) {
		t.Fatalf("logged %v, want %v", logged, want)
	}
	for i, run := range want {
		if logged[i] != run {
			t.Fatalf("logged %v, want %v", logged, want)
		}
	}

	if run := failures.recovered(); run != 2000 {
		t.Errorf("recovery reported %d missed polls, want 2000", run)
	}
	if run := failures.recovered(); run != 0 {
		t.Errorf("recovery reported %d after nothing went wrong, want 0", run)
	}
}

func TestRunStopsCleanlyOnCancellation(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)

	// stop fails the test if Run does not return; there is no outcome besides
	// having returned, because Run cannot fail.
	h.stop()
}

func TestTheMostRecentlyChangedPaneNamesTheTab(t *testing.T) {
	// Neither pane is focused, so the tab is named after whichever moved last.
	// Revisions are how a poll tells that apart.
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", Revision: 1, CWD: "/Users/dev/work/dashboard"},
			{PaneID: "wE:p2", TabID: "wE:t1", Revision: 1, CWD: "/Users/dev/work/api"},
		},
	)
	h.awaitRenames(1)

	h.client.SetPane(herdr.PaneInfo{
		PaneID: "wE:p2", TabID: "wE:t1", Revision: 2, CWD: "/Users/dev/work/api",
	})

	if got := h.awaitRenames(2)[1].Label; got != "api" {
		t.Errorf("rename = %q, want api", got)
	}
}

func TestAgentContextNamesTheTab(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{
			PaneID: "wE:p1", TabID: "wE:t1", Focused: true,
			CWD:         "/Users/dev/work/dashboard",
			Agent:       "claude",
			AgentStatus: herdr.AgentStatusWorking,
			Title:       "Implement OAuth scopes",
		}},
	)

	if want := "dashboard › claude › Implement OAuth scopes"; h.awaitRenames(1)[0].Label != want {
		t.Errorf("rename = %q, want %q", h.awaitRenames(1)[0].Label, want)
	}
}

func TestARemoteSessionIsNamedAfterItsHost(t *testing.T) {
	// What is running in a pane is not in the snapshot, so this exercises the
	// extra read the poll makes for every pane.
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)

	h.client.SetProcesses("wE:p1",
		herdr.PaneProcessInfoProcess{Name: "fish", Argv: []string{"-fish"}},
		herdr.PaneProcessInfoProcess{Name: "ssh", Argv: []string{"ssh", "-p", "2222", "deploy@prod-01"}},
	)

	renames := h.awaitRenames(2)
	if want := "ssh › prod-01"; renames[1].Label != want {
		t.Errorf("rename = %q, want %q", renames[1].Label, want)
	}
}

func TestAPaneWhoseProcessesCannotBeReadIsStillNamed(t *testing.T) {
	// The pane closed between the snapshot listing it and the read of what it
	// is running; the snapshot's own context still names the tab.
	client := herdr.NewStub(
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)

	app := New(testConfig(), discardLogger(), resolver.Default(resolver.DefaultMaxLength))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { app.Run(ctx, client); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if renames := client.Renames(); len(renames) > 0 {
			if renames[0].Label != "dashboard" {
				t.Errorf("rename = %q, want dashboard", renames[0].Label)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the tab was never named")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
}

func TestAWorkspaceNameIsNotRepeatedInItsTabs(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", WorkspaceID: "wE", Label: "1"}},
		[]herdr.PaneInfo{{
			PaneID: "wE:p1", TabID: "wE:t1", Focused: true,
			CWD:                   "/Users/dev/work/dashboard",
			TerminalTitleStripped: "Fix OAuth redirect",
		}},
	)
	h.client.SetWorkspaces(herdr.WorkspaceInfo{WorkspaceID: "wE", Label: "dashboard"})

	renames := h.awaitRenames(1)
	if got := renames[len(renames)-1].Label; got != "Fix OAuth redirect" {
		t.Errorf("rename = %q, want %q", got, "Fix OAuth redirect")
	}
}

func TestARenameByTheUserTurnsAutomaticNamingOff(t *testing.T) {
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)

	h.client.SetTab(herdr.TabInfo{TabID: "wE:t1", Label: "Important work"})
	h.awaitPolls(h.client.Polls() + 3)

	// The context moves on; the tab does not.
	h.client.SetPane(herdr.PaneInfo{
		PaneID: "wE:p1", TabID: "wE:t1", Focused: true, Revision: 2,
		CWD: "/Users/dev/work/api",
	})
	h.awaitPolls(h.client.Polls() + 3)

	if renames := h.client.Renames(); len(renames) != 1 {
		t.Errorf("issued %v, want only the one before the user took the tab", renames)
	}
}

func TestThePluginsOwnRenamesDoNotLockTheTab(t *testing.T) {
	// Every rename changes a label the plugin then sees again. Reading its own
	// work as the user's would stop it naming anything after the first time.
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)

	for i, dir := range []string{"api", "billing", "dashboard"} {
		h.client.SetPane(herdr.PaneInfo{
			PaneID: "wE:p1", TabID: "wE:t1", Focused: true, Revision: uint64(i + 2),
			CWD: "/Users/dev/work/" + dir,
		})
		renames := h.awaitRenames(i + 2)
		if got := renames[len(renames)-1].Label; got != dir {
			t.Fatalf("rename = %q, want %q", got, dir)
		}
	}
}

func TestNoTabIsLockedOnTheFirstPoll(t *testing.T) {
	// Every tab starts out carrying a label that is not what the resolver
	// would produce. Locking on that would claim the session at startup.
	h := start(t,
		[]herdr.TabInfo{
			{TabID: "wE:t1", Label: "1"},
			{TabID: "wE:t2", Label: "2"},
		},
		[]herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
			{PaneID: "wE:p2", TabID: "wE:t2", CWD: "/Users/dev/work/api", Focused: true},
		},
	)

	renames := h.awaitRenames(2)
	labels := map[string]bool{renames[0].Label: true, renames[1].Label: true}
	if !labels["dashboard"] || !labels["api"] {
		t.Errorf("renames = %v, want both tabs named", renames)
	}
}

func TestATabCreatedAndNamedBeforeTheNextPollIsLeftAlone(t *testing.T) {
	// The reported failure: a tab made and named in the half-second before the
	// poll that would first see it. Auto Title never saw it carrying its
	// number, so the name on it is not Auto Title's.
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)

	h.client.SetTab(herdr.TabInfo{TabID: "wE:t9", Label: "My thing"})
	h.client.SetPane(herdr.PaneInfo{PaneID: "wE:p9", TabID: "wE:t9", CWD: "/Users/dev/work/api", Focused: true})

	h.awaitPolls(h.client.Polls() + 4)
	for _, rename := range h.client.Renames() {
		if rename.TabID == "wE:t9" {
			t.Fatalf("renamed a tab the user had already named: %+v", rename)
		}
	}
}

func TestATabCreatedWithoutANameIsNamed(t *testing.T) {
	// Herdr names a new tab after its place in the workspace, which is nobody's
	// choice. The second tab is "2" — not TabInfo.number, which counts every
	// tab the workspace has ever held.
	h := start(t,
		[]herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		[]herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	)
	h.awaitRenames(1)

	h.client.SetTab(herdr.TabInfo{TabID: "wE:t9", Label: "2"})
	h.client.SetPane(herdr.PaneInfo{PaneID: "wE:p9", TabID: "wE:t9", CWD: "/Users/dev/work/api", Focused: true})

	renames := h.awaitRenames(2)
	if got := renames[len(renames)-1]; got.TabID != "wE:t9" || got.Label != "api" {
		t.Errorf("rename = %+v, want {wE:t9 api}", got)
	}
}
