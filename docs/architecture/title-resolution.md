---
type: doc
title: 'Title Resolution'
description: 'How a tab becomes a name: which pane speaks for the tab, the confidence ladder the sources order themselves by, what each source contributes, and the rules that keep a title from repeating itself.'
tags: [architecture]
created: 2026-08-25
generated: { by: claude-code/opus-5, at: 2026-08-25T12:46:22+03:00 }
---

# Title Resolution

A title reads as a path from the general to the particular, with one separator
throughout:

```
self-care-portal › nvim › auth.provider.ts
```

Where a part came from — a directory, a program, a file — is not something a
separator can convey, and a second one would only ask the reader to learn a
distinction they cannot see. So there is exactly one, `Separator`
(`internal/resolver/sanitize.go`).

Structurally a title is two fields, `Parts{Context, Activity}`
(`internal/resolver/resolver.go`): *where* the user is and *what* they are
doing.

## One pane speaks for the tab

A tab holding several panes is named after one of them, never after a blend of
both. `SelectContextPane` (`internal/state/tab.go`) picks, in order:

1. the focused pane;
2. failing that, a pane running an agent that is `working` or `blocked` — a
   split where the user left an agent running is about that agent, even though
   the pane below it saw the last update;
3. failing that, the pane that changed most recently.

Ties break on the most recent change and then on pane ID, so identical state
always yields the same choice. Both halves of the name then come from that pane
alone.

## The confidence ladder

Each source states its own place, and the resolver sorts itself by those numbers
rather than by the order the sources happen to be listed in
(`internal/resolver/resolver.go`):

| Confidence | Source | File | Contributes |
|---:|---|---|---|
| 90 | Agent title | `agent.go` | Activity |
| 80 | Terminal title | `terminal.go` | Activity |
| 70 | Foreground process | `process.go` | Activity |
| 60 | SSH session | `ssh.go` | Context (and Activity when bare) |
| 40 | Git branch | `git.go` | Activity |
| 30 | Working directory | `cwd.go` | Context |
| 10 | Generic fallback | `resolver.go` | the whole name (`Shell`) |

**A source never overrides a field a higher-priority source already supplied**,
but a lower one can still complete the other half. That is why the working
directory at 30 fills the context of a title whose activity came from an agent
at 90.

The numbering lives in one block because a source's place is only meaningful
relative to the others, and the gaps are what make room for the next one.

## What each source knows

### Agent

Herdr recognizes agents directly, and their process lists do not: a coding agent
shows up as a `caffeinate`, several `node`s and an MCP helper, with its own name
nowhere among them. When an agent reports a title, that title is the most direct
statement of what a tab is for that Auto Title will ever see, so it outranks
everything.

In practice most agent context arrives one rung lower. `PaneInfo.title` was null
for every Claude Code pane observed; that agent reports its topic through the
terminal title instead. An agent that echoes its own name (`Claude Code`) is
rejected as an activity — it is compared against the agent Herdr recognized in
the pane rather than against a list — and reappears as a *kind*, so a tab reads
`dashboard › claude` until there is something to report and
`dashboard › claude › Implement OAuth scopes` after.

### Terminal title

The richest source in practice, and the one that carries most agent context. Its
value is cleaned hard before it is trusted — see
[sanitization](./sanitization.md).

### Foreground process

Only a lone process names a pane. An editor reports as `nvim`; a build tool
reports as `esbuild` and five `node`s, and picking one of those would be
guesswork when the pane's terminal title already says what it is doing. A shell
as the foreground process means there is no activity, not that the shell is the
name.

What this source produces is a **kind** — the program, not the work.
`qualify` binds a kind to whatever a higher source found, and `stripKind` drops
a kind a detail already carries, so `nvim › auth.provider.ts - Nvim` does not say
the same thing twice. A kind with nothing left to add stands alone:
`dashboard › nvim` for an editor with no file open.

A mapping from command lines to friendlier names (`yarn dev` → `Dev`) was
specified and is deliberately not built: the commands it would map are invisible
in the process table, visible only in the terminal title, and a source below the
terminal title can never fill an activity the terminal title has already filled.

### SSH

A pane running `ssh` is named after the machine it reached, not the directory it
was launched from: `ssh › prod-01`, and
`ssh › prod-01 › Restart the queue workers` once the remote shell has something
to report.

**The mark goes on the host rather than into the activity slot**, because the
activity is contested — a remote shell sets a terminal title, that title
outranks anything this source could put there, and the tab would stop saying it
is remote at exactly the moment it has most to say. Nothing else names a
machine, so the host slot has no such competition.

The user is dropped: `root@prod-01` and `deploy@prod-01` are the same machine,
and a tab bar has no room to say who is logged in. Options are parsed rather
than guessed at, so `ssh -p 2222 prod-01` and
`ssh prod-01 tail -f /var/log/syslog` both yield `prod-01`. A destination that
cannot be read still marks the tab remote, as `dashboard › SSH`.

### Git

Inside a repository the branch becomes the activity, so a tab reads
`dashboard › MC-13200`. Branch names are far too long to use whole — the ones
this was calibrated against averaged fifty characters and reached ninety — and
**branch conventions vary too much to enumerate**, so nothing here is a list of
known prefixes. A list only ever fits the team it was written for. Two rules
reduce any convention:

| Branch | Contributes |
|--------|-------------|
| `bugfix-asatretdinov-cpanel-uapi-body-arguments-mc-13675` | `MC-13675` |
| `bugfix-MC-12722-sql-injection-operations-summary` | `MC-12722` |
| `feature/MC-13200` | `MC-13200` |
| `refactor-the-poller` | `refactor-the` |
| `alex/oauth-scopes` | `oauth-scopes` |
| `main`, `master` | nothing |

**An issue key wins outright.** It identifies the work, it is short, and it
survives whatever a convention wraps around it — a team whose branches all begin
`bugfix-<author>-` gets eight characters that distinguish rather than eight that
do not. **Otherwise the beginning is kept and cut at a separator**, so the result
ends on a whole word, and any namespace is dropped since every branch in the
repository carries the same one. The trunk contributes nothing: a tab in a
repository it is already named after learns nothing from being told it is on
`main`.

#### Git runs outside the loop

The lookup never runs in the poll loop. `Git.Resolve` answers from a cache and,
when a reading is missing or older than `GitTTL` (3 s), starts a background
refresh that the next poll picks up. A directory therefore contributes no branch
on the poll that first sees it, and its branch from then on. A stale reading is
used while the refresh runs, so a checkout takes a moment to show rather than
making the tab flicker back to its bare directory name.

The cache is the one piece of state not rebuilt from each snapshot — it is keyed
by directory, not by anything the session lists — so it prunes itself: a reading
that has aged well past the TTL belongs to a pane that has moved on, because a
directory still in use is asked about on every poll.

`git` is executed directly with arguments, never through a shell, with
`GIT_OPTIONAL_LOCKS=0` and `GIT_TERMINAL_PROMPT=0` and a 2 s timeout. No
repository, a detached HEAD, a missing `git` and a timed-out lookup are all
simply "no branch".

### Working directory and the fallback

The basename of the pane's directory, which is normally the project name. The
shell's own `cwd` is preferred over `foreground_cwd`, which follows whatever is
running right now. Directories that say nothing — the home directory, the
filesystem root, a relative path — yield nothing, and a tab left with no name at
all becomes `Shell`.

## The workspace is not repeated

Herdr shows the workspace above its tabs, so a tab in the workspace it is named
after spends half its width repeating what is already on screen. That half is
dropped: in a workspace called `dashboard`, a tab reads `nvim › auth.ts` rather
than `dashboard › nvim › auth.ts`.

It is dropped only when something else remains — a tab reduced to nothing has
lost more than it saved — and only on an exact match, so a tab whose directory
has left its workspace behind is exactly the one that keeps saying where it is.
