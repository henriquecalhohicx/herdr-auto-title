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
Herdr socket (NDJSON)
        │
        ▼
  socket reader ──► event channel ──► event router
                                          │
                                          ▼
                                    session index
                                          │
                                          ▼
                                 per-tab debounce (200ms)
                                          │
                                          ▼
                              bounded reconciliation workers
                                          │
                                          ▼
                          read the tab: tab.get + pane.get
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

Auto Title reacts to changes in terminal context; it does not continuously try
to understand the terminal. There is no polling loop and no scrollback scanning.

- **Bootstrap.** On connect it subscribes to events, calls `session.snapshot`
  once to seed the index, and reconciles the tabs that already exist.
- **Events are triggers, not data.** An event says which tab may have changed
  and nothing more. Subscribing makes Herdr replay a backlog — roughly the last
  hundred revisions of every pane, about ten a second — so a payload describes
  some past moment, and a title built from one would name a tab after a file the
  user closed minutes ago. Only pane identity and revision are kept; the
  revision is what makes a replayed event recognizable and cheap to ignore.
- **Read before deciding.** When a tab's timer fires, its state is read back
  with `tab.get` and one `pane.get` per pane. That read is the only source of a
  title, so a stale trigger can cost a redundant read and nothing worse. It also
  supplies the tab's real current label, which is what deduplication compares
  against.
- **Router.** The socket reader never does expensive work. Events go through a
  channel to a router that updates the index and arms a timer.
- **Debounce.** Each tab has its own 200 ms timer. A burst of events on one tab
  produces exactly one reconciliation once the burst settles; tabs never block
  each other. A burst can also be *endless* — a pane running an agent emits an
  update roughly every 100 ms for as long as the agent works — so an action
  always runs within five debounce windows of the first event, however long the
  burst lasts. Without that cap such a tab would never be titled at all. Raising
  `HERDR_AUTO_TITLE_DEBOUNCE_MS` raises the cap with it, which is the way to
  calm a tab that renames more often than you would like.
- **Resolver.** Deterministic — identical state always yields an identical
  title. Every candidate value is treated as untrusted input.
- **Deduplication.** If the resolved title equals the tab's current title, no
  rename request is sent at all. This is what keeps rename → `tab_renamed` →
  resolve from becoming a loop.

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
| 4 | Known foreground process | later slice |
| 5 | SSH session | later slice |
| 6 | Git context | later slice |
| 7 | Working directory | **implemented** |
| 8 | Agent name | **implemented** |
| 9 | Generic fallback (`Shell`) | **implemented** |

A value is cleaned before it becomes part of a title, and a source declines when
nothing useful survives. Cleaning removes locations — absolute paths,
home-anchored paths, URIs — because the context half already says where the user
is, and a path is long enough to push the useful part past the length limit. An
editor titling its window `auth.ts (~/work/dashboard/src) - Nvim` contributes
`auth.ts - Nvim`; a shell titling it `~` contributes nothing. Relative paths
survive, so `Fix bug in src/auth.ts` stays intact. On top of that, a value that
only names a program or a shell — `zsh`, `node`, `Claude Code`, `Agent` — is
rejected outright. An agent that echoes its own name is rejected too, whatever
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

An agent has no topic to report the moment it starts, and Claude Code titles its
window `Claude Code` until the conversation has a subject. That name is generic
everywhere else, but on a pane that genuinely runs the agent it is the one thing
worth saying, so the bottom rung of the chain says it: a tab with an idle agent
reads `dashboard · Claude Code` while a plain shell in the same directory reads
`dashboard`. It fills only an activity nothing else claimed, and the sources
above take over the moment the agent has something to report. The name comes from
Herdr's `display_agent` when there is one, from the terminal title when the agent
titled its window after itself, and from the matched identifier otherwise.

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
| `HERDR_AUTO_TITLE_DEBOUNCE_MS` | `200` | Per-tab debounce window; the cap on a continuous burst is five times this |
| `HERDR_AUTO_TITLE_MAX_LENGTH` | `64` | Maximum title length in characters |

Auto Title logs to stderr through `log/slog`. Raw terminal output and command
arguments are never logged.

## Troubleshooting

**The plugin exits immediately with "HERDR_SOCKET_PATH is not set".**
Auto Title has to be started by Herdr, or from inside a Herdr pane where the
variable is already exported.

**Nothing is renamed.**
Run with `HERDR_AUTO_TITLE_DEBUG=1` and watch stderr. `event received` lines
confirm the subscription is live; `title unchanged` means the resolver produced
exactly the title the tab already has.

**A busy tab is titled a second late.**
Reconciliation for a continuously updating pane runs on the cap — five debounce
windows, one second by default — rather than on the quiet window. That is the
cap doing its job.

**A tab renames every time I switch files in my editor.**
Expected: the tab follows the editor's title, which follows the buffer. Raise
`HERDR_AUTO_TITLE_DEBOUNCE_MS` to slow it down.

**A tab keeps the name I gave it.**
That is intended once manual rename protection lands. In this slice Auto Title
does not yet detect manual renames, so it will overwrite a hand-picked name on
the next context change.

**Everything is called `Shell`.**
The pane's working directory is the home directory or the filesystem root, which
carry no context. Later slices add process, agent and terminal-title sources
that name such tabs by what is running in them.

**The plugin stops after Herdr restarts.**
Reconnect is a later slice. For now a dropped socket ends the process with the
reason logged; restart Herdr's session or relaunch the binary.

## Development

```sh
make          # list every target
make check    # fmt + vet + go test -race ./...
make run      # build and run in your Herdr session with DEBUG logging
make dev      # the same, restarting on every source change
```

The test suite drives the whole pipeline through a fake Herdr client
(`internal/herdr.FakeClient`), so tab creation, context changes, debounce
collapsing, deduplication and disconnects are all exercised without a running
Herdr.

`scripts/probe.py` (wrapped by the `make probe-*` targets) inspects the live
socket: accepted subscription types, the raw event stream, the bootstrap
snapshot. Use it before assuming anything about the API.

The full workflow — the three loops, the rules for running against your own
session, and what each log line means — is in
[docs/development.md](docs/development.md).

### Notes on the Herdr socket API

Verified against Herdr 0.8.2, protocol 20:

- The transport is newline-delimited JSON over the socket at
  `HERDR_SOCKET_PATH`. Requests are `{"id","method","params"}` — `params` is
  required even when empty — and replies are `{"id","result"}` or
  `{"id","error"}`.
- Subscription types use dot notation (`pane.updated`) while the events they
  deliver arrive with snake_case kinds (`pane_updated`), wrapped as
  `{"event": "...", "data": {...}}`.
- `pane_closed` carries only `pane_id` and `workspace_id`, so the cache indexes
  panes by ID as well as by tab.
- `PaneInfo` does **not** include the foreground process name; that requires the
  separate `pane.process_info` method.
- `pane.agent_status_changed` is a **per-pane** subscription and is rejected
  without a `pane_id`; so are `pane.scroll_changed` and `pane.output_matched`.
  Auto Title needs none of them: `pane_updated` resends the whole `PaneInfo`,
  agent fields included, whenever an agent's status or title changes.
  `pane.agent_detected` is global.
- `pane_agent_detected` carries only `pane_id`, `workspace_id`, `agent`,
  `final_status` and `released` — like `pane_closed` it does not name the tab.
- `agent_status` is one of `idle`, `working`, `blocked`, `done`, `unknown`. Every
  pane carries one; a pane with no agent reports `unknown`.
- On subscribe, Herdr replays a backlog of pane updates before the live ones —
  measured at roughly the last 95 revisions **per pane**, delivered about ten a
  second, so the drain costs about ten seconds for every active pane. Live
  events queue behind it: a change made two seconds after subscribing was
  observed arriving thirteen seconds later. `events.subscribe` offers no way to
  opt out — its only parameter is the subscription list, event envelopes carry
  no timestamp or sequence number, and no method exposes a cursor. This is why
  events are treated as triggers and state is read on demand.
- `tab.get` and `pane.get` read one object each and answer with the present.
  `pane.list` filters by workspace only, not by tab.
- A malformed request is answered with an uncorrelated error frame and the
  connection is then closed.

## License

MIT — see [LICENSE](LICENSE).
