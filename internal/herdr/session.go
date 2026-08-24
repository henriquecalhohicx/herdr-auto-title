package herdr

// Agent statuses Herdr reports. Every pane carries one: a pane with no agent
// reports AgentStatusUnknown.
const (
	AgentStatusIdle    = "idle"
	AgentStatusWorking = "working"
	AgentStatusBlocked = "blocked"
	AgentStatusDone    = "done"
	AgentStatusUnknown = "unknown"
)

// Wire types carry only the fields Auto Title reads. Herdr sends a great deal
// more; adding a field here that nothing uses makes the type claim a dependency
// the code does not have.

// TabInfo describes a tab. Optional fields are decoded as plain strings, so a
// JSON null leaves them empty rather than failing.
type TabInfo struct {
	TabID string `json:"tab_id"`
	Label string `json:"label"`
}

// PaneInfo describes a pane. It carries no foreground process name: that is
// only available through the pane.process_info method.
//
// Every optional field is nullable on the wire and decodes a JSON null to "".
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
	Title        string `json:"title"`
	Agent        string `json:"agent"`
	DisplayAgent string `json:"display_agent"`
	AgentStatus  string `json:"agent_status"`
}

// PaneProcessInfoProcess is one process running in a pane. Herdr reports more
// about each — its pid, its working directory, the joined command line — but a
// title is derived from the name and the arguments alone.
type PaneProcessInfoProcess struct {
	Name string   `json:"name"`
	Argv []string `json:"argv"`
}

// PaneProcessInfo is what pane.process_info answers.
//
// ForegroundProcesses holds the pane's foreground process and its descendants,
// so a pane running an editor that shelled out lists both.
type PaneProcessInfo struct {
	ForegroundProcesses []PaneProcessInfoProcess `json:"foreground_processes"`
}

// Snapshot is the whole session as session.snapshot reports it.
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

// processInfoResult wraps what pane.process_info answers.
type processInfoResult struct {
	ProcessInfo PaneProcessInfo `json:"process_info"`
}
