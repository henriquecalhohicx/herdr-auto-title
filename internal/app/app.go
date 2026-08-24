// Package app wires the Herdr client, the state cache, the debouncer and the
// resolver into the Auto Title event loop.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"herdr-auto-title/internal/debounce"
	"herdr-auto-title/internal/herdr"
	"herdr-auto-title/internal/resolver"
	"herdr-auto-title/internal/state"
)

const (
	// reconcileWorkers bounds how many tabs are reconciled at once, so a burst
	// across many tabs cannot spawn unbounded goroutines.
	reconcileWorkers = 4
	// queueBuffer holds tabs waiting for a free worker.
	queueBuffer = 128
	// renameTimeout bounds a single tab.rename call.
	renameTimeout = 5 * time.Second
)

// Subscriptions lists the event streams Auto Title needs.
//
// Subscription types use dot notation; the events they deliver arrive with
// snake_case kinds. pane.closed is included so a closing pane leaves no stale
// context behind — pane_closed does not name its tab, which is why the cache
// indexes panes by ID as well.
func Subscriptions() []herdr.Subscription {
	return []herdr.Subscription{
		{Type: herdr.SubTabCreated},
		{Type: herdr.SubTabClosed},
		{Type: herdr.SubPaneCreated},
		{Type: herdr.SubPaneUpdated},
		{Type: herdr.SubPaneClosed},
	}
}

// App is one run of the Auto Title event loop over one connection.
type App struct {
	cfg    Config
	log    *slog.Logger
	cache  *state.Cache
	titles resolver.TitleResolver

	debouncer *debounce.Manager
	queue     *reconcileQueue

	client  herdr.Client
	workers sync.WaitGroup
}

// New builds the application. The connection is supplied to Run, so the same
// App can be driven by a real socket or by a fake client in tests.
func New(cfg Config, log *slog.Logger, titles resolver.TitleResolver) *App {
	a := &App{
		cfg:    cfg,
		log:    log,
		cache:  state.NewCache(),
		titles: titles,
		queue:  newReconcileQueue(queueBuffer),
	}
	a.debouncer = debounce.New(cfg.Debounce, cfg.MaxWait, a.queue.push)
	return a
}

// Cache exposes the state cache for tests.
func (a *App) Cache() *state.Cache { return a.cache }

// Run bootstraps from a session snapshot, subscribes to events and processes
// them until the context is cancelled or the connection ends.
//
// It returns nil on a clean shutdown and the connection's error otherwise;
// reconnect is not this slice's concern, so the caller decides what to do next.
func (a *App) Run(ctx context.Context, client herdr.Client) error {
	a.client = client

	// Subscribe first. The stream and the snapshot travel on separate
	// connections, so events that occur while the snapshot is being fetched
	// queue up instead of being lost, and they are applied on top of the
	// snapshot once the loop starts.
	if err := client.Subscribe(ctx, Subscriptions()); err != nil {
		return fmt.Errorf("subscribe to events: %w", err)
	}
	a.log.Info("subscribed to herdr events", "count", len(Subscriptions()))

	snapshot, err := herdr.SessionSnapshot(ctx, client)
	if err != nil {
		return fmt.Errorf("bootstrap session snapshot: %w", err)
	}
	a.cache.Reset(snapshot)
	a.log.Info("session snapshot loaded",
		"tabs", len(snapshot.Tabs),
		"panes", len(snapshot.Panes),
		"herdr_version", snapshot.Version,
		"protocol", snapshot.Protocol,
	)

	a.startWorkers(ctx)
	defer a.stop()

	// The snapshot is the initial state; reconcile what already exists before
	// waiting for changes.
	for _, tabID := range a.cache.TabIDs() {
		a.debouncer.Schedule(tabID)
	}

	for {
		select {
		case <-ctx.Done():
			a.log.Info("shutting down")
			return nil
		case event, ok := <-client.Events():
			if !ok {
				return client.Err()
			}
			a.handleEvent(event)
		}
	}
}

func (a *App) startWorkers(ctx context.Context) {
	for i := 0; i < reconcileWorkers; i++ {
		a.workers.Add(1)
		go func() {
			defer a.workers.Done()
			for tabID := range a.queue.out() {
				a.queue.forget(tabID)
				a.reconcile(ctx, tabID)
			}
		}()
	}
}

// stop shuts the pipeline down in order: no more timers, then no more queued
// work, then wait for reconciliations already running.
func (a *App) stop() {
	a.debouncer.Close()
	a.queue.close()
	a.workers.Wait()
}

// handleEvent updates the cache and schedules reconciliation. It never renames
// a tab itself: the socket reader must not block on expensive work.
func (a *App) handleEvent(event herdr.Event) {
	switch event.Kind {
	case herdr.EventTabCreated:
		data, ok := decodeEvent[herdr.TabCreatedData](a.log, event)
		if !ok {
			return
		}
		a.log.Debug("event received", "type", event.Kind, "tab_id", data.Tab.TabID)
		a.cache.UpsertTab(data.Tab)
		a.debouncer.Schedule(data.Tab.TabID)

	case herdr.EventTabClosed:
		data, ok := decodeEvent[herdr.TabClosedData](a.log, event)
		if !ok {
			return
		}
		a.log.Debug("event received", "type", event.Kind, "tab_id", data.TabID)
		a.debouncer.Cancel(data.TabID)
		a.cache.RemoveTab(data.TabID)

	case herdr.EventPaneCreated, herdr.EventPaneUpdated:
		data, ok := decodeEvent[herdr.PaneUpdatedData](a.log, event)
		if !ok {
			return
		}
		a.log.Debug("event received", "type", event.Kind, "pane_id", data.Pane.PaneID)
		if tabID := a.cache.UpsertPane(data.Pane); tabID != "" {
			a.debouncer.Schedule(tabID)
		}

	case herdr.EventPaneClosed:
		data, ok := decodeEvent[herdr.PaneClosedData](a.log, event)
		if !ok {
			return
		}
		a.log.Debug("event received", "type", event.Kind, "pane_id", data.PaneID)
		if tabID := a.cache.RemovePane(data.PaneID); tabID != "" {
			a.debouncer.Schedule(tabID)
		}

	default:
		// Herdr broadcasts more than Auto Title subscribes to, and future
		// versions will add kinds this build has never heard of.
		a.log.Debug("ignoring unrecognized event", "type", event.Kind)
	}
}

// reconcile resolves the tab's title and renames it if it differs.
func (a *App) reconcile(ctx context.Context, tabID string) {
	if ctx.Err() != nil {
		return
	}

	tab, ok := a.cache.Tab(tabID)
	if !ok {
		// The tab closed between the timer firing and this worker picking it up.
		return
	}
	if tab.ManualName {
		a.log.Debug("skipping tab renamed by hand", "tab_id", tabID)
		return
	}

	decision := a.titles.Resolve(ctx, tab)
	if decision.Name == "" {
		return
	}
	if decision.Name == tab.CurrentName {
		a.log.Debug("title unchanged", "tab_id", tabID, "name", decision.Name)
		return
	}

	renameCtx, cancel := context.WithTimeout(ctx, renameTimeout)
	defer cancel()

	if err := herdr.RenameTab(renameCtx, a.client, tabID, decision.Name); err != nil {
		if herdr.ErrorCode(err) == herdr.CodeTabNotFound {
			// The tab closed between resolution and the rename. Herdr will
			// deliver tab_closed too, but a short-lived tab can be gone before
			// that arrives, so drop it here rather than logging a warning.
			a.log.Debug("tab closed before it could be renamed", "tab_id", tabID)
			a.debouncer.Cancel(tabID)
			a.cache.RemoveTab(tabID)
			return
		}
		a.log.Warn("rename failed", "tab_id", tabID, "name", decision.Name, "error", err)
		return
	}

	a.cache.SetCurrentName(tabID, decision.Name)
	a.cache.MarkReconciled(tabID, time.Now())
	a.log.Info("tab renamed",
		"tab_id", tabID,
		"old", tab.CurrentName,
		"new", decision.Name,
		"reason", decision.Reason,
		"confidence", decision.Confidence,
	)
}

// decodeEvent unmarshals an event payload, treating a malformed one as an event
// to ignore rather than a reason to stop.
func decodeEvent[T any](log *slog.Logger, event herdr.Event) (T, bool) {
	var payload T
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		log.Warn("discarding malformed event", "type", event.Kind, "error", err)
		return payload, false
	}
	return payload, true
}
