# Herdr Auto Title

A [Herdr](https://herdr.dev) plugin that names your tabs for you.

Auto Title runs as a long-lived process next to Herdr, watches session events
over the Herdr socket, and keeps every tab's title in step with what that tab is
actually doing. It is written in Go and ships as a single executable: no Node,
Bun or Python runtime, no database, no external service, and no LLM.

> **Status: early.** Titles come from the terminal title, falling back to the
> pane's working directory. Agent context, known commands, SSH and Git land in
> later slices, and the resolver is already built as the priority ladder they
> slot into.

```
~/work/dashboard                            →  dashboard
~/work/dashboard, title "Fix OAuth redirect"→  dashboard · Fix OAuth redirect
$HOME                                       →  Shell
```

## Installation

Auto Title needs Go 1.24 or newer to build.

```sh
go build -o herdr-auto-title ./cmd/herdr-auto-title
```

For local development, link the repository as a plugin:

```sh
herdr plugin link /path/to/herdr-auto-title
```

Herdr launches the plugin through its startup hook and sets `HERDR_SOCKET_PATH`
in the environment; Auto Title refuses to start without it. To try the binary by
hand in a Herdr pane, just run `./herdr-auto-title` — the environment is already
set for you.

## Architecture

```
                    every 500 ms
                          │
                          ▼
                  session.snapshot ──► whole session, one request
                          │
                          ▼
              which panes changed? (revisions)
                          │
                          ▼
              per tab: pick the context pane
                          │
                          ▼
                  deterministic resolver
                          │
            title differs? ──no──► nothing
                          │
                         yes
                          ▼
                     tab.rename
```

Auto Title reads the session twice a second and renames what no longer fits.
There is no scrollback scanning and no LLM.

- **Polling, not events.** Herdr exposes an event stream and Auto Title
  deliberately ignores it. Subscribing replays a backlog before delivering
  anything live — measured at roughly the last 95 revisions of *every* pane,
  about ten a second, so some ten seconds of history per active pane — and live
  events queue behind it: a change made two seconds after subscribing arrived
  thirteen seconds later. The protocol offers no cursor to skip it. A snapshot
  describes the present, costs one request whatever the session holds, and
  carries every field the resolver reads. See
  [*Notes on the Herdr socket API*](#notes-on-the-herdr-socket-api).
- **Cost.** A snapshot of a six-pane session measured 0.47 ms and 6 KB, so two
  polls a second come to about a thousandth of a core.
- **Almost no state.** Each poll builds what it needs and discards it. The one
  thing carried forward is when each pane last changed — a snapshot says what a
  pane holds but not when that became true, and a tab with several panes and no
  focus is named after whichever moved last. Herdr's pane revisions are
  monotonic, so comparing polls answers that.
- **The interval is the rename rate.** A tab changes name at most once per poll,
  however fast its pane is churning. Raising
  `HERDR_AUTO_TITLE_POLL_MS` calms a tab that renames more often than you would
  like; lowering it makes renames land sooner.
- **Resolver.** Deterministic — identical state always yields an identical
  title. Every candidate value is treated as untrusted input.
- **Deduplication.** The snapshot reports each tab's current label, and a rename
  is skipped when the resolved title already equals it. This is what keeps the
  loop quiet, and what stops a rename from provoking the next one.

### Title sources

Titles are formatted as `<context> · <activity>`, capped at 64 characters. Each
field is filled by the highest-priority source that supplies it, so a
lower-priority source can complete a title without overriding a higher-priority
one.

| Priority | Source | Status |
|---------:|--------|--------|
| 1 | Manual rename protection | later slice |
| 2 | Meaningful agent title | **implemented** |
| 3 | Meaningful terminal title | **implemented** |
| 4 | Known foreground process | **implemented** |
| 5 | SSH session | **implemented** |
| 6 | Git context | **implemented** |
| 7 | Working directory | **implemented** |
| 8 | Generic fallback (`Shell`) | **implemented** |

A value is cleaned before it becomes part of a title, and a source declines when
nothing useful survives. Cleaning removes locations — absolute paths,
home-anchored paths, URIs — because the context half already says where the user
is, and a path is long enough to push the useful part past the length limit. An
editor titling its window `auth.ts (~/work/dashboard/src) - Nvim` contributes
`auth.ts - Nvim`; a shell titling it `~` contributes nothing. Relative paths
survive, so `Fix bug in src/auth.ts` stays intact. On top of that, a value that
only names a program or a shell — `zsh`, `node`, `Claude Code`, `Agent` — is
rejected outright, as is one that is nothing but a shell prompt: `root@psi:`,
`alex@macbook:~/work`. A prompt says who and where, which the context has
already said, and never says what the user is doing. An agent that echoes its own name is rejected too, whatever
that name is: it is compared against the agent Herdr recognized in the pane
rather than against the table.

A tab whose panes disagree is named after one of them, never after a blend of
both. The pane that speaks for the tab is the focused one; failing that, the one
running an agent that is working or waiting on the user; failing that, the most
recently updated. Both halves of the name then come from that pane alone.

On this Herdr the agent's own title field is rarely populated — `PaneInfo.title`
was null for every Claude Code pane observed — and the agent expresses what it is
working on through the terminal title instead. The agent source is what reads the
field when an agent does set it; in practice most agent context arrives one rung
lower, through the terminal title.

Two separators, and they mean different things. A middle dot separates where
from what: `self-care-portal · yarn dev`. A colon binds a kind of thing to the
particular one: `nvim:auth.provider.ts`. A project name never takes a colon,
because a project is a place rather than a kind.

An agent is asked for first, because Herdr recognizes it directly and its
process list does not — a coding agent shows up as a `caffeinate`, several
`node`s and an MCP helper, with its own name nowhere among them. Otherwise the
kind comes from the process table, and only when a pane runs a single program:
an editor reports as `nvim`, while a build tool reports as `esbuild` and five
`node`s. Picking one of those would be guesswork, and such a pane already has a
terminal title saying what it is doing. A kind that has nothing to add stands alone —
`dashboard · nvim` for an editor with no file open — and a title that already
carries its program's name loses it, since `nvim:auth.provider.ts - Nvim` says
the same thing twice.

A pane running `ssh` is named after the machine it reached, not the directory it
was launched from: `ssh:prod-01`, and `ssh:prod-01 · Restart the queue workers`
once the remote shell has something to report.

The `ssh:` mark goes on the host rather than into the activity slot, because the
activity is contested — a remote shell sets a terminal title, that title outranks
anything this source could put there, and the tab would stop saying it is remote
at exactly the moment it has most to say. Nothing else names a machine, so the
host slot has no such competition.

The user is dropped: `root@prod-01` and `deploy@prod-01` are the same machine,
and a tab bar has no room to say who is logged in. Options are parsed rather than
guessed at, so `ssh -p 2222 prod-01` and `ssh prod-01 tail -f /var/log/syslog`
both yield `prod-01`. A destination that cannot be read still marks the tab
remote, as `dashboard · SSH`.

Inside a repository the branch becomes the activity, so a tab reads
`dashboard · MC-13200`. Branch names are far too long to use whole — the ones
this was calibrated against averaged fifty characters — and branch conventions
vary too much to enumerate, so two rules reduce any of them:

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

Set `HERDR_AUTO_TITLE_BRANCH_MAX=0` to leave branches out of titles entirely.

The lookup never runs in the poll loop. Each poll answers from a cache and
starts a background refresh when its reading is missing or older than three
seconds, so a directory contributes no branch on the poll that first sees it and
its branch from then on. A stale reading is used while the refresh runs, so a
checkout takes a moment to show rather than making the tab flicker back to its
bare directory name. git is executed directly with arguments, never through a
shell, and no repository, a detached HEAD, a missing git and a timed-out lookup
are all simply "no branch".

An agent that has not decided on a topic yet titles its window after itself, so
`Claude Code` is generic as an activity and exactly right as a kind: the tab
reads `dashboard · claude` until there is something to report, then
`dashboard · claude:Implement OAuth scopes`.

Every value that reaches a title — a directory name, a terminal title, an agent
title — is stripped of ANSI escapes and control characters, whitespace
is normalized, repeated separators are collapsed, and the result is truncated to
the maximum length. Nothing derived from terminal output is ever passed to a
shell: renames go over the socket API, never through `sh -c`.

## Configuration

V1 has no configuration file. Three environment variables are read at startup;
an unusable value is logged as a warning and the default is kept.

| Variable | Default | Meaning |
|----------|---------|---------|
| `HERDR_AUTO_TITLE_DEBUG` | `false` | Log at DEBUG instead of INFO |
| `HERDR_AUTO_TITLE_POLL_MS` | `500` | How often the session is read; also the fastest a tab can be renamed |
| `HERDR_AUTO_TITLE_MAX_LENGTH` | `64` | Maximum title length in characters |
| `HERDR_AUTO_TITLE_BRANCH_MAX` | `12` | Most a git branch may add to a title; `0` leaves branches out |

Auto Title logs to stderr through `log/slog`. Raw terminal output and command
arguments are never logged.

## Troubleshooting

**The plugin exits immediately with "HERDR_SOCKET_PATH is not set".**
Auto Title has to be started by Herdr, or from inside a Herdr pane where the
variable is already exported.

**Nothing is renamed.**
That is the normal state once every tab carries the name it should — the loop
logs only when it acts. Run with `HERDR_AUTO_TITLE_DEBUG=1` and watch stderr:
`poll failed` means the snapshot is not coming back, and silence otherwise means
the resolver keeps producing the title each tab already has.

**A tab renames every time I switch files in my editor.**
Expected: the tab follows the editor's title, which follows the buffer. Raise
`HERDR_AUTO_TITLE_POLL_MS` to slow it down — the interval is also the fastest a
tab can change name.

**A tab keeps the name I gave it.**
That is intended once manual rename protection lands. In this slice Auto Title
does not yet detect manual renames, so it will overwrite a hand-picked name on
the next context change.

**Everything is called `Shell`.**
The pane's working directory is the home directory or the filesystem root, which
carry no context. Later slices add process, agent and terminal-title sources
that name such tabs by what is running in them.

**The plugin survives a Herdr restart.**
Every poll dials a fresh connection, so there is nothing to reconnect: failed
polls are logged and the loop keeps trying. Only a failure of the very first
poll ends the process, because there is nothing to run for.

## Development

```sh
make          # list every target
make check    # fmt + vet + go test -race ./...
make run      # build and run in your Herdr session with DEBUG logging
make dev      # the same, restarting on every source change
```

The test suite drives the whole loop through a stub Herdr client
(`internal/herdr.StubClient`), so a tab appearing, its context changing,
deduplication and failed calls are all exercised without a running Herdr.

`scripts/probe.py` (wrapped by the `make probe-*` targets) inspects the live
socket: the session snapshot, and — as a diagnostic for a stream the plugin does
not use — the accepted subscription types and the raw events. Use it before
assuming anything about the API.

The full workflow — the three loops, the rules for running against your own
session, and what each log line means — is in
[docs/development.md](docs/development.md).

### Notes on the Herdr socket API

Verified against Herdr 0.8.2, protocol 20:

- The transport is newline-delimited JSON over the socket at
  `HERDR_SOCKET_PATH`. Requests are `{"id","method","params"}` — `params` is
  required even when empty — and replies are `{"id","result"}` or
  `{"id","error"}`.
- **One request per connection.** Herdr closes it after answering, so every call
  dials its own. Auto Title uses exactly two methods: `session.snapshot` and
  `tab.rename`.
- `session.snapshot` returns the whole session — every tab with its label, every
  pane with its directory, terminal title, agent and agent status. Measured at
  0.47 ms and 6 KB for six panes.
- **On subscribe, Herdr replays a backlog** before delivering anything live:
  roughly the last 95 revisions of every pane, paced at about ten a second, so
  around ten seconds of history for each active pane, closed panes included.
  Live events queue behind it — a change made two seconds after subscribing was
  observed arriving thirteen seconds later. There is no way to skip it:
  `events.subscribe` takes only a subscription list, event envelopes carry no
  timestamp or sequence number, and no method exposes a cursor. This is why Auto
  Title polls.
- Subscription types would use dot notation (`pane.updated`) while the events
  they deliver arrive with snake_case kinds (`pane_updated`), wrapped as
  `{"event": "...", "data": {...}}`. `pane.output_changed` is a real event kind
  but is **not** an accepted subscription type. `pane.agent_status_changed`,
  `pane.scroll_changed` and `pane.output_matched` are **per-pane** and rejected
  without a `pane_id`; `pane.agent_detected` is global.
- `pane_closed` and `pane_agent_detected` carry only pane identifiers — neither
  names the tab.
- `PaneInfo` does **not** include the foreground process name; that requires the
  separate `pane.process_info` method, which is asked per pane and measured at
  0.11 ms — less than the snapshot itself. Its `foreground_processes` holds the
  pane's foreground process *and that process's descendants*, each with `name`
  and a nullable `argv`.
- `PaneInfo.title` is the agent's own title, and Herdr left it null for every
  Claude Code pane observed; that agent reports its topic through
  `terminal_title_stripped` instead.
- `agent_status` is one of `idle`, `working`, `blocked`, `done`, `unknown`. Every
  pane carries one; a pane with no agent reports `unknown`.
- Pane revisions are monotonic per pane, which is how one poll tells which panes
  moved since the last.
- `tab.get` and `pane.get` read one object each; `pane.list` filters by
  workspace only, not by tab.
- A malformed request is answered with an uncorrelated error frame and the
  connection is then closed.

## License

MIT — see [LICENSE](LICENSE).
