package herdr

import "encoding/json"

// Event kinds delivered on the socket. These are the snake_case values of the
// "event" field, which differ from the dot-notation subscription types.
const (
	EventTabCreated  = "tab_created"
	EventTabClosed   = "tab_closed"
	EventPaneCreated = "pane_created"
	EventPaneUpdated = "pane_updated"
	EventPaneClosed  = "pane_closed"

	// EventPaneAgentDetected fires when Herdr starts or stops recognizing an
	// agent in a pane.
	EventPaneAgentDetected = "pane_agent_detected"
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
//
// Events are treated as triggers, not as data: a payload says which tab may
// have changed, and the tab's state is then read back from Herdr. Herdr replays
// a backlog on subscribe, so a payload is not necessarily current.
type Event struct {
	Kind string
	Data json.RawMessage
}

// Wire types carry only the fields Auto Title reads. Herdr sends a great deal
// more; adding a field here that nothing uses makes the type claim a dependency
// the code does not have.

// TabInfo describes a tab. Optional fields are decoded as plain strings, so a
// JSON null leaves them empty rather than failing.
type TabInfo struct {
	TabID string `json:"tab_id"`
	Label string `json:"label"`
}

// AgentSessionInfo identifies the agent session Herdr matched to a pane. Only
// the reference itself is read, and only to record it: Auto Title never opens
// an agent's transcript.
type AgentSessionInfo struct {
	Value string `json:"value"`
}

// PaneInfo describes a pane. It carries no foreground process name: that is
// only available through the pane.process_info method.
//
// Every optional field is nullable on the wire; plain string fields decode a
// JSON null to "", and AgentSession is a pointer that stays nil.
type PaneInfo struct {
	PaneID   string `json:"pane_id"`
	TabID    string `json:"tab_id"`
	Focused  bool   `json:"focused"`
	Revision uint64 `json:"revision"`

	CWD                   string `json:"cwd"`
	ForegroundCWD         string `json:"foreground_cwd"`
	TerminalTitle         string `json:"terminal_title"`
	TerminalTitleStripped string `json:"terminal_title_stripped"`

	// Title is the agent's own title, not the terminal's. Herdr leaves it null
	// for agents that report their topic through the terminal title instead.
	Title        string            `json:"title"`
	Agent        string            `json:"agent"`
	DisplayAgent string            `json:"display_agent"`
	AgentStatus  string            `json:"agent_status"`
	AgentSession *AgentSessionInfo `json:"agent_session"`
}

// Event payloads. Creation events embed the full object; closing events carry
// only identifiers, so pane_closed does not name the tab the pane belonged to.

type TabCreatedData struct {
	Tab TabInfo `json:"tab"`
}

type TabClosedData struct {
	TabID string `json:"tab_id"`
}

// PaneUpdatedData is the payload of both pane_created and pane_updated, which
// carry the same object.
type PaneUpdatedData struct {
	Pane PaneInfo `json:"pane"`
}

type PaneClosedData struct {
	PaneID string `json:"pane_id"`
}

// PaneAgentDetectedData announces that Herdr started or stopped recognizing an
// agent in a pane. Which of the two it is does not matter — the pane is read
// afterwards either way. Like pane_closed it names only the pane, so the tab is
// found through the cache's pane index.
type PaneAgentDetectedData struct {
	PaneID string `json:"pane_id"`
}

// Snapshot is the result of session.snapshot, the initial state only.
type Snapshot struct {
	Tabs     []TabInfo  `json:"tabs"`
	Panes    []PaneInfo `json:"panes"`
	Protocol int        `json:"protocol"`
	Version  string     `json:"version"`
}

// snapshotResult wraps the snapshot in the method's result object.
type snapshotResult struct {
	Snapshot Snapshot `json:"snapshot"`
}

// tabInfoResult and paneInfoResult wrap the objects tab.get and pane.get
// answer with.
type tabInfoResult struct {
	Tab TabInfo `json:"tab"`
}

type paneInfoResult struct {
	Pane PaneInfo `json:"pane"`
}
