package resolver

import (
	"strings"

	"herdr-auto-title/internal/state"
)

// KindSeparator binds a kind to its detail: `nvim:auth.provider.ts`.
//
// It is not the separator between the two halves of a title. A colon says "this
// kind of thing, that one in particular"; the middle dot says "here, doing
// that". A project name never takes a colon, because a project is a place
// rather than a kind of thing.
const KindSeparator = ":"

// shellNames are the programs that run in a pane without being what the pane is
// for. A pane running a shell is described by what the shell is running.
var shellNames = map[string]struct{}{
	"bash": {}, "zsh": {}, "fish": {}, "sh": {}, "dash": {}, "ksh": {},
	"tcsh": {}, "csh": {}, "login": {},
}

// paneKind names what a pane is running, or "" when that cannot be said.
//
// An agent is asked for first, because Herdr recognizes it directly and its
// process list does not: a coding agent shows up as half a dozen helpers with
// its own name nowhere among them.
//
// Failing that, Herdr reports a pane's foreground process together with its
// descendants, and only a lone process names a pane. A build tool reports as
// `esbuild` and five `node`s; picking one of those would be guesswork, and the
// terminal title already says what such a pane is doing.
func paneKind(pane *state.PaneState) string {
	if pane == nil {
		return ""
	}
	if pane.HasAgent() {
		// An agent's name is in the generic table, because an agent naming
		// itself is not a report of its work. As a kind it is exactly right:
		// it names the program, which is all a kind ever does.
		return Sanitize(pane.Agent, 0)
	}

	var kind string
	for _, process := range pane.Processes {
		name := strings.TrimSpace(process.Name)
		if name == "" {
			continue
		}
		if _, isShell := shellNames[strings.ToLower(name)]; isShell {
			continue
		}
		if kind != "" {
			// More than one candidate; nothing here names the pane.
			return ""
		}
		kind = name
	}

	switch strings.ToLower(kind) {
	case "":
		return ""
	case "ssh":
		// A remote session is marked on the host, where the mark cannot be
		// outranked. Saying it again here would only repeat it.
		return ""
	}
	if _, generic := genericValues[strings.ToLower(kind)]; generic {
		// A runtime names the language, not the work.
		return ""
	}
	return Sanitize(kind, 0)
}

// qualify binds a kind to what a source found, so a title reads
// `nvim:auth.provider.ts`. A kind with nothing left to add stands alone, and
// without a kind the activity is unchanged.
func qualify(activity, kind string) string {
	if kind == "" {
		return activity
	}
	detail := stripKind(activity, kind)
	if detail == "" {
		return kind
	}
	return kind + KindSeparator + detail
}

// stripKind removes the kind from a detail that already carries it, so a kind
// and its detail never say the same thing twice: Neovim titles its window
// `auth.provider.ts - Nvim`, and under the kind `nvim` the suffix is noise.
func stripKind(detail, kind string) string {
	trimmed := strings.TrimSpace(detail)
	if trimmed == "" || kind == "" {
		return trimmed
	}

	lower, lowerKind := strings.ToLower(trimmed), strings.ToLower(kind)
	switch {
	case lower == lowerKind:
		return ""
	case strings.HasSuffix(lower, lowerKind):
		trimmed = trimmed[:len(trimmed)-len(kind)]
	case strings.HasPrefix(lower, lowerKind):
		trimmed = trimmed[len(kind):]
	}
	return strings.Trim(trimmed, " -–—|:·")
}

// Process names a pane after the program running in it when nothing has said
// what that program is doing.
//
// It is the bare half of what the terminal title contributes: a tab holding an
// editor with no file open still reads `dashboard · nvim`, which says more than
// the directory alone.
type Process struct{}

var _ Source = Process{}

func (Process) Name() string { return "process" }

func (Process) Resolve(pane *state.PaneState) (Parts, bool) {
	if pane == nil {
		return Parts{}, false
	}
	kind := paneKind(pane)
	if kind == "" {
		return Parts{}, false
	}
	return Parts{Activity: kind, Confidence: ConfidenceProcess}, true
}
