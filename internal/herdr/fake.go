package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// sorted flattens a map into a slice ordered by each value's id, so a fake
// session is enumerated in the same order every time.
func sorted[T any](items map[string]T, id func(T) string) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return id(out[i]) < id(out[j]) })
	return out
}

// RenameCall records one tab.rename issued through a FakeClient.
type RenameCall struct {
	TabID string
	Label string
}

// FakeClient is an in-memory Client for tests. Tests push events onto it and
// inspect the renames it received.
type FakeClient struct {
	mu            sync.Mutex
	snapshot      Snapshot
	tabs          map[string]TabInfo
	panes         map[string]PaneInfo
	renames       []RenameCall
	subscriptions []Subscription
	renameErr     error
	readErr       error
	closed        bool

	events chan Event
	err    error
}

var _ Client = (*FakeClient)(nil)

// NewFake returns a client that answers session.snapshot with the given
// snapshot.
func NewFake(snapshot Snapshot) *FakeClient {
	f := &FakeClient{
		snapshot: snapshot,
		tabs:     make(map[string]TabInfo, len(snapshot.Tabs)),
		panes:    make(map[string]PaneInfo, len(snapshot.Panes)),
		events:   make(chan Event, 128),
	}
	for _, tab := range snapshot.Tabs {
		f.tabs[tab.TabID] = tab
	}
	for _, pane := range snapshot.Panes {
		f.panes[pane.PaneID] = pane
	}
	return f
}

// UpdatePane stores a pane's new state and announces it, the way Herdr does:
// the event says a pane changed, and a read is what reveals how.
func (f *FakeClient) UpdatePane(pane PaneInfo) {
	f.SetPane(pane)
	f.Emit(EventPaneUpdated, PaneUpdatedData{Pane: pane})
}

// SetPane changes what a read of this pane will answer, without announcing it.
func (f *FakeClient) SetPane(pane PaneInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panes[pane.PaneID] = pane
}

// CreateTab stores a new tab and announces it, the way Herdr does.
func (f *FakeClient) CreateTab(tab TabInfo) {
	f.SetTab(tab)
	f.Emit(EventTabCreated, TabCreatedData{Tab: tab})
}

// SetTab changes what a read of this tab will answer.
func (f *FakeClient) SetTab(tab TabInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tabs[tab.TabID] = tab
}

// ClosePane makes subsequent reads of the pane fail as Herdr's would.
func (f *FakeClient) ClosePane(paneID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.panes, paneID)
}

// CloseTab makes subsequent reads of the tab fail as Herdr's would.
func (f *FakeClient) CloseTab(tabID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tabs, tabID)
}

// SetReadError makes subsequent tab.get and pane.get calls fail.
func (f *FakeClient) SetReadError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readErr = err
}

// Emit delivers one event as if Herdr had broadcast it.
func (f *FakeClient) Emit(kind string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		panic(fmt.Sprintf("fake client: marshal %s: %v", kind, err))
	}
	// Held across the send so the channel cannot be closed underneath it.
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.events <- Event{Kind: kind, Data: payload}
}

// EmitRaw delivers an event whose payload is arbitrary JSON, for exercising
// malformed and unknown events.
func (f *FakeClient) EmitRaw(kind string, data string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.events <- Event{Kind: kind, Data: json.RawMessage(data)}
}

// SetRenameError makes subsequent tab.rename calls fail.
func (f *FakeClient) SetRenameError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renameErr = err
}

// Renames returns the renames received so far.
func (f *FakeClient) Renames() []RenameCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RenameCall(nil), f.renames...)
}

// Subscriptions returns the subscriptions established so far.
func (f *FakeClient) Subscriptions() []Subscription {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Subscription(nil), f.subscriptions...)
}

func (f *FakeClient) Call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrClosed
	}

	switch method {
	case MethodSessionSnapshot:
		target, ok := result.(*snapshotResult)
		if !ok {
			return fmt.Errorf("fake client: unexpected result type for %s", method)
		}
		// Assembled from what reads would answer, so a snapshot never
		// disagrees with tab.get and pane.get the way a stored copy would.
		target.Snapshot = Snapshot{
			Tabs:     sorted(f.tabs, func(t TabInfo) string { return t.TabID }),
			Panes:    sorted(f.panes, func(p PaneInfo) string { return p.PaneID }),
			Protocol: f.snapshot.Protocol,
			Version:  f.snapshot.Version,
		}
		return nil

	case MethodTabGet:
		if f.readErr != nil {
			return f.readErr
		}
		target, ok := params.(TabTarget)
		if !ok {
			return fmt.Errorf("fake client: unexpected params for %s", method)
		}
		tab, ok := f.tabs[target.TabID]
		if !ok {
			return &APIError{Code: CodeTabNotFound, Message: "tab " + target.TabID + " not found"}
		}
		res, ok := result.(*tabInfoResult)
		if !ok {
			return fmt.Errorf("fake client: unexpected result type for %s", method)
		}
		res.Tab = tab
		return nil

	case MethodPaneGet:
		if f.readErr != nil {
			return f.readErr
		}
		target, ok := params.(PaneTarget)
		if !ok {
			return fmt.Errorf("fake client: unexpected params for %s", method)
		}
		pane, ok := f.panes[target.PaneID]
		if !ok {
			return &APIError{Code: CodePaneNotFound, Message: "pane " + target.PaneID + " not found"}
		}
		res, ok := result.(*paneInfoResult)
		if !ok {
			return fmt.Errorf("fake client: unexpected result type for %s", method)
		}
		res.Pane = pane
		return nil

	case MethodTabRename:
		if f.renameErr != nil {
			return f.renameErr
		}
		rename, ok := params.(TabRenameParams)
		if !ok {
			return fmt.Errorf("fake client: unexpected params for %s", method)
		}
		if _, ok := f.tabs[rename.TabID]; !ok {
			return &APIError{Code: CodeTabNotFound, Message: "tab " + rename.TabID + " not found"}
		}
		// Herdr's label really does change, so a later read must agree.
		tab := f.tabs[rename.TabID]
		tab.Label = rename.Label
		f.tabs[rename.TabID] = tab
		f.renames = append(f.renames, RenameCall{TabID: rename.TabID, Label: rename.Label})
		return nil

	case MethodPing:
		return nil

	default:
		return fmt.Errorf("fake client: unsupported method %s", method)
	}
}

func (f *FakeClient) Subscribe(_ context.Context, subs []Subscription) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrClosed
	}
	f.subscriptions = append(f.subscriptions, subs...)
	return nil
}

func (f *FakeClient) Events() <-chan Event { return f.events }

func (f *FakeClient) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// Disconnect ends the event stream with the given cause, as a dropped socket
// would.
func (f *FakeClient) Disconnect(cause error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	f.err = cause
	f.mu.Unlock()
	close(f.events)
}

func (f *FakeClient) Close() error {
	f.Disconnect(ErrClosed)
	return nil
}
