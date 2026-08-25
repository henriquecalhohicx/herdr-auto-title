package resolver

import (
	"regexp"
	"strings"
	"unicode"
)

// Separator joins every part of a title, and there is only one.
//
// A title reads as a path from the general to the particular:
// `self-care-portal › nvim › auth.provider.ts`. Where a part came from — a
// directory, a program, a file — is not something a separator can convey, and a
// second one only asks the reader to learn a distinction they cannot see.
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

// Sanitize turns an untrusted value — a terminal title, an agent title, a
// branch name, a path — into something safe to use as a tab label.
//
// It strips ANSI escapes, drops control characters, normalizes whitespace,
// collapses repeated separators, trims, and truncates to maxLen runes. An empty
// result means the value carries nothing usable.
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

// truncate cuts to maxLen runes, never mid-rune, and leaves no dangling
// separator behind.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	cut := strings.TrimRight(string(runes[:maxLen]), trimmable)
	return strings.TrimSpace(cut)
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
