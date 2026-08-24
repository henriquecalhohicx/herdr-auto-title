package resolver

import (
	"context"
	"strings"
	"testing"

	"herdr-auto-title/internal/state"
)

// sshPane builds a pane running the given ssh command line, beside the shell
// that started it — which is how Herdr reports a pane's processes.
func sshPane(cwd string, argv ...string) *state.PaneState {
	return &state.PaneState{
		CWD: cwd,
		Processes: []state.Process{
			{Name: "fish", Args: []string{"-fish"}},
			{Name: "ssh", Args: argv},
		},
	}
}

func TestTheHostBecomesTheContext(t *testing.T) {
	// Every form the ticket lists, and the flags that must not be mistaken for
	// a destination.
	cases := map[string]string{
		"ssh prod-01":                             "prod-01",
		"ssh root@prod-01":                        "prod-01",
		"ssh dev@production.example.com":          "production.example.com",
		"ssh -p 2222 deploy@prod-01":              "prod-01",
		"ssh -p2222 prod-01":                      "prod-01",
		"ssh -i ~/.ssh/id_ed25519 prod-01":        "prod-01",
		"ssh -L 8080:localhost:80 prod-01":        "prod-01",
		"ssh -o StrictHostKeyChecking=no prod-01": "prod-01",
		"ssh -J bastion prod-01":                  "prod-01",
		"ssh -4 -q -t prod-01":                    "prod-01",
		"ssh -tt prod-01":                         "prod-01",

		// A remote command follows the destination and must not replace it.
		"ssh root@prod-01 tail -f /var/log/syslog": "prod-01",
		"ssh prod-01 -- systemctl status":          "prod-01",

		// URL and port forms.
		"ssh ssh://deploy@prod-01:2222": "prod-01",
		"ssh ssh://prod-01/":            "prod-01",
		"ssh deploy@prod-01:2222":       "prod-01",

		// IPv6, bracketed and bare.
		"ssh root@[2001:db8::1]:22": "2001:db8::1",
		"ssh 2001:db8::1":           "2001:db8::1",
		"ssh 10.0.0.5":              "10.0.0.5",
	}

	for command, want := range cases {
		got := sshHost(strings.Fields(command))
		if got != want {
			t.Errorf("%s → %q, want %q", command, got, want)
		}
	}
}

func TestTheTabReadsHostThenSSH(t *testing.T) {
	pane := sshPane("/Users/dev/work/dashboard", "ssh", "root@prod-01")

	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane))
	if want := "prod-01 · SSH"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
	if got.Reason != "ssh" {
		t.Errorf("reason = %q, want ssh", got.Reason)
	}
	if got.Confidence != ConfidenceSSH {
		t.Errorf("confidence = %d, want %d", got.Confidence, ConfidenceSSH)
	}
}

func TestTheHostOutranksTheWorkingDirectory(t *testing.T) {
	// The local directory of a pane running ssh describes the wrong machine.
	pane := sshPane("/Users/dev/work/dashboard", "ssh", "prod-01")

	if got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane)); got.Name != "prod-01 · SSH" {
		t.Errorf("name = %q, want %q", got.Name, "prod-01 · SSH")
	}
}

func TestAnUnreadableDestinationStillMarksTheTabRemote(t *testing.T) {
	// Herdr could not read argv, or ssh was invoked with no destination at all.
	for _, argv := range [][]string{nil, {"ssh"}, {"ssh", "-p", "2222"}, {"ssh", "-"}} {
		pane := sshPane("/Users/dev/work/dashboard", argv...)

		got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane))
		if want := "dashboard · SSH"; got.Name != want {
			t.Errorf("argv %v → %q, want %q", argv, got.Name, want)
		}
	}
}

func TestAPaneWithoutSSHIsUnaffected(t *testing.T) {
	pane := &state.PaneState{
		CWD: "/Users/dev/work/dashboard",
		Processes: []state.Process{
			{Name: "fish", Args: []string{"-fish"}},
			{Name: "nvim", Args: []string{"nvim"}},
		},
	}

	if got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane)); got.Name != "dashboard" {
		t.Errorf("name = %q, want dashboard", got.Name)
	}
}

func TestSSHIsFoundAmongOtherProcesses(t *testing.T) {
	// Herdr lists the foreground process and its descendants, so ssh can be
	// anywhere in the list.
	pane := &state.PaneState{
		CWD: "/Users/dev/work/dashboard",
		Processes: []state.Process{
			{Name: "fish", Args: []string{"-fish"}},
			{Name: "ssh", Args: []string{"ssh", "prod-01"}},
			{Name: "tail", Args: []string{"tail", "-f", "/var/log/syslog"}},
		},
	}

	if got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane)); got.Name != "prod-01 · SSH" {
		t.Errorf("name = %q, want %q", got.Name, "prod-01 · SSH")
	}
}

func TestAMeaningfulTerminalTitleOutranksSSH(t *testing.T) {
	// The remote shell sets a title; the host still says which machine it is.
	pane := sshPane("/Users/dev/work/dashboard", "ssh", "prod-01")
	pane.TerminalTitle = "Restart the queue workers"

	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane))
	if want := "prod-01 · Restart the queue workers"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
}

func TestSSHSourceOnANilPane(t *testing.T) {
	if _, ok := (SSH{}).Resolve(nil); ok {
		t.Fatal("resolved a nil pane")
	}
}

func TestHostsFromArgvAreSanitized(t *testing.T) {
	// argv is terminal-derived input like any other.
	pane := sshPane("/Users/dev/work/dashboard", "ssh", "root@prod\x1b[31m-01")

	got := titleResolver(DefaultMaxLength).Resolve(context.Background(), tabWithPane(pane))
	if strings.ContainsRune(got.Name, '\x1b') {
		t.Errorf("name = %q, still carries an escape", got.Name)
	}
	if want := "prod-01 · SSH"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
}
