---
type: doc
title: 'Herdr Socket API'
description: 'The wire protocol Auto Title speaks, the methods it uses, and the measured facts about Herdr 0.8.2 that the rest of the architecture rests on — including the ones the originating specification got wrong.'
tags: [architecture, reference]
created: 2026-08-25
generated: { by: claude-code/opus-5, at: 2026-08-25T12:46:22+03:00 }
---

# Herdr Socket API

Everything Auto Title knows about a session arrives over one Unix socket. The
originating specification is wrong on several protocol details, so every fact
below was verified against a live **Herdr 0.8.2, protocol 20** install with
`scripts/probe.py` or a direct socket request. **Probe before assuming anything
not listed here**, and add what a probe teaches you both here and to the list in
`CLAUDE.md`.

## Transport

Newline-delimited JSON over the socket named by `HERDR_SOCKET_PATH`
(`internal/herdr/client.go`). A request is `{"id","method","params"}` — `params`
is required even when empty, which is why `emptyParams` exists in
`internal/herdr/protocol.go` — and a reply is `{"id","result"}` or
`{"id","error"}`.

**One request per connection.** Herdr closes the connection after answering, so
`SocketClient.Call` dials its own each time. That is not a workaround: it is why
there is no connection to lose and no reconnect logic anywhere in the plugin.
See [the poll loop](./poll-loop.md).

A malformed request is answered with an uncorrelated error frame, and the
connection is then closed.

## The methods Auto Title uses

Three, and no others (`internal/herdr/session.go`):

- **`session.snapshot`** returns the whole session — every tab with its label,
  every pane with its directory, terminal title, agent and agent status.
  Measured at 0.47 ms and 6 KB for six panes.
- **`pane.process_info`** returns what is running in one pane. Measured at
  0.11 ms — less than the snapshot itself, because it reads the process table
  rather than serializing the session. Its `foreground_processes` holds the
  pane's foreground process *and that process's descendants*, each with `name`
  and a nullable `argv`.
- **`tab.rename`** takes `{tab_id, label}`. Measured at 0.16 ms median and
  0.21 ms at p95 over forty calls, against 0.99 ms for the `session.snapshot`
  preceding them. Renaming is not what limits anything.

`tab.get` and `pane.get` read one object each, and `pane.list` filters by
workspace only, never by tab. None of them is needed while the snapshot is one
call.

## Why the event stream is not used

Herdr does expose an event stream, and Auto Title deliberately ignores it.

**On subscribe, Herdr replays a backlog before delivering anything live**:
roughly the last 95 revisions of every pane, paced at about ten a second, so
around ten seconds of history for each active pane, closed panes included. Live
events queue behind that — a change made two seconds after subscribing was
observed arriving thirteen seconds later.

There is no way to skip it. `events.subscribe` takes only a subscription list,
event envelopes carry no timestamp or sequence number, and no method exposes a
stream position. A subscriber therefore spends its first seconds reacting to a
session that no longer exists, while a snapshot always describes the present.

**Do not reintroduce a subscription** without measuring again and recording the
result here.

Two further traps, if anyone does: subscription types use dot notation
(`pane.updated`) while the events they deliver arrive with snake_case kinds
(`pane_updated`), wrapped as `{"event": ..., "data": ...}`; and
`pane.output_changed` is a real event kind but is **not** an accepted
subscription type. `pane.agent_status_changed`, `pane.scroll_changed` and
`pane.output_matched` are per-pane and rejected without a `pane_id`, while
`pane.agent_detected` is global. `pane_closed` and `pane_agent_detected` carry
only pane identifiers — neither names the tab.

## What the objects carry

The wire types in `internal/herdr/session.go` mirror only the fields the code
reads, so this section describes Herdr rather than those types.

- **Pane revisions are monotonic per pane.** That is how one poll tells which
  panes moved since the last, and it is the whole basis of
  `internal/state/changes.go`.
- **`PaneInfo` carries no foreground process name.** Only `pane.process_info`
  answers that, and nothing announces that a command started.
- **`PaneInfo.title` is the agent's own title**, not the terminal's. Herdr left
  it null for every Claude Code pane observed; that agent reports its topic
  through `terminal_title_stripped` instead. This is why most agent context
  reaches a title one rung below the agent source — see
  [title resolution](./title-resolution.md).
- **`agent_status` is `idle | working | blocked | done | unknown`.** Every pane
  carries one, and a pane with no agent reports `unknown`. `TabInfo` carries one
  as well, aggregated over the tab's panes: with a single Claude Code pane
  working, its tab reported `working` while every other tab reported `unknown`.
  How it aggregates two agent panes in one tab has not been probed.
- **`TabInfo.number` is not the label an unnamed tab carries.** `number` counts
  every tab its workspace has ever held and never repeats — a workspace holding
  six tabs was seen numbering them 2, 9, 30, 33, 35, 36. The label Herdr puts on
  a tab nobody has named is its *position* in the workspace, counted from one,
  and it slides down whenever a tab to the left of it closes: three fresh tabs
  labelled `5`, `6`, `7` became `5`, `6` when the middle one was closed. Tabs
  arrive from `session.snapshot` in the order they are shown, so the position is
  the count of the workspace's tabs up to and including that one.

  Reading `number` as the default label locked every tab created after startup;
  the story is in [manual rename protection](./manual-rename-protection.md).
