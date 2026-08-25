---
type: doc
title: 'The Poll Loop'
description: 'Why Auto Title polls the Herdr session instead of subscribing to it, what one poll does, what little state survives between polls, and how the loop behaves when Herdr is unreachable.'
tags: [architecture]
created: 2026-08-25
generated: { by: claude-code/opus-5, at: 2026-08-25T12:46:22+03:00 }
---

# The Poll Loop

Auto Title is one long-lived process that reads the whole Herdr session twice a
second and renames the tabs whose titles no longer fit. There is no scrollback
scanning, no LLM and no external service.

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

The loop lives in `App.Run` and `App.poll` (`internal/app/app.go`).

## Polling, not events

Herdr has an event stream and Auto Title ignores it, because subscribing replays
about ten seconds of history per active pane before delivering anything live and
offers no cursor to skip it. The measurements are in
[the socket API](./herdr-socket-api.md#why-the-event-stream-is-not-used).

A snapshot describes the present, costs one request whatever the session holds,
and carries every field the resolver reads. At 0.47 ms and 6 KB for six panes,
two polls a second come to about a thousandth of a core.

## Decide from freshly read state

**Every poll reads the session and throws the result away again.** A tab's
name is derived from the snapshot in hand, never from a cache of what was true
earlier, which is what makes the resolver's determinism worth anything: identical
session state always yields an identical title.

Two things are carried between polls, and both exist because a snapshot cannot
express them:

- **When each pane last changed** (`internal/state/changes.go`). A snapshot says
  what a pane holds but not when that became true, and a tab with several panes
  and none focused is named after whichever moved last. Herdr's pane revisions
  are monotonic, so comparing one poll's revisions with the last says which panes
  moved. The map is rebuilt from each snapshot, so panes that closed disappear
  from it for free.
- **What Auto Title last named each tab** (`internal/state/manual.go`), which is
  how a rename by the user is told from the plugin's own work. That is a design
  of its own: [manual rename protection](./manual-rename-protection.md).

One consequence worth stating: **the interval is the rename rate.** A tab
changes name at most once per poll however fast its pane is churning, so
`HERDR_AUTO_TITLE_POLL_MS` is both the freshness knob and the calm knob.

## One poll

1. `session.snapshot` — the whole session in one request.
2. `Changes.Observe` — note which panes' revisions advanced.
3. `tabsIn` — assemble tabs with their panes, asking `pane.process_info` per
   pane. That is a request per pane, made rather than guessed at because it
   measured cheaper than the snapshot that preceded it; a pane whose processes
   cannot be read simply has none.
4. `Manual.Retain` — drop bookkeeping for tabs the session no longer holds.
5. Per tab: skip it if locked, otherwise resolve a title, check whether the
   label moved under us, and rename when the result differs from the label the
   tab already carries.

**Deduplication is what keeps the loop quiet.** The snapshot reports each tab's
current label, and a rename is skipped when the resolved title already equals
it — which is also what stops a rename from provoking the next one. A session
where every tab already carries the right name issues no requests beyond the
snapshot and the per-pane reads.

The whole poll is bounded by `pollTimeout` (5 s). A tab that closed between the
snapshot and its rename answers `tab_not_found`, which is expected rather than an
error.

## When Herdr is not there

A connection lives for exactly one request, so there is nothing to reconnect and
no connection state to reconcile. An outage is simply a run of failed dials, and
recovery is the first dial that succeeds.

- **No poll failing is fatal, the first one included.** Herdr launches a plugin
  through a one-shot startup hook rather than supervising it, so a plugin that
  gave up would stay dead for the rest of the session — and Herdr's socket can
  be a moment behind the process it just launched. Nothing carried between polls
  is spoiled by a failure, so the next tick simply tries again.
- **Polls keep their usual rate through an outage.** A failed dial to an absent
  socket costs microseconds, and the rate is what makes recovery immediate.
- **The logging backs off instead.** At two polls a second, an hour of Herdr
  being down is seven thousand identical warnings. `failureLog`
  (`internal/app/app.go`) reports the first failure and then only as the run of
  them doubles, which turns that hour into thirteen lines, and a line on the way
  out says how many polls were missed.

The only failure that stops the process is a missing `HERDR_SOCKET_PATH`, caught
in `herdr.New` before the loop is ever entered.

Measured on a live session: with the socket removed for eight seconds the
process stayed up and polls resumed the moment it returned; started with no
socket at all, it stayed up for ten seconds on five warnings and named every tab
as soon as the socket appeared.

## Shutdown

`signal.NotifyContext` in `cmd/herdr-auto-title/main.go` cancels the context on
`SIGINT` and `SIGTERM`; `Run` returns and the process exits. There are no
debounce timers to cancel and no socket to close, because a connection never
outlives the call that made it.

The one thing that can still be in flight is a background git lookup, which is
bounded by its own timeout — see
[title resolution](./title-resolution.md#git-runs-outside-the-loop).
