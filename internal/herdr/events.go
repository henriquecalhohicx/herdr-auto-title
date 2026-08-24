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

	// Agent kinds. pane_agent_detected fires when Herdr starts or stops
	// recognizing an agent in a pane; pane_agent_status_changed follows the
	// agent's own reporting.
	EventPaneAgentDetected      = "pane_agent_detected"
	EventPaneAgentStatusChanged = "pane_agent_status_changed"
)

// Agent statuses Herdr reports. Every pane carries one: a pane with no agent
// reports AgentStatusUnknown.
const (
	AgentStatusIdle    = "idle"
	AgentStatusWorking = "working"
	AgentStatusBlocked = "blocked"
	AgentStatusDone    = "done"
	AgentStatusUnknown = "unknown"
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

// AgentSessionInfo identifies the agent session Herdr matched to a pane, such
// as a Claude Code session id or the path of its transcript. Auto Title only
// ever reads it; no transcript is opened.
type AgentSessionInfo struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

// PaneInfo describes a pane. It carries no foreground process name: that is
// only available through the pane.process_info method.
//
// Every optional field is nullable on the wire; plain string fields decode a
// JSON null to "", and AgentSession is a pointer that stays nil.
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

	AgentSession *AgentSessionInfo `json:"agent_session"`
	StateLabels  map[string]string `json:"state_labels"`
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

// PaneAgentDetectedData announces that Herdr started recognizing an agent in a
// pane, or, when Released is true, that the agent is gone. Like pane_closed it
// names only the pane, so the tab is found through the cache's pane index.
type PaneAgentDetectedData struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	Agent       string `json:"agent"`
	FinalStatus string `json:"final_status"`
	Released    bool   `json:"released"`
}

// PaneAgentStatusChangedData carries an agent's own reporting: its status and,
// when it has one, the title of what it is working on.
type PaneAgentStatusChangedData struct {
	PaneID       string            `json:"pane_id"`
	WorkspaceID  string            `json:"workspace_id"`
	Agent        string            `json:"agent"`
	DisplayAgent string            `json:"display_agent"`
	AgentStatus  string            `json:"agent_status"`
	Title        string            `json:"title"`
	StateLabels  map[string]string `json:"state_labels"`
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
