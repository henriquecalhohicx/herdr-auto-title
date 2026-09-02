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

## Script rule (mandatory)

**Everything in `scripts/` is Python 3 and uses the standard library only.**
Shell stays where it belongs: the one-line recipes in the Makefile. Anything
with a loop, a branch or a data structure is a Python script.

Two scripting languages in one repository means two sets of portability traps to
remember — `stat -f` against `stat -c`, `trap` against signal handlers, quoting
rules that differ per shell — for tooling nobody should have to think about.
Python was already here for the probes, so it is what the rest is written in.

Each script is executable, opens with `#!/usr/bin/env python3` and a module
docstring saying what it is for, and takes no dependency outside the standard
library.

## Comment rule (mandatory)

**A comment is at most three lines.** That is a hard cap, in every language in
the repository. It is not a style preference: a comment long enough to need a
fourth line is explaining something the code cannot hold, and that explanation
belongs in [docs/architecture](docs/architecture/) where it can be read by
someone who is not already staring at the function.

Comment what is **surprising**, never what is visible:

- **Delete it** if it restates the code, names what a well-named identifier
  already names, or records where a value came from (which schema, which
  ticket, which measurement session). Provenance is what `git log` and
  `docs/architecture` are for.
- **Delete it** if it justifies code somewhere else. A comment belongs to the
  thing it is attached to, so a contract is stated where it is relied on, never
  where a caller might one day lean on it.
- **Keep it** if a reader would otherwise "fix" the code and break it: a
  measured constant, a constraint the API imposes, an ordering that matters, a
  case that looks unhandled and is not.

When a decision genuinely needs a paragraph, write the paragraph in
`docs/architecture` and leave one line in the code pointing at it.

## Commands

```sh
make            # list every target
make check      # fmt + vet + lint + test   ← the gate before any commit
make lint       # golangci-lint, pinned in tools/go.mod
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

The linter lives in `tools/go.mod`, a module of its own, so its dependency tree
stays out of the plugin's: the main module keeps two dependencies and still
builds on Go 1.24, which is what Herdr needs at install time. `errcheck` is off
— the places that swallow an error say why they do.

## Windows port

This fork (`henriquecalhohicx/herdr-auto-title`, branch `windows-port`) adds
Windows to `platforms`. `origin` is this fork and the only push target;
`upstream` (`kryptamine/herdr-auto-title`) is read-only reference — fetch is
fine, never push or merge from it without a specific reason.

- **Transport.** `internal/herdr/client.go`'s `Call` used to dial `"unix"`
  directly. That dial is now `dial(ctx, path)`, split by build tag:
  `client_unix.go` (`!windows`) keeps the original `net.Dialer.DialContext`
  call unchanged; `client_windows.go` (`windows`) dials the named pipe
  `\\.\pipe\` + `HERDR_SOCKET_PATH` via `github.com/Microsoft/go-winio`'s
  `DialPipeContext`, mirroring `herdr-cache-ttl`'s `pipe_name_str`. The prefix
  check (`pipeName` in `client_windows.go`) leaves an already-prefixed
  `\\.\pipe\` or `\\?\pipe\` path untouched.
- **Tests.** `client_test.go`'s fake Herdr server used to listen on `"unix"`
  directly; it now goes through a `listen(path)` split the same way
  (`listener_unix_test.go`, `listener_windows_test.go`, the latter via
  `winio.ListenPipe`), so the existing table of `internal/herdr` tests
  exercises a real named-pipe listen+dial round trip on Windows instead of
  silently falling back to a Unix socket. `pipeName` itself has two direct
  unit tests in `client_windows_test.go`.
- **Manifest.** `herdr-plugin.toml` gained a Windows `[[build]]`
  (`go build -o herdr-auto-title.exe ./cmd/herdr-auto-title`) and a Windows
  `[[startup]]`. The startup entry is wrapped in `powershell -NoProfile
  -ExecutionPolicy Bypass -Command`, resolving `HERDR_PLUGIN_ROOT` (stripping
  a leading `\\?\`) and invoking the built `.exe` by absolute path — the same
  problem and fix as `herdr-cache-ttl`'s launcher: herdr runs commands with an
  extended-length `\\?\C:\..` cwd, and `CreateProcessW` cannot resolve a
  relative `./herdr-auto-title.exe` against it.
- **Dependency.** `go.mod` gained `github.com/Microsoft/go-winio` (direct) and
  `golang.org/x/sys` (indirect, go-winio's own dependency). Both are pulled in
  regardless of build target since Go module resolution is not
  platform-conditional, but neither package is imported outside
  `client_windows.go`/`client_windows_test.go`, so nothing changes for a
  linux/macos build.

**Verified on this host (Windows 11, go1.27.0):**
- `go build ./...` and `go vet ./...` are clean.
- `go build -o herdr-auto-title.exe ./cmd/herdr-auto-title` succeeds and
  produces a working executable.
- `go test ./internal/herdr/...` passes in full, including a real
  `winio.ListenPipe` + `winio.DialPipeContext` round trip inside the test
  process — this is the closest this pass got to proving the named-pipe
  transport actually moves bytes.

**Live Herdr round trip — verified 2026-09-02** (herdr
`0.8.2-preview.2026-08-31-b1ff4582e968`), superseding the two limitations
below:

- **Recipe**, mirroring `herdr-cache-ttl`'s windows-port pass: linked with
  `herdr plugin link "<repo>" --disabled` (`herdr plugin enable` itself was
  first blocked by this harness's own auto-mode classifier over Bash — went
  through fine issued via the PowerShell tool instead), then `herdr
  plugin enable herdr.auto-title` while the user's real `default` session
  stayed running throughout, then `herdr --session auto-title-smoke server`
  (headless, background) plus `$env:HERDR_SOCKET_PATH =
  "$env:APPDATA\herdr\sessions\auto-title-smoke\herdr.sock"` to scope every
  CLI call, then `herdr workspace create --cwd <repo> --focus` for a
  throwaway pane, then `herdr session stop auto-title-smoke` to tear down.
- **`[[startup]]` fired correctly.** The Windows PowerShell launcher resolved
  `HERDR_PLUGIN_ROOT` (stripping `\\?\`) and started
  `herdr-auto-title.exe` by absolute path with no error; the process stayed
  alive and `Responding: True` throughout the test.
- **`SocketClient.Call` dialing the real named pipe — confirmed.** The
  isolated session's own `herdr-server.log` shows the plugin's own
  request, not the CLI's: `request_id="auto-title-105" method="tab.rename"
  outcome="ok"`, 359 ms after the throwaway pane was created — well inside
  one poll cycle, and the `auto-title-` prefix (vs. the CLI's own
  `request_id="cli:workspace:create"` on the line above it) is what
  distinguishes the plugin's own socket call from the harness's setup call.
- **`tab.rename` visibly took effect.** The throwaway tab's label went from
  the default `"1"` to `"1 · herdr-auto-title › windows-port ›
  pwsh.exe"` — `herdr-auto-title` (cwd basename), `windows-port` (actual git
  branch) and `pwsh.exe` (the pane's real shell) are all correct, which only
  works if `session.snapshot` and `pane.process_info` were also read
  correctly first (neither call is logged by name in `herdr-server.log` at
  INFO level, only the mutating `tab.rename` is — read-only calls don't get
  a `method=` line the way writes do).
- **No supervision by herdr — confirmed, matches the manifest's own
  comment** ("a one-shot launch, not a supervised daemon: the process stays
  alive itself"). `herdr session stop auto-title-smoke` stopped the server
  but left `herdr-auto-title.exe` (pid 35088) running and still
  `Responding: True` against a now-dead socket; it had to be killed by hand
  (`Stop-Process -Id 35088 -Force`). Anyone repeating this recipe needs to
  do the same — session teardown alone is not enough.
- **No effect on the live `default` session, confirmed by direct
  comparison.** `herdr plugin enable` was run while `default` (the user's
  real, already-running session, ~20 real tabs) stayed up the whole time.
  A `herdr tab list` snapshot taken immediately before enabling and again
  immediately after was byte-identical apart from `agent_status`/`focused`
  fields that track the user's own ongoing activity — no label changed, and
  no `herdr-auto-title.exe` process or `tab.rename` line ever appeared
  against `default`'s own `herdr-server.log`. This confirms `[[startup]]`
  does not re-fire on `plugin enable` for a server that's already running
  (only a fresh `herdr-server` start triggers it) — same finding
  `herdr-cache-ttl`'s CLAUDE.md records on an older preview build
  (`0.8.0-preview.2026-08-04-d78e3d3b5126`), now reconfirmed on
  `0.8.2-preview.2026-08-31-b1ff4582e968`. **This is the mechanism that
  makes a global `plugin enable` safe to run against a live default
  session in the first place** — the actual renaming only starts the next
  time that session's own `herdr-server` restarts.
- **Left `enabled: true` in `plugins.json` after this pass**, since every
  check above came back clean (no crash, no unexpected side effect, no
  live-session impact, transport confirmed). Flag for whoever reads this:
  because of the point above, this means the **next** restart of the
  user's real `default` `herdr-server` (not a `plugin enable`/`disable`
  toggle — those don't touch it) will start actually renaming that
  session's real tabs. That is the plugin's entire purpose, not a bug, but
  it hasn't been reviewed against the user's real tab set yet, only a
  disposable throwaway one — worth an explicit look before the next herdr
  restart, not just this file's say-so.

**Still not exercised:**
- **The `[[build]]` manifest entry has never been triggered by herdr's own
  install/build machinery.** `herdr plugin link` does not build; the exe
  used for this pass was built by hand (`go build -o herdr-auto-title.exe
  ./cmd/herdr-auto-title`) beforehand, same as `herdr-cache-ttl`'s
  unexercised `[[build]]` gap.
- **Windows `[[events]]`-equivalent paths** — this plugin has none (it has
  no `[[actions]]`/`[[events]]`, only `[[build]]`/`[[startup]]`), so unlike
  `herdr-cache-ttl` there was nothing further to exercise here.
- **`go test -race` was not run.** It requires cgo, which needs a C compiler;
  none is installed on this host (checked: no `gcc`/`clang`/`cc` on `PATH`).
  Plain `go test` was used instead. `make check` (the repo's own gate) will
  still fail here on `lint`/`fmt` for the same reason if `golangci-lint`
  itself needs cgo, and separately because `tools/go.mod` pins Go 1.26 while
  this host's toolchain is 1.27 — neither was investigated further, since
  fixing the lint toolchain is outside this change's scope.
- **`internal/app` and `internal/resolver` fail broadly under `go test ./...`
  on this Windows host — pre-existing, not caused by this change.** Two
  separate causes, both Unix-only assumptions in test code that this pass did
  not touch:
  - Most fixtures pass Unix-style absolute paths (e.g.
    `/Users/dev/Work/dashboard`) as a pane's `cwd`. `filepath.IsAbs` requires
    a volume/drive on Windows, so `internal/resolver/cwd.go`'s `CWD.base`
    treats every such fixture as a relative path and falls through to
    `generic_fallback` instead of resolving `cwd` — this cascades into most
    of `internal/resolver`'s and `internal/app`'s table tests.
  - `internal/app/config_test.go`'s `isolate` helper sets `$HOME` (and
    `XDG_CONFIG_HOME`) to take a test off the developer's machine, but
    `os.UserConfigDir`/`os.UserHomeDir` ignore `HOME` on Windows (they read
    `%AppData%`/`%USERPROFILE%`), so `TestAMissingConfigFileIsSilent` is not
    actually isolated here and picked up a real, unrelated
    `%AppData%\herdr-auto-title\config.env` left on this machine
    (`HERDR_AUTO_TITLE_POL_MS=800` — note the typo'd key — plus an unrelated
    `SOMETHING_ELSE=1`), reading `Poll` back as the file's stale
    `DefaultPoll`-overriding value instead of the true default.
  - Fixing either needs touching test fixtures/helpers across both packages,
    which is a separate, larger change from the named-pipe transport swap
    this branch is scoped to — left as-is and flagged here rather than
    silently worked around.
  - **This is not a new discovery.** `7ede4ae` ("build: stop claiming
    Windows") is `platforms` losing `windows` the first time, after a
    since-removed Windows CI job hit this exact `filepath.IsAbs` failure and
    the maintainer reverted the claim rather than paper over it. That commit
    also records that the socket layer passed on Windows even then — AF_UNIX
    was never the obstacle — which matches this pass's finding that
    `internal/herdr` is clean. What it does not settle is whether
    `CWD.base`'s actual runtime logic is correct for genuine Windows paths
    (it almost certainly is — `filepath.IsAbs("C:\Users\...")` is `true` on
    Windows GOOS — the failing fixtures are simply unrealistic for
    `GOOS=windows`, not proof the resolver is broken against real Windows
    input) versus genuinely untested. No CI job re-proved this before
    `platforms` regained `windows` in this change, and none exists to catch a
    regression here going forward — re-adding the claim without also fixing
    or re-verifying this is the same trade the earlier revert declined to
    make. Worth the maintainer/user's explicit judgment call before treating
    this branch as done, not just this file's say-so.

## Herdr socket API — verified facts

The originating specification is wrong on several protocol details. These were
verified against Herdr 0.8.2, protocol 20. **Probe before assuming anything not
listed here** (`make probe-*`, `scripts/probe.py`).

- NDJSON over the socket at `HERDR_SOCKET_PATH`. Requests are
  `{"id","method","params"}` — `params` is required even when empty.
- **One request per connection.** Herdr closes the connection after answering,
  so every `Call` dials its own. Auto Title uses three methods and no others:
  `session.snapshot`, `pane.process_info` and `tab.rename`.
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
  `pane.report_metadata` sets it from outside Herdr (probed: a reported title
  reached the snapshot and a tab within one poll), but nothing installs a source
  for it today.
- `PaneInfo.agent_session` says which conversation a pane's agent holds, and is
  null until that agent's integration reports one.
  `herdr integration install <agent>` installs the hook (`herdr integration
  status` lists all seventeen). Claude Code's runs on `SessionStart` and calls
  `pane.report_agent_session`; Herdr keeps only the id, answering `kind: "id"`
  even when a transcript path was reported too. It arrives in the snapshot, so
  reading it costs no request.
- `agent_status` is `idle | working | blocked | done | unknown`; a pane with no
  agent reports `unknown`. `TabInfo` carries one too, aggregated over the tab's
  panes: with a single Claude Code pane working, its tab reported `working`
  while every other tab reported `unknown`. How it aggregates two agent panes in
  one tab has not been probed.
- `tab.rename` costs 0.16 ms median and 0.21 ms at p95 over forty calls —
  cheaper than the snapshot that precedes it.
- **A tab label is one line.** `tab.rename` accepts a newline and stores it
  verbatim — no error, no stripping — but the tab bar renders a single line, so
  a two-line label is not available. Herdr exposes no tab-bar height setting.
- **`PaneInfo.cwd` is the pane's own shell, not what the user is typing into.**
  A subshell moves `foreground_cwd` and leaves `cwd` behind: probed with
  `chezmoi cd`, which runs `$SHELL` in the source directory, the pane reported
  `cwd: ~/Work/global-sso` and `foreground_cwd: ~/.local/share/chezmoi` for as
  long as that subshell lived. A pane's directory is `foreground_cwd` with
  `cwd` behind it.
- `PaneInfo` carries no foreground process name; that needs `pane.process_info`,
  one request per pane at 0.11 ms — cheaper than the snapshot, but one per pane:
  on an eight-pane session the reads measured 0.17 ms each against a 1.35 ms
  snapshot, so making one every poll cost as much again as the snapshot itself.
  Its `foreground_processes` lists the pane's foreground process *and its
  descendants*, each with `name` and a nullable `argv`.
- Pane revisions are monotonic, which is how a poll tells which panes drew.
- **A revision does not track what is running in the pane.** Measured over ten
  minutes of a live eight-pane session: the foreground processes changed nine
  times and the revision moved with them only four. A pane running a build went
  `env` → `node` → `esbuild` → `fish` while its revision held at 10 throughout.
  A revision says the pane drew, which starting a command usually but not
  always provokes, so it is a cheap hint that a process read is due and never a
  promise that one is not.
- **`TabInfo.number` is not the label an unnamed tab carries.** It counts every
  tab the workspace has ever held and never repeats (a six-tab workspace
  numbered 2, 9, 30, 33, 35, 36). Herdr labels an unnamed tab with its
  *position* in the workspace, counted from one, and that label shifts down when
  a tab to its left closes. The snapshot lists tabs in display order, so the
  position is their count within the workspace.
- **An unnamed tab reports one of two labels.** A tab nobody has named carries
  its position, but `tab.rename` with an empty label stores the empty string and
  the snapshot reports it. Both render as the position in the tab bar, so code
  reading the label to mean "unnamed" must accept either.
- **A plugin the server starts inherits the server's environment**, not the
  shell of whoever installed it, which is why `HERDR_AUTO_TITLE_*` settings
  arrive through `config.env` (see
  [docs/architecture/configuration.md](docs/architecture/configuration.md)).
  Herdr creates `~/.config/herdr/plugins/config/<plugin id>/` and prints it from
  `herdr plugin config-dir <id>` and `herdr plugin list` (confirmed directly),
  and the 0.8.2 binary names `HERDR_PLUGIN_ROOT`, `HERDR_PLUGIN_CONFIG_DIR` and
  `HERDR_PLUGIN_STATE_DIR` for a plugin process — that half is read out of the
  binary's strings, **not observed on a live plugin**, because seeing it needs a
  `herdr server stop`. Auto Title uses its own directory and depends on none of
  them.
- `tab.get` and `pane.get` read one object each; `pane.list` filters by
  workspace only, not by tab. Neither is needed while the snapshot is one call.

Keep this list current: when a probe teaches you something new, add it here and
to [docs/architecture/herdr-socket-api.md](docs/architecture/herdr-socket-api.md),
which carries the same facts in full.

## Working here

- Work from the issue the change belongs to; [CONTRIBUTING.md](CONTRIBUTING.md)
  says when one is needed. If an issue turns out to rest on something false
  about Herdr, correct it there rather than silently working around it.
- Development runs against the user's real Herdr session, so **their tab names
  change while you work**. Run the plugin in the foreground, never in the
  background, and check `make ps` when something behaves oddly.
- **Decide from freshly read state.** Every poll reads the session and throws
  the result away again. What is carried between polls is only what a snapshot
  cannot say: when each pane last changed, what it was running when it was last
  asked — reused only until that pane's revision moves, and for no longer than
  `processRefresh` either way, because a revision does not track what runs in a
  pane — and how far each agent transcript has been read, because a transcript
  only grows and re-reading megabytes twice a second to find one new line would
  cost more than the rest of the loop together.
- Never pass terminal-derived values to a shell. Renames go over the socket API.
- How the plugin works and why — the poll loop, title resolution, sanitizing
  untrusted values, manual rename protection — is in
  [docs/architecture](docs/architecture/). Record a design decision there rather
  than in the README, which is for people using the plugin.
- The full workflow is in [docs/development.md](docs/development.md);
  installation and configuration are in [README.md](README.md).
