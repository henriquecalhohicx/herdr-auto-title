package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"herdr-auto-title/internal/herdr"
	"herdr-auto-title/internal/resolver"
)

const testDebounce = 10 * time.Millisecond

func testConfig() Config {
	return Config{
		Debounce:  testDebounce,
		MaxWait:   20 * testDebounce,
		MaxLength: resolver.DefaultMaxLength,
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// harness runs an App against a fake Herdr connection.
type harness struct {
	t      *testing.T
	app    *App
	client *herdr.FakeClient
	done   chan error
	cancel context.CancelFunc

	stopped bool
	stopErr error
}

func start(t *testing.T, snapshot herdr.Snapshot) *harness {
	t.Helper()

	client := herdr.NewFake(snapshot)
	app := New(testConfig(), discardLogger(), resolver.Default(resolver.DefaultMaxLength))

	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, app: app, client: client, done: make(chan error, 1), cancel: cancel}
	go func() { h.done <- app.Run(ctx, client) }()

	// Wait for the bootstrap to establish subscriptions before driving events.
	deadline := time.Now().Add(2 * time.Second)
	for len(client.Subscriptions()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the bootstrap to subscribe")
		}
		time.Sleep(time.Millisecond)
	}

	t.Cleanup(func() { h.stop() })
	return h
}

// stop cancels the run and reports how it ended. It is safe to call twice, so
// a test can assert on the outcome and still leave the cleanup in place.
func (h *harness) stop() error {
	if h.stopped {
		return h.stopErr
	}
	h.stopped = true

	h.cancel()
	select {
	case h.stopErr = <-h.done:
	case <-time.After(5 * time.Second):
		h.t.Fatal("Run did not return after its context was cancelled")
	}
	return h.stopErr
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

// settle waits long enough for any pending debounce to have fired.
func (h *harness) settle() {
	time.Sleep(20 * testDebounce)
}

func TestBootstrapRenamesTabsFromTheSnapshot(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t1", WorkspaceID: "wE", Label: "1"}},
		Panes: []herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", WorkspaceID: "wE", CWD: "/Users/dev/work/dashboard", Focused: true},
		},
	})

	renames := h.awaitRenames(1)
	if renames[0] != (herdr.RenameCall{TabID: "wE:t1", Label: "dashboard"}) {
		t.Errorf("rename = %+v, want {wE:t1 dashboard}", renames[0])
	}
}

func TestBootstrapSubscribesToTheExpectedEvents(t *testing.T) {
	h := start(t, herdr.Snapshot{})

	got := h.client.Subscriptions()
	want := map[string]bool{
		herdr.SubTabCreated:        false,
		herdr.SubTabClosed:         false,
		herdr.SubPaneCreated:       false,
		herdr.SubPaneUpdated:       false,
		herdr.SubPaneClosed:        false,
		herdr.SubPaneAgentDetected: false,
	}
	for _, sub := range got {
		if _, ok := want[sub.Type]; !ok {
			t.Errorf("unexpected subscription %q", sub.Type)
			continue
		}
		// A per-pane subscription would need a pane id and a connection of its
		// own; every stream Auto Title needs is global.
		if sub.PaneID != "" {
			t.Errorf("subscription %q is scoped to pane %q", sub.Type, sub.PaneID)
		}
		want[sub.Type] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("missing subscription %q", name)
		}
	}
}

func TestIdenticalTitleIssuesNoRename(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t1", Label: "dashboard"}},
		Panes: []herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
		},
	})

	h.settle()
	if renames := h.client.Renames(); len(renames) != 0 {
		t.Errorf("issued %v, want no rename for an unchanged title", renames)
	}
}

func TestRepeatedResolutionIssuesOneRename(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		Panes: []herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
		},
	})
	h.awaitRenames(1)

	// The context has not changed, so further events resolve to the same title
	// and must not produce a second rename.
	for i := 0; i < 5; i++ {
		h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
			Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
		})
		h.settle()
	}

	if renames := h.client.Renames(); len(renames) != 1 {
		t.Errorf("issued %d renames, want 1: %v", len(renames), renames)
	}
}

func TestTabAndPaneEventsProduceATitle(t *testing.T) {
	h := start(t, herdr.Snapshot{})

	h.client.Emit(herdr.EventTabCreated, herdr.TabCreatedData{
		Tab: herdr.TabInfo{TabID: "wE:t1", WorkspaceID: "wE", Label: "1"},
	})
	h.client.Emit(herdr.EventPaneCreated, herdr.PaneCreatedData{
		Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
	})

	renames := h.awaitRenames(1)
	if renames[len(renames)-1].Label != "dashboard" {
		t.Errorf("label = %q, want dashboard", renames[len(renames)-1].Label)
	}
}

func TestChangedDirectoryRetitlesTheTab(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		Panes: []herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
		},
	})
	h.awaitRenames(1)

	h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
		Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/api", Focused: true},
	})

	renames := h.awaitRenames(2)
	if renames[1].Label != "api" {
		t.Errorf("label = %q, want api", renames[1].Label)
	}
}

func TestBurstOfEventsProducesOneRename(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs:  []herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		Panes: []herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	})
	h.awaitRenames(1)

	// Ten updates arriving faster than the debounce window; only the settled
	// state should be acted on.
	for i := 0; i < 10; i++ {
		h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
			Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/api", Focused: true},
		})
	}

	h.settle()
	renames := h.client.Renames()
	if len(renames) != 2 {
		t.Fatalf("issued %d renames, want 2: %v", len(renames), renames)
	}
	if renames[1].Label != "api" {
		t.Errorf("label = %q, want api", renames[1].Label)
	}
}

func TestClosedTabIsNotRenamed(t *testing.T) {
	h := start(t, herdr.Snapshot{})

	h.client.Emit(herdr.EventTabCreated, herdr.TabCreatedData{Tab: herdr.TabInfo{TabID: "wE:t1", Label: "1"}})
	h.client.Emit(herdr.EventPaneCreated, herdr.PaneCreatedData{
		Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard"},
	})
	h.client.Emit(herdr.EventTabClosed, herdr.TabClosedData{TabID: "wE:t1", WorkspaceID: "wE"})

	h.settle()
	if renames := h.client.Renames(); len(renames) != 0 {
		t.Errorf("issued %v, want no rename for a closed tab", renames)
	}
	if _, ok := h.app.Cache().Tab("wE:t1"); ok {
		t.Error("a closed tab is still cached")
	}
}

func TestClosedPaneLeavesNoStaleContext(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs:  []herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		Panes: []herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	})
	h.awaitRenames(1)

	h.client.Emit(herdr.EventPaneClosed, herdr.PaneClosedData{PaneID: "wE:p1", WorkspaceID: "wE"})

	// With no panes left the tab has no context and falls back to the generic
	// name.
	renames := h.awaitRenames(2)
	if renames[1].Label != resolver.GenericFallback {
		t.Errorf("label = %q, want %q", renames[1].Label, resolver.GenericFallback)
	}
	tab, ok := h.app.Cache().Tab("wE:t1")
	if !ok {
		t.Fatal("the tab disappeared with its pane")
	}
	if len(tab.Panes) != 0 {
		t.Errorf("tab still holds %d panes", len(tab.Panes))
	}
}

func TestManualNameIsPreserved(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs:  []herdr.TabInfo{{TabID: "wE:t1", Label: "Important work"}},
		Panes: []herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	})
	h.app.Cache().SetManualName("wE:t1", true)

	h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
		Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/api", Focused: true},
	})

	h.settle()
	if renames := h.client.Renames(); len(renames) != 0 {
		t.Errorf("issued %v, want no rename for a manually named tab", renames)
	}
}

func TestUnknownAndMalformedEventsAreIgnored(t *testing.T) {
	h := start(t, herdr.Snapshot{})

	h.client.EmitRaw("workspace_reordered", `{"type":"workspace_reordered"}`)
	h.client.EmitRaw("some_future_event", `{"whatever":true}`)
	h.client.EmitRaw(herdr.EventPaneUpdated, `{"pane":"not an object"}`)

	// The loop must still be alive and processing.
	h.client.Emit(herdr.EventTabCreated, herdr.TabCreatedData{Tab: herdr.TabInfo{TabID: "wE:t1", Label: "1"}})
	h.client.Emit(herdr.EventPaneCreated, herdr.PaneCreatedData{
		Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard"},
	})

	renames := h.awaitRenames(1)
	if renames[0].Label != "dashboard" {
		t.Errorf("label = %q, want dashboard", renames[0].Label)
	}
}

func TestFailedRenameIsRetriedOnTheNextChange(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs:  []herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		Panes: []herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	})

	h.client.SetRenameError(errors.New("herdr says no"))
	h.settle()
	if renames := h.client.Renames(); len(renames) != 0 {
		t.Fatalf("a failed rename was recorded: %v", renames)
	}

	// The cached name must not have advanced, so the next event tries again.
	h.client.SetRenameError(nil)
	h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
		Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
	})

	renames := h.awaitRenames(1)
	if renames[0].Label != "dashboard" {
		t.Errorf("label = %q, want dashboard", renames[0].Label)
	}
}

func TestRunStopsCleanlyOnCancellation(t *testing.T) {
	h := start(t, herdr.Snapshot{})

	if err := h.stop(); err != nil {
		t.Errorf("Run returned %v on cancellation, want nil", err)
	}
}

func TestRunReportsALostConnection(t *testing.T) {
	client := herdr.NewFake(herdr.Snapshot{})
	app := New(testConfig(), discardLogger(), resolver.Default(resolver.DefaultMaxLength))

	done := make(chan error, 1)
	go func() { done <- app.Run(context.Background(), client) }()

	for len(client.Subscriptions()) == 0 {
		time.Sleep(time.Millisecond)
	}
	dropped := errors.New("socket closed")
	client.Disconnect(dropped)

	select {
	case err := <-done:
		if !errors.Is(err, dropped) {
			t.Errorf("Run returned %v, want %v", err, dropped)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the connection dropped")
	}
}

func TestBootstrapFailureIsReported(t *testing.T) {
	client := herdr.NewFake(herdr.Snapshot{})
	client.Close()

	app := New(testConfig(), discardLogger(), resolver.Default(resolver.DefaultMaxLength))
	if err := app.Run(context.Background(), client); err == nil {
		t.Error("Run succeeded despite a failed bootstrap")
	}
}

func TestTabsAreHandledIndependently(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{
			{TabID: "wE:t1", Label: "1"},
			{TabID: "wE:t2", Label: "2"},
			{TabID: "wE:t3", Label: "3"},
		},
		Panes: []herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
			{PaneID: "wE:p2", TabID: "wE:t2", CWD: "/Users/dev/work/api", Focused: true},
			{PaneID: "wE:p3", TabID: "wE:t3", CWD: "/Users/dev/work/infra", Focused: true},
		},
	})

	renames := h.awaitRenames(3)
	got := make(map[string]string, len(renames))
	for _, rename := range renames {
		got[rename.TabID] = rename.Label
	}
	want := map[string]string{"wE:t1": "dashboard", "wE:t2": "api", "wE:t3": "infra"}
	for tabID, label := range want {
		if got[tabID] != label {
			t.Errorf("tab %s named %q, want %q", tabID, got[tabID], label)
		}
	}
}

func TestBusyPaneIsStillTitled(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs:  []herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		Panes: []herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true}},
	})

	// A pane running an agent updates continuously. Reconciliation must not be
	// starved by events that keep rearming the debounce timer.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
					Pane: herdr.PaneInfo{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
				})
				time.Sleep(testDebounce / 4)
			}
		}
	}()
	defer close(stop)

	renames := h.awaitRenames(1)
	if renames[0].Label != "dashboard" {
		t.Errorf("label = %q, want dashboard", renames[0].Label)
	}
}

func TestVanishedTabIsDroppedQuietly(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs:  []herdr.TabInfo{{TabID: "wE:t9", Label: "1"}},
		Panes: []herdr.PaneInfo{{PaneID: "wE:p9", TabID: "wE:t9", CWD: "/Users/dev/work/dashboard", Focused: true}},
	})

	// A tab can close between resolution and the rename, before its tab_closed
	// event arrives.
	h.client.SetRenameError(&herdr.APIError{Code: herdr.CodeTabNotFound, Message: "tab wE:t9 not found"})
	h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
		Pane: herdr.PaneInfo{PaneID: "wE:p9", TabID: "wE:t9", CWD: "/Users/dev/work/api", Focused: true},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := h.app.Cache().Tab("wE:t9"); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a tab Herdr no longer knows about is still cached")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTerminalTitleFlowsThroughEvents(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t1", Label: "1"}},
		Panes: []herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", CWD: "/Users/dev/work/dashboard", Focused: true},
		},
	})

	renames := h.awaitRenames(1)
	if renames[0].Label != "dashboard" {
		t.Fatalf("label = %q, want dashboard", renames[0].Label)
	}

	// A program sets a meaningful title; Herdr reports it as a pane update.
	h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
		Pane: herdr.PaneInfo{
			PaneID: "wE:p1", TabID: "wE:t1", Focused: true,
			CWD:                   "/Users/dev/work/dashboard",
			TerminalTitle:         "◐ Fix OAuth redirect",
			TerminalTitleStripped: "Fix OAuth redirect",
		},
	})

	renames = h.awaitRenames(2)
	if renames[1].Label != "dashboard · Fix OAuth redirect" {
		t.Fatalf("label = %q, want %q", renames[1].Label, "dashboard · Fix OAuth redirect")
	}

	// The title goes back to something generic; the tab returns to its
	// directory rather than keeping a stale title.
	h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
		Pane: herdr.PaneInfo{
			PaneID: "wE:p1", TabID: "wE:t1", Focused: true,
			CWD:                   "/Users/dev/work/dashboard",
			TerminalTitleStripped: "zsh",
		},
	})

	renames = h.awaitRenames(3)
	if renames[2].Label != "dashboard" {
		t.Errorf("label = %q, want dashboard", renames[2].Label)
	}
}

func TestAgentStatusChangeArrivesThroughPaneUpdated(t *testing.T) {
	// Herdr offers no global pane.agent_status_changed subscription, so agent
	// context has to reach Auto Title through pane_updated. It does: the whole
	// PaneInfo is resent, agent fields included.
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t1", WorkspaceID: "wE", Label: "1"}},
		Panes: []herdr.PaneInfo{
			{PaneID: "wE:p1", TabID: "wE:t1", WorkspaceID: "wE", CWD: "/Users/dev/work/dashboard", Focused: true},
		},
	})

	if got := h.awaitRenames(1)[0].Label; got != "dashboard" {
		t.Fatalf("bootstrap rename = %q, want dashboard", got)
	}

	h.client.Emit(herdr.EventPaneUpdated, herdr.PaneUpdatedData{
		Pane: herdr.PaneInfo{
			PaneID: "wE:p1", TabID: "wE:t1", WorkspaceID: "wE",
			CWD: "/Users/dev/work/dashboard", Focused: true,
			Agent:       "claude",
			AgentStatus: herdr.AgentStatusWorking,
			Title:       "Implement OAuth scopes",
		},
	})

	renames := h.awaitRenames(2)
	if want := "dashboard · Implement OAuth scopes"; renames[1].Label != want {
		t.Errorf("rename = %q, want %q", renames[1].Label, want)
	}
}

func TestAgentDetectionRetitlesTheTab(t *testing.T) {
	// pane_agent_detected names only the pane, so the tab has to be found
	// through the cache's pane index.
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t1", WorkspaceID: "wE", Label: "1"}},
		Panes: []herdr.PaneInfo{
			{
				PaneID: "wE:p1", TabID: "wE:t1", WorkspaceID: "wE",
				CWD: "/Users/dev/work/dashboard", Focused: true,
				Title: "Implement OAuth scopes",
			},
		},
	})

	// Without a recognized agent the leftover title is not agent context.
	if got := h.awaitRenames(1)[0].Label; got != "dashboard" {
		t.Fatalf("bootstrap rename = %q, want dashboard", got)
	}

	h.client.Emit(herdr.EventPaneAgentDetected, herdr.PaneAgentDetectedData{
		PaneID: "wE:p1", WorkspaceID: "wE", Agent: "claude",
	})

	renames := h.awaitRenames(2)
	if want := "dashboard · Implement OAuth scopes"; renames[1].Label != want {
		t.Errorf("rename = %q, want %q", renames[1].Label, want)
	}
}

func TestReleasedAgentDropsItsContext(t *testing.T) {
	h := start(t, herdr.Snapshot{
		Tabs: []herdr.TabInfo{{TabID: "wE:t1", WorkspaceID: "wE", Label: "1"}},
		Panes: []herdr.PaneInfo{
			{
				PaneID: "wE:p1", TabID: "wE:t1", WorkspaceID: "wE",
				CWD: "/Users/dev/work/dashboard", Focused: true,
				Agent:       "claude",
				AgentStatus: herdr.AgentStatusWorking,
				Title:       "Implement OAuth scopes",
			},
		},
	})

	if want := "dashboard · Implement OAuth scopes"; h.awaitRenames(1)[0].Label != want {
		t.Fatalf("bootstrap rename = %q, want %q", h.awaitRenames(1)[0].Label, want)
	}

	h.client.Emit(herdr.EventPaneAgentDetected, herdr.PaneAgentDetectedData{
		PaneID: "wE:p1", WorkspaceID: "wE", Released: true, FinalStatus: herdr.AgentStatusDone,
	})

	renames := h.awaitRenames(2)
	if renames[1].Label != "dashboard" {
		t.Errorf("rename = %q, want dashboard", renames[1].Label)
	}
}

func TestNullAgentFieldsAreSurvivable(t *testing.T) {
	// Every agent field is nullable on the wire, agent_session included.
	h := start(t, herdr.Snapshot{
		Tabs:  []herdr.TabInfo{{TabID: "wE:t1", WorkspaceID: "wE", Label: "1"}},
		Panes: []herdr.PaneInfo{{PaneID: "wE:p1", TabID: "wE:t1", WorkspaceID: "wE", Focused: true}},
	})

	h.client.EmitRaw(herdr.EventPaneUpdated, `{"pane":{
		"pane_id":"wE:p1","tab_id":"wE:t1","workspace_id":"wE","terminal_id":"t",
		"focused":true,"revision":2,"cwd":"/Users/dev/work/dashboard",
		"foreground_cwd":null,"terminal_title":null,"terminal_title_stripped":null,
		"title":null,"agent":null,"display_agent":null,"agent_status":"unknown",
		"agent_session":null,"state_labels":{}}}`)

	if got := h.awaitRenames(1)[0].Label; got != "dashboard" {
		t.Errorf("rename = %q, want dashboard", got)
	}
	h.settle()
}
