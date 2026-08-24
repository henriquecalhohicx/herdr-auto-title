package resolver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"herdr-auto-title/internal/state"
)

// tabWithCWD builds a one-pane tab whose pane sits in dir.
func tabWithCWD(dir string) state.TabState {
	return state.TabState{
		ID: "wE:t1",
		Panes: map[string]*state.PaneState{
			"wE:p1": {ID: "wE:p1", TabID: "wE:t1", CWD: dir, Focused: true},
		},
	}
}

func TestResolveFromCWD(t *testing.T) {
	home := t.TempDir()
	source := CWD{home: filepath.Clean(home)}
	r := New(DefaultMaxLength, source)

	tests := []struct {
		name       string
		cwd        string
		want       string
		wantReason string
	}{
		{"project directory becomes the title", "/Users/dev/work/dashboard", "dashboard", "cwd"},
		{"nested directory uses its own basename", "/Users/dev/work/dashboard/src/api", "api", "cwd"},
		{"trailing slash is ignored", "/Users/dev/work/dashboard/", "dashboard", "cwd"},
		{"home directory falls back", home, GenericFallback, "generic_fallback"},
		{"filesystem root falls back", "/", GenericFallback, "generic_fallback"},
		{"relative path falls back", "work/dashboard", GenericFallback, "generic_fallback"},
		{"empty path falls back", "", GenericFallback, "generic_fallback"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Resolve(context.Background(), tabWithCWD(tc.cwd))
			if got.Name != tc.want {
				t.Errorf("name = %q, want %q", got.Name, tc.want)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}

func TestResolveFallsBackToForegroundCWD(t *testing.T) {
	r := New(DefaultMaxLength, NewCWD())
	tab := state.TabState{
		ID: "wE:t1",
		Panes: map[string]*state.PaneState{
			"wE:p1": {ID: "wE:p1", ForegroundCWD: "/Users/dev/work/api", Focused: true},
		},
	}

	if got := r.Resolve(context.Background(), tab); got.Name != "api" {
		t.Errorf("name = %q, want %q", got.Name, "api")
	}
}

func TestResolveTabWithoutPanes(t *testing.T) {
	r := New(DefaultMaxLength, NewCWD())

	got := r.Resolve(context.Background(), state.TabState{ID: "wE:t1"})
	if got.Name != GenericFallback {
		t.Errorf("name = %q, want %q", got.Name, GenericFallback)
	}
}

func TestResolveTruncatesToMaxLength(t *testing.T) {
	long := strings.Repeat("x", 100)
	r := New(10, NewCWD())

	got := r.Resolve(context.Background(), tabWithCWD("/Users/dev/"+long))
	if len([]rune(got.Name)) != 10 {
		t.Errorf("name %q has %d runes, want 10", got.Name, len([]rune(got.Name)))
	}
}

func TestResolveIsDeterministic(t *testing.T) {
	r := New(DefaultMaxLength, NewCWD())
	tab := state.TabState{
		ID: "wE:t1",
		Panes: map[string]*state.PaneState{
			"wE:p1": {ID: "wE:p1", CWD: "/Users/dev/work/dashboard"},
			"wE:p2": {ID: "wE:p2", CWD: "/Users/dev/work/api"},
			"wE:p3": {ID: "wE:p3", CWD: "/Users/dev/work/infra"},
		},
	}

	first := r.Resolve(context.Background(), tab)
	// Map iteration order varies between runs; the decision must not.
	for i := 0; i < 50; i++ {
		if got := r.Resolve(context.Background(), tab); got != first {
			t.Fatalf("resolution %d = %+v, want %+v", i, got, first)
		}
	}
}

// higherSource stands in for the sources later slices add above CWD.
type higherSource struct {
	parts Parts
	ok    bool
}

func (higherSource) Name() string { return "test_source" }
func (s higherSource) Resolve(*state.PaneState) (Parts, bool) {
	return s.parts, s.ok
}

func TestHigherPrioritySourceSuppliesActivity(t *testing.T) {
	r := New(DefaultMaxLength,
		higherSource{parts: Parts{Activity: "Tests", Confidence: ConfidenceProcess}, ok: true},
		NewCWD(),
	)

	got := r.Resolve(context.Background(), tabWithCWD("/Users/dev/work/dashboard"))
	if got.Name != "dashboard · Tests" {
		t.Errorf("name = %q, want %q", got.Name, "dashboard · Tests")
	}
	if got.Reason != "test_source" || got.Confidence != ConfidenceProcess {
		t.Errorf("reason/confidence = %q/%d, want test_source/%d", got.Reason, got.Confidence, ConfidenceProcess)
	}
}

func TestHigherPrioritySourceOverridesContext(t *testing.T) {
	r := New(DefaultMaxLength,
		higherSource{parts: Parts{Context: "prod-01", Activity: "SSH", Confidence: ConfidenceSSH}, ok: true},
		NewCWD(),
	)

	got := r.Resolve(context.Background(), tabWithCWD("/Users/dev/work/dashboard"))
	if got.Name != "prod-01 · SSH" {
		t.Errorf("name = %q, want %q", got.Name, "prod-01 · SSH")
	}
}

func TestSourceThatDeclinesIsSkipped(t *testing.T) {
	r := New(DefaultMaxLength,
		higherSource{ok: false},
		NewCWD(),
	)

	got := r.Resolve(context.Background(), tabWithCWD("/Users/dev/work/dashboard"))
	if got.Name != "dashboard" || got.Reason != "cwd" {
		t.Errorf("decision = %+v, want dashboard via cwd", got)
	}
}
