# CLAUDE.md

Herdr Auto Title — a Herdr plugin, written in Go, that generates tab titles from
each tab's current context. Long-running process, event-driven, no LLM and no
external service.

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
Scope is a package or area (`resolver`, `debounce`, `herdr`, `app`).

- Subject in the imperative mood, lowercase, no trailing period, ≤72 characters
  ("add manual rename protection", not "Added manual rename protection.").
- The body explains why, not what — the diff already says what.
- One logical change per commit.
- Never add a co-author trailer.

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
make probe-subs # subscription types Herdr actually accepts
make probe-events   # the live event stream
make probe-snapshot # the bootstrap snapshot
```

`go test -race` is the gate, not `go test`: the pipeline is concurrent by design
(socket reader, router, per-tab timers, reconciliation workers).

## Herdr socket API — verified facts

The originating specification is wrong on several protocol details. These were
verified against Herdr 0.8.2, protocol 20. **Probe before assuming anything not
listed here** (`make probe-*`, `scripts/probe.py`).

- NDJSON over the socket at `HERDR_SOCKET_PATH`. Requests are
  `{"id","method","params"}` — `params` is required even when empty.
- **One request per connection.** Herdr closes the connection after answering,
  and any request sent on a subscription connection ends the stream. Each `Call`
  therefore dials its own connection; the event stream is read-only for its life.
- Subscription types are dot-separated (`pane.updated`); the events they deliver
  arrive snake_case (`pane_updated`), wrapped as `{"event": ..., "data": ...}`.
- `pane.output_changed` is a real event kind but is **not** an accepted
  subscription type. `pane.agent_status_changed`, `pane.scroll_changed` and
  `pane.output_matched` are per-pane subscriptions and require a `pane_id`.
- Per-pane subscriptions are **not needed for agent context**: `pane_updated`
  resends the whole `PaneInfo`, agent fields included, whenever an agent's
  status or title changes. `pane.agent_detected` is global and carries only
  `pane_id`, `workspace_id`, `agent`, `final_status`, `released` — no tab.
- `PaneInfo.title` is the agent's own title. It was null for every Claude Code
  pane observed; that agent reports its topic through `terminal_title_stripped`.
- `agent_status` is `idle | working | blocked | done | unknown`; a pane with no
  agent reports `unknown`.
- Subscribing **replays a backlog** of pane updates before the live ones:
  measured at roughly the last 95 revisions of a pane, paced at ~10 a second.
  They arrive further apart than the debounce window, so without filtering the
  burst cap turns each second of replay into a rename. Pane revisions are
  monotonic, so the cache drops updates older than the one it holds.
- `PaneInfo` carries no foreground process name; that needs `pane.process_info`.
- `pane_closed` names only the pane, not its tab — the cache indexes panes by ID.

Keep this list current: when a probe teaches you something new, add it here and
to the README's *Notes on the Herdr socket API*.

## Working here

- Tickets live in `docs/issues/`, ordered by dependency; take any whose blockers
  are done. If a ticket turns out to rest on something false, fix the ticket text
  rather than silently working around it.
- Development runs against the user's real Herdr session, so **their tab names
  change while you work**. Run the plugin in the foreground, never in the
  background, and check `make ps` when something behaves oddly.
- Never pass terminal-derived values to a shell. Renames go over the socket API.
- The full workflow is in [docs/development.md](docs/development.md); the
  architecture and configuration are in [README.md](README.md).
