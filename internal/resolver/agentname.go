package resolver

import (
	"strings"
	"unicode"

	"herdr-auto-title/internal/state"
)

// AgentName names the agent itself when nothing has said what it is working on.
//
// An agent does not report a topic the moment it starts: Claude Code titles its
// window `Claude Code` until the conversation has a subject. Until then a tab
// holding an agent and a tab holding a plain shell in the same directory would
// carry the same name, and the one worth switching to would be indistinguishable
// from the one that is not.
//
// This is the weakest source there is — a name is not work — so it sits at the
// bottom of the chain and only ever fills an activity that nothing else claimed.
// The moment the agent has something to report, the sources above take over.
type AgentName struct{}

var _ Source = AgentName{}

func (AgentName) Name() string { return "agent_name" }

func (AgentName) Resolve(pane *state.PaneState) (Parts, bool) {
	if !pane.HasAgent() {
		return Parts{}, false
	}
	name := agentDisplayName(pane)
	if name == "" {
		return Parts{}, false
	}
	return Parts{Activity: name, Confidence: ConfidenceAgentName}, true
}

// agentDisplayName picks the most recognizable name for the pane's agent.
//
// Herdr's own display_agent comes first. Failing that — it is null on every
// pane observed — the terminal title is used when the agent set it to its own
// product name, because `Claude Code` says more than the `claude` Herdr
// matched. Otherwise the matched name is capitalized and used as it is.
func agentDisplayName(pane *state.PaneState) string {
	if display := Sanitize(pane.DisplayAgent, 0); display != "" {
		return display
	}

	agent := Sanitize(pane.Agent, 0)
	if title := Sanitize(pane.TerminalTitle, 0); namesAgent(title, agent) {
		return title
	}
	return capitalize(agent)
}

// namesAgent reports whether a terminal title is the agent naming itself, which
// is what makes it a better label than the agent's bare identifier.
func namesAgent(title, agent string) bool {
	if title == "" || agent == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(title), strings.ToLower(agent))
}

func capitalize(value string) string {
	words := strings.Fields(value)
	for i, word := range words {
		runes := []rune(word)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
