package resolver

import (
	"context"
	"testing"

	"herdr-auto-title/internal/herdr"
	"herdr-auto-title/internal/state"
)

func TestAgentTitleBeatsEverySourceBelowIt(t *testing.T) {
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		CWD:           "/Users/dev/work/dashboard",
		TerminalTitle: "Claude Code",
		Agent:         "claude",
		AgentStatus:   herdr.AgentStatusWorking,
		AgentTitle:    "Implement OAuth scopes",
	}))

	if want := "dashboard · Implement OAuth scopes"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
	if got.Reason != "agent" {
		t.Errorf("reason = %q, want agent", got.Reason)
	}
	if got.Confidence != ConfidenceAgent {
		t.Errorf("confidence = %d, want %d", got.Confidence, ConfidenceAgent)
	}
}

func TestAgentTitleOutranksAMeaningfulTerminalTitle(t *testing.T) {
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		CWD:           "/Users/dev/work/dashboard",
		TerminalTitle: "Fix OAuth redirect",
		Agent:         "claude",
		AgentStatus:   herdr.AgentStatusWorking,
		AgentTitle:    "Implement OAuth scopes",
	}))

	if want := "dashboard · Implement OAuth scopes"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
}

func TestGenericAgentNameFallsThrough(t *testing.T) {
	// An agent that has nothing to report titles itself. In a live session the
	// topic then arrives through the terminal title instead.
	for _, title := range []string{"Claude", "Claude Code", "Agent", "Coding Agent", ""} {
		t.Run(title, func(t *testing.T) {
			got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
				CWD:           "/Users/dev/work/dashboard",
				TerminalTitle: "Fix OAuth redirect",
				Agent:         "claude",
				AgentStatus:   herdr.AgentStatusWorking,
				AgentTitle:    title,
			}))

			if want := "dashboard · Fix OAuth redirect"; got.Name != want {
				t.Errorf("name = %q, want %q", got.Name, want)
			}
			if got.Reason != "terminal_title" {
				t.Errorf("reason = %q, want terminal_title", got.Reason)
			}
		})
	}
}

func TestAgentEchoingItsOwnNameIsNotAgentContext(t *testing.T) {
	// Agents the generic table has never heard of must not pass their own name
	// off as a report of their work. The name still reaches the tab, but as the
	// weakest source rather than the strongest.
	pane := &state.PaneState{
		CWD:          "/Users/dev/work/dashboard",
		Agent:        "acme-bot",
		DisplayAgent: "Acme Bot",
		AgentStatus:  herdr.AgentStatusWorking,
		AgentTitle:   "Acme Bot",
	}

	if _, ok := (Agent{}).Resolve(pane); ok {
		t.Error("the agent source claimed an agent naming itself")
	}

	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane))
	if want := "dashboard · Acme Bot"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
	if got.Reason != "agent_name" {
		t.Errorf("reason = %q, want agent_name", got.Reason)
	}
}

func TestAgentTitleWithoutAnAgentIsIgnored(t *testing.T) {
	// Herdr leaves the title on a pane whose agent it no longer recognizes;
	// without an agent it is not agent context.
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		CWD:        "/Users/dev/work/dashboard",
		AgentTitle: "Implement OAuth scopes",
	}))

	if got.Name != "dashboard" {
		t.Errorf("name = %q, want %q", got.Name, "dashboard")
	}
}

func TestAgentSourceOnANilPane(t *testing.T) {
	if _, ok := (Agent{}).Resolve(nil); ok {
		t.Fatal("resolved a nil pane")
	}
}

func TestAgentTitleWithNoDirectoryStandsAlone(t *testing.T) {
	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(&state.PaneState{
		Agent:       "claude",
		AgentStatus: herdr.AgentStatusWorking,
		AgentTitle:  "Implement OAuth scopes",
	}))

	if want := "Implement OAuth scopes"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
}

func TestContextAndActivityComeFromTheSamePane(t *testing.T) {
	// The agent pane wins the selection; the other pane's directory must not
	// leak into the name and describe neither of them.
	tab := state.TabState{
		ID: "wE:t1",
		Panes: map[string]*state.PaneState{
			"wE:p1": {
				ID: "wE:p1", TabID: "wE:t1",
				CWD:           "/Users/dev/work/api",
				TerminalTitle: "Run migrations",
			},
			"wE:p2": {
				ID: "wE:p2", TabID: "wE:t1",
				CWD:         "/Users/dev/work/dashboard",
				Agent:       "claude",
				AgentStatus: herdr.AgentStatusWorking,
				AgentTitle:  "Implement OAuth scopes",
			},
		},
	}

	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tab)
	if want := "dashboard · Implement OAuth scopes"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
}
