package resolver

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kryptamine/herdr-auto-title/internal/git"
	"github.com/kryptamine/herdr-auto-title/internal/state"
)

// DefaultBranchMaxLength bounds what a branch may contribute to a title, in
// columns of the tab bar.
//
// Twelve columns hold a tracker key, or a short word and part of the next, and
// leave a tab readable beside a dozen others. Real branch names run far past
// it: the ones this was calibrated against averaged fifty characters and
// reached sixty-two.
const DefaultBranchMaxLength = 12

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

// Git names the branch the pane's repository has checked out.
//
// It contributes to the **context**, beside the directory, rather than to the
// activity: a branch says which slice of a project a tab is on, which is part
// of where the user is and not of what they are doing. That is also what makes
// it useful — the activity is contested by the terminal title, and a branch
// filed there spoke only for a plain shell. See docs/architecture/
// title-resolution.md.
type Git struct {
	maxLength int
}

var _ Source = Git{}

// NewGit builds the source. A maxLength of zero or less leaves branches out of
// titles entirely.
func NewGit(maxLength int) Git { return Git{maxLength: maxLength} }

func (Git) Name() string    { return "git" }
func (Git) Confidence() int { return ConfidenceGit }

func (g Git) Resolve(pane *state.PaneState) (Parts, bool) {
	if pane == nil || g.maxLength <= 0 {
		return Parts{}, false
	}

	// The branch is read from the directory ssh was launched in, which says
	// nothing about the machine the tab is showing.
	if _, remote := sshArgs(pane); remote {
		return Parts{}, false
	}

	branch := branchLabel(pane.Git, g.maxLength)
	if branch == "" {
		return Parts{}, false
	}
	return Parts{Branch: branch}, true
}

// branchLabel reduces a checkout to what a tab says about it, or "" when it
// says nothing worth the width.
func branchLabel(checkout git.Checkout, maxLength int) string {
	if checkout.Branch == "" {
		// A detached HEAD is the one place a bare hash belongs in a title: it
		// is where commits get lost, and no name is being left behind.
		return Sanitize(checkout.Commit, 0)
	}
	// A tab in a repository it is already named after learns nothing from
	// being told it is on that repository's default branch.
	if strings.EqualFold(checkout.Branch, checkout.Default) {
		return ""
	}
	return shortenBranch(Sanitize(checkout.Branch, 0), maxLength)
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
//   - A name that already fits is left alone: dropping `feat/` from a branch
//     with room to spare would throw away what distinguishes it from `fix/`.
//   - Otherwise drop the namespace, which every branch in the repository
//     carries alike, and cut at a separator so the result ends on a whole word.
func shortenBranch(branch string, maxLength int) string {
	branch = strings.Trim(branch, branchSeparators)
	if branch == "" || maxLength <= 0 {
		return ""
	}

	// A key is atomic: cutting it leaves something that identifies nothing, so
	// it is the one value allowed past maxLength.
	if key := trackerKey.FindString(branch); key != "" {
		return strings.ToUpper(key)
	}

	if _, over := splitAtWidth(branch, maxLength); over == "" {
		return branch
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
