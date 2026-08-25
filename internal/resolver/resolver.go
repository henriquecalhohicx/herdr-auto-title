// Package resolver turns a tab's read state into a tab title.
//
// Resolution is deterministic: identical state always yields an identical
// decision. No network call, no LLM, no transcript reading.
package resolver

import (
	"context"
	"sort"
	"strings"

	"github.com/kryptamine/herdr-auto-title/internal/state"
)

// DefaultMaxLength bounds a generated title, in columns of the tab bar.
const DefaultMaxLength = 64

// GenericFallback names a tab whose context tells us nothing.
const GenericFallback = "Shell"

// Confidence levels form the resolution ladder. Each source returns its own
// through Source.Confidence, and the resolver orders itself by them, so the
// ladder is stated once rather than being implied a second time by the order
// sources happen to be listed in. A source never overrides a field a
// higher-priority source already supplied.
//
// They live in one block because the numbering is shared: a source's place is
// only meaningful relative to the others, and a gap left free here is what
// makes room for the next one.
const (
	ConfidenceFallback      = 10
	ConfidenceCWD           = 30
	ConfidenceSSH           = 60
	ConfidenceProcess       = 70
	ConfidenceTerminalTitle = 80
	ConfidenceAgent         = 90
)

// Parts are the components a source contributes to a title, formatted as
// "<context> › <activity>". A source may supply either or both.
type Parts struct {
	Context  string
	Activity string
}

// Source contributes title parts from a pane's context.
type Source interface {
	// Name identifies the source in the rename reason.
	Name() string
	// Confidence is the source's place on the resolution ladder. It belongs to
	// the source rather than to each result it returns: a source is trusted for
	// what it reads, not for what it happened to find this time.
	Confidence() int
	// Resolve reports the parts this source derives, or false when the pane
	// carries nothing this source recognizes.
	Resolve(pane *state.PaneState) (Parts, bool)
}

// Decision is the outcome of resolving a tab.
type Decision struct {
	Name       string
	Confidence int
	Reason     string
}

// TitleResolver produces a title for a tab.
type TitleResolver interface {
	Resolve(ctx context.Context, tab state.TabState) Decision
}

// Deterministic resolves titles from a fixed priority list of sources.
type Deterministic struct {
	sources   []Source
	maxLength int
}

var _ TitleResolver = (*Deterministic)(nil)

// New builds a resolver from sources, ordering them by confidence.
//
// The order is derived rather than trusted. Listing sources out of ladder order
// used to leave the numbers saying one thing and the behaviour doing another,
// with nothing to catch it; now the numbers decide. Equal confidences keep the
// order they were given.
func New(maxLength int, sources ...Source) *Deterministic {
	if maxLength <= 0 {
		maxLength = DefaultMaxLength
	}
	ordered := append([]Source(nil), sources...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Confidence() > ordered[j].Confidence()
	})
	return &Deterministic{sources: ordered, maxLength: maxLength}
}

// Default builds the resolver Auto Title ships with: every source there is.
// Later tickets add their sources here, so the binary and the tests can never
// drift apart on what the chain contains. The order below is the ladder's, but
// only for reading — New sorts by confidence regardless.
func Default(maxLength int) *Deterministic {
	return New(maxLength,
		NewAgent(),
		NewTerminalTitle(),
		NewProcess(),
		NewSSH(),
		NewCWD(),
	)
}

// Resolve names a tab in three steps: ask the sources what they see, drop the
// parts that only repeat something already on screen, and assemble the rest.
func (d *Deterministic) Resolve(_ context.Context, tab state.TabState) Decision {
	found := d.collect(state.SelectContextPane(tab))
	found.parts = withoutRepetition(found.parts, tab.WorkspaceName)

	name := Format(found.parts, d.maxLength)
	if name == "" {
		return Decision{Name: GenericFallback, Confidence: ConfidenceFallback, Reason: "generic_fallback"}
	}
	return Decision{Name: name, Confidence: found.confidence, Reason: found.reason}
}

// collected is what the chain produced: the parts of a title, and the source
// that answers for it.
type collected struct {
	parts      Parts
	reason     string
	confidence int
}

// collect walks the sources in ladder order, filling each field with the first
// source that supplies it.
//
// The two fields are filled independently, so a source low on the ladder can
// complete a title a higher one only half answered: an agent says what a tab is
// doing, and the working directory still says where.
func (d *Deterministic) collect(pane *state.PaneState) collected {
	var found collected
	for _, source := range d.sources {
		parts, ok := source.Resolve(pane)
		if !ok {
			continue
		}
		found.take(source, parts)
		if found.complete() {
			break
		}
	}
	return found
}

// take fills whatever this source supplies and nothing already has.
func (c *collected) take(source Source, parts Parts) {
	// The activity is what a title is about, so its source answers for the
	// title whenever one turns up. A context is credited only while no activity
	// has been found.
	if c.parts.Activity == "" && parts.Activity != "" {
		c.parts.Activity = parts.Activity
		c.credit(source)
	}
	if c.parts.Context == "" && parts.Context != "" {
		c.parts.Context = parts.Context
		if c.reason == "" {
			c.credit(source)
		}
	}
}

func (c *collected) credit(source Source) {
	c.reason = source.Name()
	c.confidence = source.Confidence()
}

func (c collected) complete() bool {
	return c.parts.Context != "" && c.parts.Activity != ""
}

// withoutRepetition drops the parts of a title that only say again what the
// reader can already see.
func withoutRepetition(parts Parts, workspace string) Parts {
	// A shell that titles its window after its directory would otherwise
	// produce `dashboard › dashboard`.
	if strings.EqualFold(parts.Activity, parts.Context) {
		parts.Activity = ""
	}

	// Herdr shows the workspace above its tabs, so a tab in the workspace it is
	// named after spends half its width repeating what is already on screen.
	// Dropped only when something else remains: a tab reduced to nothing has
	// lost more than it saved.
	if parts.Activity != "" && strings.EqualFold(parts.Context, workspace) {
		parts.Context = ""
	}

	return parts
}
