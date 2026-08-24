package resolver

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"herdr-auto-title/internal/state"
)

const (
	// GitTTL is how long a branch reading stays fresh. A checkout shows up in
	// the tab within this long; until then the cached branch is used.
	GitTTL = 3 * time.Second
	// GitTimeout bounds one git invocation. A repository on a stalled network
	// mount must not leave a goroutine waiting forever.
	GitTimeout = 2 * time.Second
)

// defaultBranches name a repository's trunk, which says nothing the directory
// has not already said: a tab in ~/work/dashboard on main is just "dashboard".
var defaultBranches = map[string]struct{}{
	"main":   {},
	"master": {},
}

// branchPrefixes are the conventional namespaces a branch name is filed under.
// They are the same on every branch in the repository, so they cost characters
// without distinguishing anything.
var branchPrefixes = map[string]struct{}{
	"feature": {}, "features": {}, "feat": {},
	"bugfix": {}, "bug": {}, "fix": {}, "hotfix": {},
	"release": {}, "chore": {}, "refactor": {}, "docs": {}, "test": {},
}

// Git derives the activity from the branch checked out in the pane's directory.
//
// It ranks above the working directory and below every source that reports what
// the user is doing: a branch says which slice of a project a tab is on, which
// is more than the directory alone but less than a title someone set on purpose.
//
// **The lookup never runs in the poll loop.** Resolve answers from a cache and,
// when that reading is missing or stale, starts a background refresh that the
// next poll picks up. A directory therefore contributes no branch on the poll
// that first sees it, and its branch from then on.
type Git struct {
	// lookup reads the branch checked out in a directory. It is a field so
	// tests can drive every outcome without a repository on disk.
	lookup  func(ctx context.Context, dir string) (string, bool)
	ttl     time.Duration
	timeout time.Duration

	mu      sync.Mutex
	entries map[string]*gitEntry
}

type gitEntry struct {
	branch   string
	found    bool
	readAt   time.Time
	inFlight bool
}

var _ Source = (*Git)(nil)

// NewGit builds the source with the real git executable behind it.
func NewGit() *Git {
	return &Git{
		lookup:  gitBranch,
		ttl:     GitTTL,
		timeout: GitTimeout,
		entries: make(map[string]*gitEntry),
	}
}

func (*Git) Name() string { return "git" }

func (g *Git) Resolve(pane *state.PaneState) (Parts, bool) {
	if pane == nil {
		return Parts{}, false
	}

	// The shell's own directory, for the same reason the CWD source prefers it:
	// it names the project more stably than whatever is running right now.
	dir := pane.CWD
	if dir == "" {
		dir = pane.ForegroundCWD
	}
	if dir == "" || !filepath.IsAbs(dir) {
		return Parts{}, false
	}

	branch, found := g.cached(dir)
	if !found {
		return Parts{}, false
	}

	activity := shortenBranch(Sanitize(branch, 0))
	if activity == "" {
		return Parts{}, false
	}
	if _, isDefault := defaultBranches[strings.ToLower(activity)]; isDefault {
		return Parts{}, false
	}
	return Parts{Activity: activity, Confidence: ConfidenceGit}, true
}

// cached returns the branch known for a directory, refreshing it in the
// background when the reading is missing or has aged out.
//
// A stale reading is still returned while the refresh runs. A branch that has
// just been checked out is worth showing a moment late; a tab that flickers
// back to its directory name and forward again is not.
func (g *Git) cached(dir string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	entry, known := g.entries[dir]
	if !known {
		entry = &gitEntry{}
		g.entries[dir] = entry
	}

	if !entry.inFlight && (!known || time.Since(entry.readAt) > g.ttl) {
		entry.inFlight = true
		go g.refresh(dir)
	}
	return entry.branch, entry.found
}

func (g *Git) refresh(dir string) {
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	defer cancel()

	branch, found := g.lookup(ctx, dir)

	g.mu.Lock()
	defer g.mu.Unlock()
	if entry, ok := g.entries[dir]; ok {
		entry.branch, entry.found = branch, found
		entry.readAt = time.Now()
		entry.inFlight = false
	}
}

// gitBranch asks git which branch is checked out in dir.
//
// One invocation covers every outcome: git missing, the directory not being a
// repository, and a detached HEAD are all "no branch". git is executed
// directly, never through a shell, and dir is an argument rather than part of a
// command line.
func gitBranch(ctx context.Context, dir string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	// git reads the environment for pagers, editors and prompts; none of that
	// belongs in a background lookup.
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.Output()
	if err != nil {
		return "", false
	}

	branch := strings.TrimSpace(string(out))
	// A detached HEAD reports itself as "HEAD", which names nothing.
	if branch == "" || branch == "HEAD" {
		return "", false
	}
	return branch, true
}

// shortenBranch drops a conventional namespace from a branch name, so
// `feature/MC-13200` contributes `MC-13200`.
//
// Only the leading segment is considered, and only when it is a namespace
// rather than part of the name: `fix/oauth` shortens, `oauth/fix` does not.
func shortenBranch(branch string) string {
	prefix, rest, found := strings.Cut(branch, "/")
	if !found || rest == "" {
		return branch
	}
	if _, isPrefix := branchPrefixes[strings.ToLower(prefix)]; !isPrefix {
		return branch
	}
	return rest
}
