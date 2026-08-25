package resolver

import (
	"strings"

	"herdr-auto-title/internal/state"
)

// Agent derives the activity from what the pane's agent says it is working on.
//
// It outranks every other source: when an agent reports a title, that title is
// the most direct statement of what the tab is for that Auto Title will ever
// see. The agent supplies no context, so the working directory still completes
// the name into `<context> › <activity>`.
//
// Many agents report no title at all and express the same thing through the
// terminal title instead; those panes fall through to TerminalTitle, which is
// the next source down. Nothing here reads an agent's transcript or asks
// anything over the network — the input is what Herdr's snapshot already carries.
type Agent struct{}

var _ Source = Agent{}

func (Agent) Name() string    { return "agent" }
func (Agent) Confidence() int { return ConfidenceAgent }

func (Agent) Resolve(pane *state.PaneState) (Parts, bool) {
	if !pane.HasAgent() {
		return Parts{}, false
	}

	// Truncation belongs to the assembled name, so no limit is applied here.
	activity, ok := Meaningful(Sanitize(pane.AgentTitle, 0))
	if !ok {
		return Parts{}, false
	}

	// An agent with nothing to report often echoes its own name. That is as
	// generic as anything in the table, but the name differs per agent, so it
	// is compared against the pane instead of being listed.
	if strings.EqualFold(activity, pane.Agent) || strings.EqualFold(activity, pane.DisplayAgent) {
		return Parts{}, false
	}
	return Parts{Activity: qualify(activity, paneKind(pane))}, true
}
