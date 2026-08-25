// Package app polls the Herdr session and keeps every tab's title in step with
// what that tab is doing.
package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/kryptamine/herdr-auto-title/internal/herdr"
	"github.com/kryptamine/herdr-auto-title/internal/resolver"
	"github.com/kryptamine/herdr-auto-title/internal/state"
)

// pollTimeout bounds one poll: a snapshot and the renames it decides on.
const pollTimeout = 5 * time.Second

// App is one run of the Auto Title loop.
type App struct {
	cfg     Config
	log     *slog.Logger
	titles  resolver.TitleResolver
	changes *state.Changes
	manual  *state.Manual
}

// New builds the application. The connection is supplied to Run, so the same
// App can be driven by a real socket or by a stub in tests.
func New(cfg Config, log *slog.Logger, titles resolver.TitleResolver) *App {
	return &App{
		cfg:     cfg,
		log:     log,
		titles:  titles,
		changes: state.NewChanges(),
		manual:  state.LoadManual(cfg.ManualPath),
	}
}

// Run polls the session until the context is cancelled. Herdr's event stream is
// deliberately not used, and the measurements that settled that are in
// docs/architecture/poll-loop.md.
func (a *App) Run(ctx context.Context, client herdr.Client) error {
	var failures failureLog

	// Name what already exists before waiting for the first tick.
	a.attempt(ctx, client, &failures)

	ticker := time.NewTicker(a.cfg.Poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.log.Info("shutting down")
			return nil
		case <-ticker.C:
			a.attempt(ctx, client, &failures)
		}
	}
}

// attempt polls once and reports how it went, on the schedule failures keep. No
// poll failing is fatal, the first one included: a plugin that gave up would
// stay dead, and Herdr's socket can be a moment behind the process it launched.
func (a *App) attempt(ctx context.Context, client herdr.Client, failures *failureLog) {
	err := a.poll(ctx, client)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		if run := failures.failed(); run > 0 {
			a.log.Warn("poll failed", "error", err, "in a row", run)
		}
		return
	}
	if run := failures.recovered(); run > 0 {
		a.log.Info("the session is answering again", "polls missed", run)
	}
}

// failureLog decides how often a run of failing polls is worth mentioning. At
// two polls a second, logging every one turns an hour of Herdr being down into
// thousands of identical lines; logging as the run doubles costs a dozen.
type failureLog struct {
	run  int
	next int
}

// failed records a failed poll and returns the length of the run when it is
// worth logging, or zero when it is not.
func (f *failureLog) failed() int {
	f.run++
	if f.run < f.next {
		return 0
	}
	f.next = f.run * 2
	return f.run
}

// recovered records a successful poll and returns how many polls the run of
// failures it ended cost, or zero when nothing was wrong.
func (f *failureLog) recovered() int {
	run := f.run
	f.run, f.next = 0, 0
	return run
}

// poll reads the session and renames every tab whose title no longer fits.
func (a *App) poll(ctx context.Context, client herdr.Client) error {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	snapshot, err := herdr.SessionSnapshot(ctx, client)
	if err != nil {
		return err
	}
	a.changes.Observe(snapshot.Panes)

	tabs := a.tabsIn(ctx, client, snapshot)
	a.manual.Retain(labelsOf(tabs))

	for _, tab := range tabs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if a.manual.Locked(tab.ID) {
			continue
		}

		decision := a.titles.Resolve(tab)
		if a.manual.Observe(state.Sighting{
			TabID:   tab.ID,
			Current: tab.CurrentName,
			Desired: decision.Name,
			Default: tab.DefaultName,
		}) {
			a.log.Info("leaving a tab the user renamed", "tab_id", tab.ID, "name", tab.CurrentName)
			continue
		}
		if decision.Name == "" || decision.Name == tab.CurrentName {
			continue
		}

		if err := herdr.RenameTab(ctx, client, tab.ID, decision.Name); err != nil {
			if herdr.ErrorCode(err) == herdr.CodeTabNotFound {
				// The tab closed between the snapshot and the rename. The next
				// poll will not see it at all.
				a.log.Debug("tab closed before it could be renamed", "tab_id", tab.ID)
				continue
			}
			a.log.Warn("rename failed", "tab_id", tab.ID, "name", decision.Name, "error", err)
			continue
		}

		// Recorded before the log line so the next poll cannot read this
		// rename as the user's.
		a.manual.Applied(tab.ID, decision.Name)
		a.log.Info("tab renamed",
			"tab_id", tab.ID,
			"old", tab.CurrentName,
			"new", decision.Name,
			"reason", decision.Reason,
			"confidence", decision.Confidence,
		)
	}

	// Reached only when every tab was seen. Deferring this would settle after a
	// poll cut short, and the tabs it missed would look new and already named.
	a.manual.Settled()
	return nil
}

// labelsOf indexes tabs by id for the manual-name bookkeeping, which needs both
// an id that is gone and a label that has moved on.
func labelsOf(tabs []state.TabState) map[string]string {
	labels := make(map[string]string, len(tabs))
	for _, tab := range tabs {
		labels[tab.ID] = tab.CurrentName
	}
	return labels
}

// tabsIn assembles the snapshot's tabs with their panes. What is running in a
// pane needs a request of its own, measured at 0.11 ms — cheaper than the
// snapshot — so it is made for every pane rather than guessed at.
func (a *App) tabsIn(ctx context.Context, client herdr.Client, snapshot herdr.Snapshot) []state.TabState {
	workspaces := make(map[string]string, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		workspaces[workspace.WorkspaceID] = workspace.Label
	}

	byTab := make(map[string][]*state.PaneState, len(snapshot.Tabs))
	for _, pane := range snapshot.Panes {
		processes, err := herdr.PaneProcesses(ctx, client, pane.PaneID)
		if err != nil && herdr.ErrorCode(err) != herdr.CodePaneNotFound && ctx.Err() == nil {
			a.log.Debug("could not read what a pane is running", "pane_id", pane.PaneID, "error", err)
		}
		byTab[pane.TabID] = append(byTab[pane.TabID],
			state.PaneFrom(pane, processes, a.changes.ChangedAt(pane.PaneID)))
	}

	// An unnamed tab carries its place in the workspace, and the snapshot lists
	// tabs in display order, so counting them gives that label.
	positions := make(map[string]int, len(snapshot.Workspaces))

	tabs := make([]state.TabState, 0, len(snapshot.Tabs))
	for _, info := range snapshot.Tabs {
		positions[info.WorkspaceID]++
		tabs = append(tabs, state.TabFrom(
			info, workspaces[info.WorkspaceID], positions[info.WorkspaceID], byTab[info.TabID]))
	}
	return tabs
}
