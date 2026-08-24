package resolver

import (
	"context"
	"testing"

	"herdr-auto-title/internal/herdr"
	"herdr-auto-title/internal/state"
)

func TestStartingAgentIsNamedBeforeItHasATopic(t *testing.T) {
	// Exactly what a freshly opened Claude Code pane looks like: an agent is
	// recognized, it has titled its window after itself, and it has not yet
	// decided what the conversation is about.
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		CWD:           "/Users/dev/work/dashboard",
		TerminalTitle: "Claude Code",
		Agent:         "claude",
		AgentStatus:   herdr.AgentStatusIdle,
	}))

	if want := "dashboard · Claude Code"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
	if got.Reason != "agent_name" {
		t.Errorf("reason = %q, want agent_name", got.Reason)
	}
	if got.Confidence != ConfidenceAgentName {
		t.Errorf("confidence = %d, want %d", got.Confidence, ConfidenceAgentName)
	}
}

func TestAgentNameYieldsAsSoonAsThereIsATopic(t *testing.T) {
	for _, pane := range []*state.PaneState{
		{
			// The topic arrived through the terminal title, which is how
			// Claude Code reports it.
			CWD: "/Users/dev/work/dashboard", Agent: "claude",
			TerminalTitle: "Fix OAuth redirect",
		},
		{
			// The topic arrived through the agent's own title field.
			CWD: "/Users/dev/work/dashboard", Agent: "claude",
			TerminalTitle: "Claude Code",
			AgentTitle:    "Fix OAuth redirect",
		},
	} {
		got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane))
		if want := "dashboard · Fix OAuth redirect"; got.Name != want {
			t.Errorf("name = %q, want %q", got.Name, want)
		}
		if got.Reason == "agent_name" {
			t.Error("the agent name won over a topic")
		}
	}
}

func TestPaneWithoutAnAgentIsNotNamedAfterOne(t *testing.T) {
	// The same directory, no agent: the tab must stay distinguishable from the
	// one that has one.
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		CWD:           "/Users/dev/work/dashboard",
		TerminalTitle: "Claude Code",
	}))

	if got.Name != "dashboard" {
		t.Errorf("name = %q, want %q", got.Name, "dashboard")
	}
}

func TestAgentNamePrefersHerdrsDisplayName(t *testing.T) {
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		CWD:           "/Users/dev/work/dashboard",
		TerminalTitle: "Claude Code",
		Agent:         "claude",
		DisplayAgent:  "Claude Sonnet",
	}))

	if want := "dashboard · Claude Sonnet"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
}

func TestAgentNameCapitalizesTheMatchedIdentifier(t *testing.T) {
	// Herdr matched an agent but neither it nor the agent offers a display
	// name, so the identifier itself has to do.
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		CWD:           "/Users/dev/work/dashboard",
		TerminalTitle: "~/w/dashboard",
		Agent:         "claude",
	}))

	if want := "dashboard · Claude"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
}

func TestAgentNameOnANilPane(t *testing.T) {
	if _, ok := (AgentName{}).Resolve(nil); ok {
		t.Fatal("resolved a nil pane")
	}
}

func TestAgentNameStandsAloneWithoutADirectory(t *testing.T) {
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		TerminalTitle: "Claude Code",
		Agent:         "claude",
	}))

	if got.Name != "Claude Code" {
		t.Errorf("name = %q, want %q", got.Name, "Claude Code")
	}
}
