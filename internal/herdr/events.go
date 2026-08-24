package herdr

import "encoding/json"

// Event kinds delivered on the socket. These are the snake_case values of the
// "event" field, which differ from the dot-notation subscription types.
const (
	EventTabCreated  = "tab_created"
	EventTabClosed   = "tab_closed"
	EventTabRenamed  = "tab_renamed"
	EventPaneCreated = "pane_created"
	EventPaneUpdated = "pane_updated"
	EventPaneClosed  = "pane_closed"
)

// Event is one broadcast event as received from the socket. Data is left raw
// so that unknown kinds cost nothing and can be ignored safely.
type Event struct {
	Kind string
	Data json.RawMessage
}

// TabInfo describes a tab. Optional fields are decoded as plain strings, so a
// JSON null leaves them empty rather than failing.
type TabInfo struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      uint64 `json:"number"`
	PaneCount   uint64 `json:"pane_count"`
	Focused     bool   `json:"focused"`
	AgentStatus string `json:"agent_status"`
}

// PaneInfo describes a pane. It carries no foreground process name: that is
// only available through the pane.process_info method.
type PaneInfo struct {
	PaneID                string `json:"pane_id"`
	TabID                 string `json:"tab_id"`
	WorkspaceID           string `json:"workspace_id"`
	TerminalID            string `json:"terminal_id"`
	Focused               bool   `json:"focused"`
	Revision              uint64 `json:"revision"`
	CWD                   string `json:"cwd"`
	ForegroundCWD         string `json:"foreground_cwd"`
	TerminalTitle         string `json:"terminal_title"`
	TerminalTitleStripped string `json:"terminal_title_stripped"`
	Title                 string `json:"title"`
	Agent                 string `json:"agent"`
	DisplayAgent          string `json:"display_agent"`
	AgentStatus           string `json:"agent_status"`
}

// Event payloads. Creation events embed the full object; closing events carry
// only identifiers, so pane_closed does not name the tab the pane belonged to.

type TabCreatedData struct {
	Tab TabInfo `json:"tab"`
}

type TabClosedData struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

type TabRenamedData struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type PaneCreatedData struct {
	Pane PaneInfo `json:"pane"`
}

type PaneUpdatedData struct {
	Pane PaneInfo `json:"pane"`
}

type PaneClosedData struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
}

// Snapshot is the result of session.snapshot, the initial state only.
type Snapshot struct {
	Tabs          []TabInfo  `json:"tabs"`
	Panes         []PaneInfo `json:"panes"`
	FocusedTabID  string     `json:"focused_tab_id"`
	FocusedPaneID string     `json:"focused_pane_id"`
	Protocol      int        `json:"protocol"`
	Version       string     `json:"version"`
}

// snapshotResult wraps the snapshot in the method's result object.
type snapshotResult struct {
	Snapshot Snapshot `json:"snapshot"`
}
