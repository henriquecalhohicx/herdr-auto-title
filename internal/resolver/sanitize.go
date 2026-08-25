package resolver

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// Separator joins every part of a title, and there is only one: where a part
// came from is not something a separator can convey, and a second kind would
// ask the reader to learn a distinction they cannot see.
const Separator = " " + separatorRune + " "

const separatorRune = "›"

// trimmable are the characters a title must not start or end on: a separator
// left dangling by truncation says a part was lost without saying which.
const trimmable = " " + separatorRune

var (
	// ansiRe matches CSI sequences, OSC strings and single-character escapes.
	ansiRe = regexp.MustCompile(
		"\x1b\\[[0-9;?<>=]*[ -/]*[@-~]" +
			"|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)" +
			"|\x1b[@-Z\\\\-_]",
	)
	spaceRe      = regexp.MustCompile(`\s+`)
	sepRunRe     = regexp.MustCompile(separatorRune + `(?:\s*` + separatorRune + `)+`)
	sepSpacingRe = regexp.MustCompile(`\s*` + separatorRune + `\s*`)
)

// Sanitize turns an untrusted value into something safe to use as a tab label:
// ANSI escapes and control characters go, whitespace and separators are
// normalized, and the result is cut to maxLen columns. Empty means unusable.
func Sanitize(s string, maxLen int) string {
	s = ansiRe.ReplaceAllString(s, "")

	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)

	s = spaceRe.ReplaceAllString(s, " ")
	s = sepRunRe.ReplaceAllString(s, separatorRune)
	s = sepSpacingRe.ReplaceAllString(s, Separator)
	s = strings.Trim(s, trimmable)
	s = strings.TrimSpace(s)

	return truncate(s, maxLen)
}

// truncate cuts to maxWidth columns and leaves no dangling separator behind.
func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	head, rest := splitAtWidth(s, maxWidth)
	if rest == "" {
		return s
	}
	cut := strings.TrimRight(head, trimmable)
	return strings.TrimSpace(cut)
}

// splitAtWidth returns the longest prefix of s fitting in maxWidth terminal
// columns, cut between grapheme clusters. Neither runes nor bytes work here —
// see docs/architecture/sanitization.md.
func splitAtWidth(s string, maxWidth int) (head, rest string) {
	rest = s
	state := -1
	for width := 0; rest != ""; {
		_, next, clusterWidth, nextState := uniseg.FirstGraphemeClusterInString(rest, state)
		if width+clusterWidth > maxWidth {
			break
		}
		width, rest, state = width+clusterWidth, next, nextState
	}
	return s[:len(s)-len(rest)], rest
}

// Format assembles a title from its parts and sanitizes the result as a whole.
func Format(parts Parts, maxLen int) string {
	var b strings.Builder
	if parts.Context != "" {
		b.WriteString(parts.Context)
	}
	if parts.Activity != "" {
		if b.Len() > 0 {
			b.WriteString(Separator)
		}
		b.WriteString(parts.Activity)
	}
	return Sanitize(b.String(), maxLen)
}
