package resolver

import "herdr-auto-title/internal/state"

// TerminalTitle derives the activity from the pane's terminal title.
//
// It outranks the working directory: a title a program went out of its way to
// set usually says what is happening, while the directory only says where. It
// contributes an activity and no context, so a meaningful title combines with
// the directory into `<context> › <activity>`.
//
// When the pane names a single program, that program qualifies the title:
// `nvim:auth.provider.ts` rather than `auth.provider.ts - Nvim`. The program is
// read from the process table rather than guessed at from the title's shape,
// and the title then loses the name it was already carrying.
type TerminalTitle struct{}

var _ Source = TerminalTitle{}

// NewTerminalTitle builds the source.
func NewTerminalTitle() TerminalTitle { return TerminalTitle{} }

func (TerminalTitle) Name() string    { return "terminal_title" }
func (TerminalTitle) Confidence() int { return ConfidenceTerminalTitle }

func (TerminalTitle) Resolve(pane *state.PaneState) (Parts, bool) {
	if pane == nil {
		return Parts{}, false
	}

	// Herdr strips escapes and decorative prefixes for us; the raw field is
	// only a fallback for when it has not.
	title := pane.TerminalTitle
	if title == "" {
		title = pane.TerminalTitleRaw
	}

	// Truncation belongs to the assembled name, so no limit is applied here.
	activity, ok := Meaningful(Sanitize(title, 0))
	if !ok {
		return Parts{}, false
	}

	return Parts{Activity: qualify(activity, paneKind(pane))}, true
}
