# CLAUDE.md

Herdr Auto Title — a Herdr plugin, written in Go, that generates tab titles from
each tab's current context. Long-running process that polls the Herdr session,
no LLM and no external service.

## Language rule (mandatory)

**Everything written into this repository is in English.** Code comments, commit
messages, log and error messages, documentation, test names, ticket text — all
English, with no exceptions. This holds regardless of the language the request
was made in; only the conversation with the user follows the user's language.

## Commit convention (mandatory)

Commits follow [Conventional Commits](https://www.conventionalcommits.org):

```
<type>(<optional scope>): <subject>

<optional body explaining why, wrapped at 72 columns>
```

Types in use: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `chore`.
Scope is a package or area (`resolver`, `state`, `herdr`, `app`).

- Subject in the imperative mood, lowercase, no trailing period, ≤72 characters
  ("add manual rename protection", not "Added manual rename protection.").
- The body explains why, not what — the diff already says what.
- One logical change per commit.
- Never add a co-author trailer.

## Type rule (mandatory)

**A struct field exists only if code reads it.** Herdr's wire objects carry far
more than Auto Title needs; mirroring them in full makes a type claim a
dependency the code does not have, and every unread field is a promise to keep
something working that nothing exercises. Add a field when the code that reads
it lands in the same change, and delete a field the moment its last reader goes.
The same holds for methods, constants and event payload types.

## Commands

```sh
make            # list every target
make check      # fmt + vet + go test -race   ← the gate before any commit
make test       # go test -race ./...
make run        # build and run in the current Herdr session, DEBUG logging
make dev        # the same, restarting on every source change
make ps         # show running plugin/watcher instances
make stop       # stop them
make tabs       # current tab names
make watch-tabs # ...refreshed every second
make probe-snapshot # the session snapshot the plugin polls
```

`go test -race` is the gate, not `go test`: the poll loop and the change history
it keeps are exercised concurrently in tests, and a future reset action will
touch that history from outside the loop.

## Herdr socket API — verified facts

The originating specification is wrong on several protocol details. These were
verified against Herdr 0.8.2, protocol 20. **Probe before assuming anything not
listed here** (`make probe-*`, `scripts/probe.py`).

- NDJSON over the socket at `HERDR_SOCKET_PATH`. Requests are
  `{"id","method","params"}` — `params` is required even when empty.
- **One request per connection.** Herdr closes the connection after answering,
  so every `Call` dials its own. Auto Title uses two methods and no others:
  `session.snapshot` and `tab.rename`.
- **The event stream is not used, on purpose.** `events.subscribe` replays a
  backlog before anything live — about the last 95 revisions of *every* pane at
  ten a second, so ~10 s of history per active pane, closed panes included — and
  live events queue behind it (measured: a change made 2 s after subscribing
  arrived 13 s later). There is no cursor: `events.subscribe` takes only a
  subscription list, envelopes carry no timestamp or ordinal, and no method
  exposes a stream position. A snapshot costs 0.47 ms and 6 KB for six panes and
  describes the present. **Do not reintroduce a subscription** without measuring
  again and recording the result here.
- Subscription types would be dot-separated (`pane.updated`) while the events
  they deliver arrive snake_case (`pane_updated`), wrapped as
  `{"event": ..., "data": ...}`. `pane.output_changed` is a real event kind but
  is not an accepted subscription type; `pane.agent_status_changed`,
  `pane.scroll_changed` and `pane.output_matched` are per-pane and need a
  `pane_id`.
- `PaneInfo.title` is the agent's own title. It was null for every Claude Code
  pane observed; that agent reports its topic through `terminal_title_stripped`.
- `agent_status` is `idle | working | blocked | done | unknown`; a pane with no
  agent reports `unknown`.
- `PaneInfo` carries no foreground process name; that needs `pane.process_info`.
- Pane revisions are monotonic, which is how a poll tells which panes moved.
- `tab.get` and `pane.get` read one object each; `pane.list` filters by
  workspace only, not by tab. Neither is needed while the snapshot is one call.

Keep this list current: when a probe teaches you something new, add it here and
to the README's *Notes on the Herdr socket API*.

## Working here

- Tickets live in `docs/issues/`, ordered by dependency; take any whose blockers
  are done. If a ticket turns out to rest on something false, fix the ticket text
  rather than silently working around it.
- Development runs against the user's real Herdr session, so **their tab names
  change while you work**. Run the plugin in the foreground, never in the
  background, and check `make ps` when something behaves oddly.
- **Decide from freshly read state.** Every poll reads the session and throws
  the result away again. The only thing carried between polls is when each pane
  last changed, which a snapshot cannot say.
- Never pass terminal-derived values to a shell. Renames go over the socket API.
- The full workflow is in [docs/development.md](docs/development.md); the
  architecture and configuration are in [README.md](README.md).
