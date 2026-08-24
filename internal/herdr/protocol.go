// Package herdr implements a client for the Herdr local socket API.
//
// The wire format is newline-delimited JSON over the local socket named by
// HERDR_SOCKET_PATH. Every line is one JSON object: a request out, a response
// back, and then the connection closes.
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

// frame is one inbound line: a result or an error.
type frame struct {
	Result json.RawMessage `json:"result"`
	Error  *APIError       `json:"error"`
}

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
	// CodeTabNotFound is returned when a tab closed between the snapshot that
	// named it and the rename that followed.
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

// Method names used by Auto Title. There are only two: one to read the session
// and one to act on it.
const (
	MethodSessionSnapshot = "session.snapshot"
	MethodTabRename       = "tab.rename"
)

// TabRenameParams are the parameters of tab.rename.
type TabRenameParams struct {
	TabID string `json:"tab_id"`
	Label string `json:"label"`
}

// emptyParams is the parameter object for methods that take none.
type emptyParams struct{}
