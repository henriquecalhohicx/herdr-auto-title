// Package resolver turns cached tab state into a tab title.
//
// Resolution is deterministic: identical state always yields an identical
// decision. No network call, no LLM, no transcript reading.
package resolver

import (
	"context"
	"strings"

	"herdr-auto-title/internal/state"
)

// DefaultMaxLength bounds a generated title.
const DefaultMaxLength = 64

// GenericFallback names a tab whose context tells us nothing.
const GenericFallback = "Shell"

// Confidence levels form the resolution ladder. Higher-priority sources carry
// higher confidence, and a source never overrides a field a higher-priority
// source already supplied.
const (
	ConfidenceFallback      = 10
	ConfidenceAgentName     = 20
	ConfidenceCWD           = 30
	ConfidenceGit           = 40
	ConfidenceSSH           = 60
	ConfidenceProcess       = 70
	ConfidenceTerminalTitle = 80
	ConfidenceAgent         = 90
)

// Parts are the components a source contributes to a title, formatted as
// "<context> · <activity>". A source may supply either or both.
type Parts struct {
	Context    string
	Activity   string
	Confidence int
}

// Source contributes title parts from a pane's context.
type Source interface {
	// Name identifies the source in the rename reason.
	Name() string
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

// New builds a resolver from sources given in priority order, highest first.
func New(maxLength int, sources ...Source) *Deterministic {
	if maxLength <= 0 {
		maxLength = DefaultMaxLength
	}
	return &Deterministic{sources: sources, maxLength: maxLength}
}

// Default builds the resolver Auto Title ships with: every source in priority
// order, highest first. Later tickets add their sources here, so the binary and
// the tests can never drift apart on what the chain contains.
func Default(maxLength int) *Deterministic {
	return New(maxLength,
		Agent{},
		TerminalTitle{},
		NewCWD(),
		AgentName{},
	)
}

// Resolve walks the sources in priority order. Each field is filled by the
// first source that supplies it, so a lower-priority source can complete a
// title without overriding what a higher-priority source already decided.
func (d *Deterministic) Resolve(_ context.Context, tab state.TabState) Decision {
	pane := state.SelectContextPane(tab)

	var (
		parts      Parts
		reason     string
		confidence int
	)

	for _, source := range d.sources {
		got, ok := source.Resolve(pane)
		if !ok {
			continue
		}
		if parts.Activity == "" && got.Activity != "" {
			parts.Activity = got.Activity
			reason = source.Name()
			confidence = got.Confidence
		}
		if parts.Context == "" && got.Context != "" {
			parts.Context = got.Context
			if reason == "" {
				reason = source.Name()
				confidence = got.Confidence
			}
		}
		if parts.Context != "" && parts.Activity != "" {
			break
		}
	}

	// A shell that titles its window after the directory would otherwise
	// produce "dashboard · dashboard".
	if strings.EqualFold(parts.Activity, parts.Context) {
		parts.Activity = ""
	}

	name := Format(parts, d.maxLength)
	if name == "" {
		return Decision{Name: GenericFallback, Confidence: ConfidenceFallback, Reason: "generic_fallback"}
	}
	return Decision{Name: name, Confidence: confidence, Reason: reason}
}
