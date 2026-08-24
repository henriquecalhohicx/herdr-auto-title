# Development workflow

Auto Title is developed against the Herdr session you are already working in.
That is deliberate: the plugin's whole job is reacting to a real terminal, and a
synthetic session would not have agents running, directories changing or panes
churning. The cost is that **your own tab names change while you work on it** —
that is the plugin doing its job, not a problem to fix.

Everything below assumes `make`, Go 1.24+ and a shell running inside a Herdr
pane (which is what exports `HERDR_SOCKET_PATH`).

Run `make` on its own to list every target.

## The three loops

Work in the innermost loop that can answer your question. Most changes never
need the outer two.

### 1. Tests — seconds, no Herdr involved

```sh
make test        # go test -race ./...
make test-v      # the same, verbose
make check       # fmt + vet + test, run this before every commit
```

The suite drives the entire pipeline through `herdr.FakeClient`: bootstrap,
event routing, debounce collapsing, deduplication, rename failures, disconnects.
If a change can be expressed as "given this state, expect this title", it belongs
here and nowhere else.

**Write the test first when you are fixing something a live run revealed.** Both
defects found so far — a busy pane starving its own debounce, and a tab closing
between resolution and rename — are now regression tests, and both would have
been invisible to a smoke test that only checked the happy path.

### 2. Live run — a minute, in your real session

```sh
make run         # build, then run in the foreground with DEBUG logging
```

Rules that are worth following literally:

- **Run it in a tab of its own, in the foreground.** Ctrl+C then always stops it.
- **Never background it.** A stray instance keeps renaming tabs long after you
  have moved on, and you will blame the wrong code for it.
- **`make ps` before you start, `make stop` when confused.** Both targets cover
  the watcher as well as the plugin. They exist because backgrounded instances
  really do survive — twice during this project, once because a `pkill` pattern
  did not match, and once because `make check` rewrote a file, which a forgotten
  watcher took as a cue to rebuild and restart.

Watch what it does from a second tab:

```sh
make watch-tabs  # tab ids and labels, refreshed every second
```

While iterating on the plugin itself, replace `make run` with:

```sh
make dev         # rebuild and restart on every source change
```

Same rules apply: foreground, one tab, Ctrl+C to stop both watcher and plugin.
Note that `make check` rewrites files with `gofmt -w`, so a watcher left running
in another tab will rebuild and restart on it.

### 3. Protocol probe — when you are about to assume something

```sh
make probe-subs      # subscription types Herdr actually accepts
make probe-events    # the live event stream, one readable line per event
make probe-snapshot  # the snapshot Auto Title bootstraps from
```

`scripts/probe.py` talks to the socket directly, so it shows you the wire truth
rather than what this repo believes. Reach for it **before** writing code that
subscribes to a new event, reads a new field, or calls a new method.

This is not optional caution. The specification this project was planned from got
three protocol details wrong, each of which would have cost an afternoon of
debugging:

- the socket serves **one request per connection**, and any request sent on a
  subscription connection ends the stream;
- subscription types are dot-separated (`pane.updated`) while the events they
  deliver arrive snake_case (`pane_updated`);
- `pane.output_changed` is a real event kind but is **not** an accepted
  subscription type, and `pane.agent_status_changed`, `pane.scroll_changed` and
  `pane.output_matched` can only be subscribed per pane.

The current list of verified facts lives in the README under *Notes on the Herdr
socket API*. When a probe teaches you something new, add it there.

## Working through a ticket

Tickets live in `docs/issues/` and are ordered by dependency; take any whose
blockers are done.

1. Read the ticket. Probe anything it asserts about the API that is not already
   in the README's verified list.
2. Write the failing test for the behaviour the ticket describes.
3. Implement until `make check` is green.
4. Do one live run and actually look at the logs — the fast loop cannot see
   event timing, event volume, or what Herdr does under load.
5. Tick the ticket's acceptance criteria. If a criterion turned out to be based
   on something false, fix the ticket text rather than quietly skipping it.

## Reading the logs

`make run` sets `HERDR_AUTO_TITLE_DEBUG=1`. What each line tells you:

| Line | Meaning |
|------|---------|
| `subscribed to herdr events` | the stream is live; if this never appears, the subscription was rejected |
| `session snapshot loaded` | bootstrap worked; `tabs=` and `panes=` show what was seeded |
| `event received` | the router saw an event and armed a timer |
| `title unchanged` | resolution ran and deduplication suppressed the rename — the common case |
| `tab renamed` | the only line that means Herdr was actually asked to do something |
| `rename failed` | something went wrong that is worth your attention |

A quiet log with `event received` lines and no renames is the plugin working
correctly, not the plugin doing nothing.

## Things that will bite you

**Your tabs get renamed and stay renamed.** There is no undo — the plugin does
not remember previous names. Manual rename protection is a later ticket; until it
lands, a name you set by hand is overwritten on the next context change.

**A busy pane is titled on the one-second cap, not the 200 ms window.** A pane
running an agent emits updates every ~100 ms, which would rearm the debounce
timer forever. The cap is what makes such a tab get a name at all.

**Short-lived tabs produce `tab_not_found`.** Herdr creates and closes tabs for
its own purposes faster than events arrive. That is handled and logged at DEBUG;
if you see it as a warning, something regressed.

**`go test -race` is the gate, not `go test`.** The pipeline is concurrent by
design: socket reader, router, per-tab timers, reconciliation workers. Races here
surface as tabs that are occasionally named wrong, which is nearly impossible to
debug after the fact.
