// Package herdr implements a client for the Herdr local socket API.
//
// The wire format is newline-delimited JSON over the local socket named by
// HERDR_SOCKET_PATH. Every line is one JSON object. Outbound lines are
// requests; inbound lines are either responses (correlated by "id") or
// broadcast events (carrying "event" and "data").
//
// Verified against Herdr v0.8.2, protocol 20.
package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
)

// request is one outbound line.
//
// Herdr requires "params" on every method, so it is never omitted; methods
// without parameters take an empty object.
type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

// frame is one inbound line. A response carries ID plus Result or Error; an
// event carries Event plus Data.
type frame struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *APIError       `json:"error"`
	Event  string          `json:"event"`
	Data   json.RawMessage `json:"data"`
}

func (f *frame) isEvent() bool { return f.Event != "" }

// APIError is an error returned by Herdr for a request.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("herdr api error %s: %s", e.Code, e.Message)
}

// Error codes Auto Title reacts to.
const (
	// CodeTabNotFound is returned when a tab has closed since it was cached.
	CodeTabNotFound = "tab_not_found"
)

// ErrorCode returns the Herdr error code carried by err, or "" if err is not a
// Herdr API error.
func ErrorCode(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// Method names used by Auto Title.
const (
	MethodPing            = "ping"
	MethodSessionSnapshot = "session.snapshot"
	MethodEventsSubscribe = "events.subscribe"
	MethodTabRename       = "tab.rename"
)

// Subscription selects one event stream. Subscription types use dot notation
// ("pane.updated") while the events they deliver arrive with snake_case kinds
// ("pane_updated"); see the Event* constants.
//
// Most types are global. A few — pane.agent_status_changed, pane.scroll_changed,
// pane.output_matched — are per-pane and are rejected without a PaneID.
type Subscription struct {
	Type   string `json:"type"`
	PaneID string `json:"pane_id,omitempty"`
}

// Subscription types Auto Title subscribes to.
const (
	SubTabCreated  = "tab.created"
	SubTabClosed   = "tab.closed"
	SubPaneCreated = "pane.created"
	SubPaneUpdated = "pane.updated"
	SubPaneClosed  = "pane.closed"
	// SubPaneAgentDetected is global; it fires when Herdr starts or stops
	// recognizing an agent in a pane.
	SubPaneAgentDetected = "pane.agent_detected"
)

type subscribeParams struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

// TabRenameParams are the parameters of tab.rename.
type TabRenameParams struct {
	TabID string `json:"tab_id"`
	Label string `json:"label"`
}

// emptyParams is the parameter object for methods that take none.
type emptyParams struct{}
