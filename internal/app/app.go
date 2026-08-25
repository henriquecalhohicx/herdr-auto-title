// Package app polls the Herdr session and keeps every tab's title in step with
// what that tab is doing.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"herdr-auto-title/internal/herdr"
	"herdr-auto-title/internal/resolver"
	"herdr-auto-title/internal/state"
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

// Run polls the session until the context is cancelled.
//
// Herdr does expose an event stream, and Auto Title deliberately does not use
// it. Subscribing replays a backlog before delivering anything live — roughly
// the last hundred revisions of every pane, about ten a second, so ten seconds
// of history per active pane — and events.subscribe offers no cursor to skip
// it. A subscriber therefore spends its first seconds reacting to a session
// that no longer exists, while a snapshot always describes the present and
// costs one request whatever the session holds.
func (a *App) Run(ctx context.Context, client herdr.Client) error {
	// Name what already exists before waiting for the first tick.
	if err := a.poll(ctx, client); err != nil {
		return fmt.Errorf("first poll: %w", err)
	}

	ticker := time.NewTicker(a.cfg.Poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.log.Info("shutting down")
			return nil
		case <-ticker.C:
			if err := a.poll(ctx, client); err != nil {
				if ctx.Err() != nil {
					continue
				}
				// A single failed poll is not fatal: Herdr may be busy, or a
				// tab may have closed underneath the request. The next tick
				// tries again.
				a.log.Warn("poll failed", "error", err)
			}
		}
	}
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

		decision := a.titles.Resolve(ctx, tab)
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

	a.manual.Settled()
	return nil
}

// labelsOf indexes tabs by id for the manual-name bookkeeping, which needs both
// halves: an id that is gone, and a label that has moved on.
func labelsOf(tabs []state.TabState) map[string]string {
	labels := make(map[string]string, len(tabs))
	for _, tab := range tabs {
		labels[tab.ID] = tab.CurrentName
	}
	return labels
}

// tabsIn assembles the snapshot's tabs, each with the panes it holds.
//
// The snapshot carries everything about a pane except what is running in it,
// which needs a request of its own. That request measured 0.11 ms — less than
// the snapshot that preceded it — so it is made for every pane rather than
// guessed at, and a pane whose processes cannot be read simply has none.
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

	// A tab nobody has named carries its place in its workspace, and the
	// snapshot lists tabs in the order they are shown, so counting them gives
	// the label Herdr would have put there.
	positions := make(map[string]int, len(snapshot.Workspaces))

	tabs := make([]state.TabState, 0, len(snapshot.Tabs))
	for _, info := range snapshot.Tabs {
		positions[info.WorkspaceID]++
		tabs = append(tabs, state.TabFrom(
			info, workspaces[info.WorkspaceID], positions[info.WorkspaceID], byTab[info.TabID]))
	}
	return tabs
}
