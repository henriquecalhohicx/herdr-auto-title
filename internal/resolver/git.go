package resolver

import (
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"herdr-auto-title/internal/state"
)

const (
	// GitTTL is how long a branch reading stays fresh. A checkout shows up in
	// the tab within this long; until then the cached branch is used.
	GitTTL = 3 * time.Second
	// GitTimeout bounds one git invocation. A repository on a stalled network
	// mount must not leave a goroutine waiting forever.
	GitTimeout = 2 * time.Second
	// GitIdle is how long a reading nothing asks about is kept. A directory
	// still in use is asked about on every poll, so ten times the TTL leaves
	// ample room for one that is.
	GitIdle = 10 * GitTTL
)

// DefaultBranchMaxLength bounds what a branch may contribute to a title, in
// columns of the tab bar.
//
// Twelve columns hold a tracker key, or a short word and part of the next, and
// leave a tab readable beside a dozen others. Real branch names run far past
// it: the ones this was calibrated against averaged fifty characters and
// reached ninety.
const DefaultBranchMaxLength = 12

// defaultBranches name a repository's trunk, which says nothing the directory
// has not already said: a tab in ~/work/dashboard on main is just "dashboard".
var defaultBranches = map[string]struct{}{
	"main":   {},
	"master": {},
}

// trackerKey matches an issue key such as MC-13675 or ABC-42.
//
// It is the one part of a branch name that identifies the work, and it survives
// every naming convention: whether a team writes `feature/MC-13675`,
// `bugfix-alex-the-thing-mc-13675` or `MC-13675`, the key is in there
// somewhere. Two to six letters followed by at least two digits keeps it clear
// of ordinary hyphenated words and of version-like fragments such as `utf-8`.
var trackerKey = regexp.MustCompile(`(?i)\b[a-z]{2,6}-\d{2,6}\b`)

// branchSeparators are the characters branch names are built out of. Cutting a
// long branch at one of them ends it on a whole word.
const branchSeparators = "-_./ "

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
	lookup    func(ctx context.Context, dir string) (string, bool)
	ttl       time.Duration
	idle      time.Duration
	timeout   time.Duration
	maxLength int

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

// NewGit builds the source with the real git executable behind it. A maxLength
// of zero or less leaves branches out of titles entirely.
func NewGit(maxLength int) *Git {
	return &Git{
		lookup:    gitBranch,
		ttl:       GitTTL,
		idle:      GitIdle,
		timeout:   GitTimeout,
		maxLength: maxLength,
		entries:   make(map[string]*gitEntry),
	}
}

func (*Git) Name() string    { return "git" }
func (*Git) Confidence() int { return ConfidenceGit }

func (g *Git) Resolve(pane *state.PaneState) (Parts, bool) {
	if pane == nil || g.maxLength <= 0 {
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

	activity := shortenBranch(Sanitize(branch, 0), g.maxLength)
	if activity == "" {
		return Parts{}, false
	}
	return Parts{Activity: activity}, true
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

	g.forget()

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

// forget drops readings for directories nothing is asking about. The caller
// holds the mutex.
//
// Every other thing Auto Title remembers between polls is rebuilt from the
// snapshot, which prunes it to the live session for free. This cache is keyed
// by directory instead, so without this it grows for the life of the session,
// one entry for every directory any pane has ever been in.
//
// A directory still in use is asked about on every poll, and that keeps its
// reading fresh; one that has aged well past the TTL belongs to a pane that has
// moved on or closed. A lookup still running is left alone — its reading has
// not been taken yet.
func (g *Git) forget() {
	for dir, entry := range g.entries {
		if !entry.inFlight && time.Since(entry.readAt) > g.idle {
			delete(g.entries, dir)
		}
	}
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

// shortenBranch reduces a branch name to the part worth putting in a tab title.
//
// Branch conventions vary too much to enumerate, so nothing here is a list of
// known prefixes — a list only ever fits the team it was written for. Two rules
// cover every convention seen:
//
//   - An issue key wins outright. It identifies the work, it is short, and it
//     survives whatever the convention wraps around it. A team whose branches
//     all begin `bugfix-<author>-` gets eight characters that distinguish
//     instead of eight that do not.
//   - Otherwise keep the beginning, cut at a separator so the result ends on a
//     whole word, and drop any namespace the branch is filed under, since every
//     branch in the repository carries the same one.
//
// The trunk contributes nothing either way: a tab in a repository it is already
// named after learns nothing from being told it is on main.
func shortenBranch(branch string, maxLength int) string {
	branch = strings.Trim(branch, branchSeparators)
	if branch == "" || maxLength <= 0 {
		return ""
	}
	if _, isDefault := defaultBranches[strings.ToLower(branch)]; isDefault {
		return ""
	}

	if key := trackerKey.FindString(branch); key != "" {
		return strings.ToUpper(key)
	}

	if cut := strings.LastIndex(branch, "/"); cut >= 0 && cut+1 < len(branch) {
		branch = branch[cut+1:]
	}
	return cutAtSeparator(branch, maxLength)
}

// cutAtSeparator shortens a value to maxWidth columns, ending on the last
// separator that fits so the result is a whole word rather than a fragment.
func cutAtSeparator(value string, maxWidth int) string {
	head, rest := splitAtWidth(value, maxWidth)
	if rest == "" {
		return strings.Trim(value, branchSeparators)
	}

	// When the character that did not fit is itself a separator, the head
	// already ends on a whole word and cutting again would throw one away.
	next, _ := utf8.DecodeRuneInString(rest)
	if !strings.ContainsRune(branchSeparators, next) {
		if cut := strings.LastIndexAny(head, branchSeparators); cut > 0 {
			head = head[:cut]
		}
	}
	return strings.Trim(head, branchSeparators)
}
