package resolver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rivo/uniseg"

	"herdr-auto-title/internal/state"
)

// stubGit builds a Git source answering from a fixed table, so every outcome is
// reachable without a repository on disk.
func stubGit(branches map[string]string) *Git {
	g := &Git{
		ttl:       GitTTL,
		idle:      GitIdle,
		timeout:   GitTimeout,
		maxLength: DefaultBranchMaxLength,
		entries:   make(map[string]*gitEntry),
	}
	g.lookup = func(_ context.Context, dir string) (string, bool) {
		branch, ok := branches[dir]
		return branch, ok
	}
	return g
}

// awaitLookup resolves once to start the background lookup, then waits for it
// to land. It works for outcomes that produce no branch, which polling until a
// branch appears cannot.
func awaitLookup(t *testing.T, g *Git, pane *state.PaneState) {
	t.Helper()
	g.Resolve(pane)

	dir := pane.CWD
	if dir == "" {
		dir = pane.ForegroundCWD
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		g.mu.Lock()
		entry, ok := g.entries[dir]
		done := ok && !entry.inFlight && !entry.readAt.IsZero()
		g.mu.Unlock()
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the lookup for %s never landed", dir)
		}
		time.Sleep(time.Millisecond)
	}
}

// awaitBranch waits for the lookup and returns what the source then reports.
func awaitBranch(t *testing.T, g *Git, pane *state.PaneState) (Parts, bool) {
	t.Helper()
	awaitLookup(t, g, pane)
	return g.Resolve(pane)
}

func TestBranchBecomesTheActivity(t *testing.T) {
	g := stubGit(map[string]string{"/Users/dev/work/dashboard": "MC-13200"})
	pane := &state.PaneState{CWD: "/Users/dev/work/dashboard"}

	parts, ok := awaitBranch(t, g, pane)
	if !ok {
		t.Fatal("the branch never arrived")
	}
	if parts.Activity != "MC-13200" {
		t.Errorf("activity = %q, want MC-13200", parts.Activity)
	}
	if parts.Context != "" {
		t.Errorf("context = %q, want the directory source to supply it", parts.Context)
	}
	if got := NewGit(DefaultBranchMaxLength).Confidence(); got != ConfidenceGit {
		t.Errorf("confidence = %d, want %d", got, ConfidenceGit)
	}
}

func TestTheFirstResolveDoesNotWaitForGit(t *testing.T) {
	// The whole point of the cache: a poll must never block on a subprocess.
	release := make(chan struct{})
	g := stubGit(nil)
	g.lookup = func(ctx context.Context, _ string) (string, bool) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "MC-13200", true
	}
	defer close(release)

	done := make(chan struct{})
	go func() {
		g.Resolve(&state.PaneState{CWD: "/Users/dev/work/dashboard"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Resolve blocked on the git lookup")
	}
}

func TestDefaultBranchesAddNothing(t *testing.T) {
	for _, branch := range []string{"main", "master", "Main", "MASTER"} {
		t.Run(branch, func(t *testing.T) {
			g := stubGit(map[string]string{"/Users/dev/work/dashboard": branch})
			pane := &state.PaneState{CWD: "/Users/dev/work/dashboard"}

			awaitLookup(t, g, pane)
			if _, ok := g.Resolve(pane); ok {
				t.Errorf("branch %q was used as an activity", branch)
			}
		})
	}
}

func TestBranchesAreReducedToWhatIdentifiesThem(t *testing.T) {
	// The first block is taken verbatim from a repository this was calibrated
	// against: no slashes anywhere, every branch prefixed the same way, and the
	// issue key in a different place each time. A rule built on known prefixes
	// or on the first N characters returns "bugfix-asa" for all of them.
	cases := map[string]string{
		"bugfix-asatretdinov-cpanel-uapi-body-arguments-mc-13675":     "MC-13675",
		"bugfix-MC-12722-sql-injection-operations-summary":            "MC-12722",
		"bugfix-dmodin-MC-13618":                                      "MC-13618",
		"bugfix-nchebotarev-early_access_bar_breaks_scroll-MC-13590":  "MC-13590",
		"bugfix-mboiko-MC-4911-show-clear-error-message-for-wp-agent": "MC-4911",

		// Other conventions reach the same key.
		"feature/MC-13200": "MC-13200",
		"MC-13200":         "MC-13200",
		"mc-13200":         "MC-13200",

		// No key: keep the beginning, ending on a whole word.
		"refactor-the-poller":         "refactor-the",
		"drop_the_event_stream":       "drop_the",
		"short":                       "short",
		"exactly-12ch":                "exactly-12ch",
		"averyveryverylongsingleword": "averyveryver",

		// A namespace applies to every branch in the repository.
		"feature/oauth":     "oauth",
		"alex/oauth-scopes": "oauth-scopes",
		"a/b/oauth":         "oauth",

		// Version-like fragments are not issue keys.
		"fix-utf-8-encoding": "fix-utf-8",
		"http-2-support":     "http-2",
	}
	for branch, want := range cases {
		if got := shortenBranch(branch, DefaultBranchMaxLength); got != want {
			t.Errorf("shortenBranch(%q) = %q, want %q", branch, got, want)
		}
	}
}

func TestNoBranchIsShownWhenTheLimitIsZero(t *testing.T) {
	// The way to turn branches off entirely.
	g := stubGit(map[string]string{"/Users/dev/work/dashboard": "MC-13200"})
	g.maxLength = 0
	pane := &state.PaneState{CWD: "/Users/dev/work/dashboard"}

	if _, ok := g.Resolve(pane); ok {
		t.Error("a branch was contributed with the limit at zero")
	}
	g.mu.Lock()
	started := len(g.entries)
	g.mu.Unlock()
	if started != 0 {
		t.Error("a lookup was started even though branches are off")
	}
}

func TestTheLimitBoundsWhatABranchAdds(t *testing.T) {
	// The limit is columns of the tab bar, which is why the wide branch is
	// here: eight of its characters would fill sixteen.
	for _, branch := range []string{"refactor-the-whole-poller", "\u8a2d\u5b9a-\u30d5\u30a1\u30a4\u30eb-work"} {
		for _, limit := range []int{4, 8, 12, 40} {
			got := shortenBranch(branch, limit)
			if width := uniseg.StringWidth(got); width > limit {
				t.Errorf("limit %d on %q produced %q, %d columns", limit, branch, got, width)
			}
		}
	}
}

func TestNoRepositoryContributesNothing(t *testing.T) {
	g := stubGit(nil)
	pane := &state.PaneState{CWD: "/Users/dev/not-a-repo"}

	if _, ok := awaitBranch(t, g, pane); ok {
		t.Error("a directory outside a repository produced a branch")
	}
}

func TestRelativeAndEmptyDirectoriesAreIgnored(t *testing.T) {
	g := stubGit(map[string]string{"work/dashboard": "MC-13200"})

	for _, pane := range []*state.PaneState{
		{CWD: "work/dashboard"},
		{},
		nil,
	} {
		if _, ok := g.Resolve(pane); ok {
			t.Errorf("pane %+v produced a branch", pane)
		}
	}
}

func TestTheForegroundDirectoryIsTheFallback(t *testing.T) {
	g := stubGit(map[string]string{"/Users/dev/work/dashboard": "MC-13200"})
	pane := &state.PaneState{ForegroundCWD: "/Users/dev/work/dashboard"}

	if _, ok := awaitBranch(t, g, pane); !ok {
		t.Error("the foreground directory was not consulted")
	}
}

func TestRepeatedResolvesDoNotSpawnRepeatedLookups(t *testing.T) {
	var calls atomic.Int64
	g := stubGit(nil)
	g.lookup = func(context.Context, string) (string, bool) {
		calls.Add(1)
		return "MC-13200", true
	}
	pane := &state.PaneState{CWD: "/Users/dev/work/dashboard"}

	awaitBranch(t, g, pane)
	for i := 0; i < 100; i++ {
		g.Resolve(pane)
	}

	// One for the first sighting; the rest are served from the cache because
	// none of them is older than the TTL.
	if got := calls.Load(); got != 1 {
		t.Errorf("git ran %d times, want once", got)
	}
}

func TestAnAgedReadingIsRefreshed(t *testing.T) {
	var branch atomic.Value
	branch.Store("MC-13200")

	g := stubGit(nil)
	g.ttl = time.Millisecond
	g.lookup = func(context.Context, string) (string, bool) {
		return branch.Load().(string), true
	}
	pane := &state.PaneState{CWD: "/Users/dev/work/dashboard"}

	if parts, _ := awaitBranch(t, g, pane); parts.Activity != "MC-13200" {
		t.Fatalf("activity = %q, want MC-13200", parts.Activity)
	}

	branch.Store("MC-14000")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if parts, _ := g.Resolve(pane); parts.Activity == "MC-14000" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the checkout never reached the title")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAStaleReadingIsUsedWhileItRefreshes(t *testing.T) {
	// A tab must not flicker back to its bare directory name every time the
	// reading ages out.
	block := make(chan struct{})
	var first sync.Once

	g := stubGit(nil)
	g.ttl = time.Millisecond
	g.lookup = func(ctx context.Context, _ string) (string, bool) {
		ready := false
		first.Do(func() { ready = true })
		if !ready {
			select {
			case <-block:
			case <-ctx.Done():
			}
		}
		return "MC-13200", true
	}
	defer close(block)

	pane := &state.PaneState{CWD: "/Users/dev/work/dashboard"}
	awaitBranch(t, g, pane)

	time.Sleep(5 * time.Millisecond)
	for i := 0; i < 5; i++ {
		parts, ok := g.Resolve(pane)
		if !ok || parts.Activity != "MC-13200" {
			t.Fatalf("resolve %d lost the branch while refreshing: %+v", i, parts)
		}
	}
}

func TestATimedOutLookupIsNotFatal(t *testing.T) {
	g := stubGit(nil)
	g.timeout = time.Millisecond
	g.lookup = func(ctx context.Context, _ string) (string, bool) {
		<-ctx.Done()
		return "", false
	}
	pane := &state.PaneState{CWD: "/Users/dev/work/dashboard"}

	awaitLookup(t, g, pane)
	if _, ok := g.Resolve(pane); ok {
		t.Error("a timed-out lookup produced a branch")
	}
}

func TestGitSourceIsSafeUnderConcurrentUse(t *testing.T) {
	g := stubGit(map[string]string{"/Users/dev/work/dashboard": "MC-13200"})
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				g.Resolve(&state.PaneState{CWD: "/Users/dev/work/dashboard"})
			}
		}()
	}
	wg.Wait()
}

// The tests below drive the real executable, because the point of the source is
// what git actually answers.

func TestRealGitReadsTheCheckedOutBranch(t *testing.T) {
	repo := newRepo(t)
	run(t, repo, "checkout", "-b", "feature/MC-13200")

	branch, ok := gitBranch(context.Background(), repo)
	if !ok {
		t.Fatal("git reported no branch in a repository")
	}
	if branch != "feature/MC-13200" {
		t.Errorf("branch = %q, want feature/MC-13200", branch)
	}
}

func TestRealGitOutsideARepository(t *testing.T) {
	dir := t.TempDir()
	// A temp dir can sit inside a repository on some machines; make sure it
	// cannot be one.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))

	if _, ok := gitBranch(context.Background(), dir); ok {
		t.Error("git reported a branch outside a repository")
	}
}

func TestRealGitOnADetachedHead(t *testing.T) {
	repo := newRepo(t)
	run(t, repo, "checkout", "--detach")

	if _, ok := gitBranch(context.Background(), repo); ok {
		t.Error("a detached HEAD was reported as a branch")
	}
}

func TestRealGitOnAMissingDirectory(t *testing.T) {
	if _, ok := gitBranch(context.Background(), "/nonexistent/for/auto/title"); ok {
		t.Error("a missing directory produced a branch")
	}
}

// newRepo creates a repository with one commit, so HEAD resolves.
func newRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)

	dir := t.TempDir()
	run(t, dir, "init", "--quiet")
	run(t, dir, "config", "user.email", "test@example.com")
	run(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run(t, dir, "add", "file")
	run(t, dir, "commit", "--quiet", "-m", "first")
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			t.Skip("git is not installed")
		}
		t.Fatalf("look up git: %v", err)
	}
}

func TestTheShippedChainPutsTheBranchAfterTheDirectory(t *testing.T) {
	// End to end through Default(), so the ladder's order is exercised rather
	// than assumed: the directory is the context, the branch the activity.
	repo := newRepo(t)
	run(t, repo, "checkout", "-b", "feature/MC-13200")

	chain := Default(DefaultMaxLength, DefaultBranchMaxLength)
	tab := tabWithPane(&state.PaneState{CWD: repo})
	want := filepath.Base(repo) + " › MC-13200"

	deadline := time.Now().Add(2 * time.Second)
	for {
		got := chain.Resolve(context.Background(), tab)
		if got.Name == want {
			if got.Reason != "git" {
				t.Errorf("reason = %q, want git", got.Reason)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("name = %q, want %q", got.Name, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAMeaningfulTerminalTitleOutranksTheBranch(t *testing.T) {
	repo := newRepo(t)
	run(t, repo, "checkout", "-b", "feature/MC-13200")

	chain := Default(DefaultMaxLength, DefaultBranchMaxLength)
	tab := tabWithPane(&state.PaneState{CWD: repo, TerminalTitle: "Fix OAuth redirect"})

	// Give the branch every chance to arrive before asserting it lost.
	time.Sleep(50 * time.Millisecond)
	got := chain.Resolve(context.Background(), tab)

	if want := filepath.Base(repo) + " › Fix OAuth redirect"; got.Name != want {
		t.Errorf("name = %q, want %q", got.Name, want)
	}
}

func TestTheCacheForgetsDirectoriesNothingAsksAbout(t *testing.T) {
	// Every other thing kept between polls is rebuilt from the snapshot, which
	// prunes it for free. This cache is keyed by directory, so it needs the
	// pruning done for it or a long session accumulates every directory any
	// pane has ever been in.
	g := stubGit(map[string]string{"/work/one": "MC-1", "/work/two": "MC-2"})
	awaitLookup(t, g, &state.PaneState{CWD: "/work/one"})
	awaitLookup(t, g, &state.PaneState{CWD: "/work/two"})

	// Age one reading as a pane that closed would: nothing asks about it any
	// more, so nothing refreshes it.
	g.mu.Lock()
	g.entries["/work/one"].readAt = time.Now().Add(-2 * g.idle)
	g.mu.Unlock()

	// The next poll, which still asks about the other directory.
	g.Resolve(&state.PaneState{CWD: "/work/two"})

	g.mu.Lock()
	defer g.mu.Unlock()
	if _, kept := g.entries["/work/one"]; kept {
		t.Error("a directory nothing asks about is still cached")
	}
	if _, kept := g.entries["/work/two"]; !kept {
		t.Error("the directory still in use was forgotten")
	}
}

func TestALookupStillRunningIsNotForgotten(t *testing.T) {
	// A fresh entry has no reading yet, so its zero timestamp is older than any
	// idle window. Dropping it would lose the lookup already on its way.
	g := stubGit(nil)
	started := make(chan struct{})
	release := make(chan struct{})
	g.lookup = func(context.Context, string) (string, bool) {
		close(started)
		<-release
		return "", false
	}

	g.Resolve(&state.PaneState{CWD: "/work/slow"})
	<-started
	g.Resolve(&state.PaneState{CWD: "/work/slow"})

	g.mu.Lock()
	_, kept := g.entries["/work/slow"]
	g.mu.Unlock()
	close(release)

	if !kept {
		t.Error("a directory whose lookup was still running was forgotten")
	}
}
