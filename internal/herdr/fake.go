package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

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
	renames       []RenameCall
	subscriptions []Subscription
	renameErr     error
	closed        bool

	events chan Event
	err    error
}

var _ Client = (*FakeClient)(nil)

// NewFake returns a client that answers session.snapshot with the given
// snapshot.
func NewFake(snapshot Snapshot) *FakeClient {
	return &FakeClient{
		snapshot: snapshot,
		events:   make(chan Event, 128),
	}
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
		if target, ok := result.(*snapshotResult); ok {
			target.Snapshot = f.snapshot
			return nil
		}
		return fmt.Errorf("fake client: unexpected result type for %s", method)

	case MethodTabRename:
		if f.renameErr != nil {
			return f.renameErr
		}
		rename, ok := params.(TabRenameParams)
		if !ok {
			return fmt.Errorf("fake client: unexpected params for %s", method)
		}
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
