package resolver

import (
	"os"
	"path/filepath"

	"github.com/kryptamine/herdr-auto-title/internal/state"
)

// CWD derives the tab's context from the pane's working directory: the
// basename of the directory, which is normally the project name.
//
// It is the lowest-priority source. Directories that say nothing about what the
// user is doing — the home directory, the filesystem root, a relative path —
// yield nothing, and the resolver falls back to a generic name.
type CWD struct {
	home string
}

var _ Source = CWD{}

// NewCWD builds the source, resolving the user's home directory once.
func NewCWD() CWD {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return CWD{home: filepath.Clean(home)}
}

func (CWD) Name() string    { return "cwd" }
func (CWD) Confidence() int { return ConfidenceCWD }

func (c CWD) Resolve(pane *state.PaneState) (Parts, bool) {
	if pane == nil {
		return Parts{}, false
	}

	// The shell's own directory names the project more stably than the
	// foreground process's, which follows whatever is currently running.
	dir := pane.CWD
	if dir == "" {
		dir = pane.ForegroundCWD
	}

	name := c.base(dir)
	if name == "" {
		return Parts{}, false
	}
	return Parts{Context: name}, true
}

// base returns the meaningful basename of dir, or "" when the path carries no
// useful context.
func (c CWD) base(dir string) string {
	if dir == "" {
		return ""
	}
	clean := filepath.Clean(dir)
	if !filepath.IsAbs(clean) {
		return ""
	}
	if clean == string(filepath.Separator) {
		return ""
	}
	if c.home != "" && clean == c.home {
		return ""
	}

	base := filepath.Base(clean)
	switch base {
	case ".", "..", string(filepath.Separator):
		return ""
	}
	return base
}
