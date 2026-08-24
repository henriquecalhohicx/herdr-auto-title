package resolver

import (
	"regexp"
	"strings"
)

// genericValues name a program, a shell or a place rather than what the user is
// doing there. A title made of one of these tells a reader nothing the tab does
// not already show, so a source that finds one declines and the resolver falls
// through to something more specific.
//
// Keys are lower-cased; lookups normalize before matching. Later sources add
// their own entries here rather than growing their own lists.
var genericValues = map[string]struct{}{
	// Shells.
	"bash":  {},
	"zsh":   {},
	"sh":    {},
	"fish":  {},
	"shell": {},
	// Terminals and runtimes.
	"terminal": {},
	"node":     {},
	// Agents naming themselves instead of their work.
	"claude":      {},
	"claude code": {},
}

// uriPattern matches a scheme-qualified location such as oil:///home/dev.
var uriPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

// punctuation wraps paths inside titles like `Makefile (~/work/dashboard)`.
const punctuation = `()[]{}<>"'` + ",;:"

// IsGeneric reports whether a value is too generic to become part of a title.
//
// Besides the table, anything that carries a location is generic: the context
// half of a title already says where the user is, so repeating it wastes the
// budget and buries whatever the title actually said.
func IsGeneric(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}
	if _, ok := genericValues[strings.ToLower(trimmed)]; ok {
		return true
	}
	return carriesLocation(trimmed)
}

// carriesLocation reports whether any word of value is an absolute path, a
// home-anchored path or a URI.
//
// Editors title their window this way — `Makefile (~/work/dashboard) - Nvim`,
// `- (oil:///home/dev/work) - Nvim` — and such titles change with every
// keystroke, so accepting them renames a tab repeatedly to a location the tab
// already displays. Relative paths stay acceptable: `Fix bug in src/auth.ts`
// describes work rather than a place.
func carriesLocation(value string) bool {
	for _, word := range strings.Fields(value) {
		word = strings.Trim(word, punctuation)
		switch {
		case word == "~", strings.HasPrefix(word, "~/"):
			return true
		case strings.HasPrefix(word, "/"):
			return true
		case uriPattern.MatchString(word):
			return true
		}
	}
	return false
}
