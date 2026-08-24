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

// punctuation wraps and joins words inside titles such as
// `Makefile (~/work/dashboard) - Nvim`.
const punctuation = `()[]{}<>"'` + ",;:-–—|"

// Meaningful cleans an untrusted value for use as part of a title and reports
// whether anything useful survived.
//
// Cleaning removes locations. Editors title their window with the path of the
// file they hold — `auth.ts (~/work/dashboard/src) - Nvim` — and the context
// half of a title already says where the user is, so the path is both redundant
// and long enough to push the useful part past the length limit. What remains,
// `auth.ts - Nvim`, is what the user actually wanted to know.
//
// A value that is nothing but a location, such as the `~` a shell sets, leaves
// nothing behind and is rejected.
func Meaningful(value string) (string, bool) {
	cleaned := stripLocations(strings.TrimSpace(value))
	if cleaned == "" {
		return "", false
	}
	if _, generic := genericValues[strings.ToLower(cleaned)]; generic {
		return "", false
	}
	return cleaned, true
}

// stripLocations removes every word that is an absolute path, a home-anchored
// path or a URI, then tidies up the punctuation those words left behind.
//
// Relative paths survive: `Fix bug in src/auth.ts` describes work rather than a
// place.
func stripLocations(value string) string {
	words := strings.Fields(value)
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if isLocation(strings.Trim(word, punctuation)) {
			continue
		}
		kept = append(kept, word)
	}
	return tidy(kept)
}

func isLocation(word string) bool {
	switch {
	case word == "~", strings.HasPrefix(word, "~/"):
		return true
	case strings.HasPrefix(word, "/"):
		return true
	default:
		return uriPattern.MatchString(word)
	}
}

// tidy joins words back together, dropping the punctuation that only made sense
// around a word that has been removed: `- (oil:///work) - Nvim` loses its path
// and must not become `- - Nvim`.
func tidy(words []string) string {
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if !isPunctuationOnly(word) {
			kept = append(kept, word)
			continue
		}
		// A separator is only worth keeping between two real words.
		if len(kept) == 0 || isPunctuationOnly(kept[len(kept)-1]) {
			continue
		}
		kept = append(kept, word)
	}
	for len(kept) > 0 && isPunctuationOnly(kept[len(kept)-1]) {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, " ")
}

func isPunctuationOnly(word string) bool {
	return strings.Trim(word, punctuation) == ""
}
