# OpenSpec: Herdr Auto Title

## 1. Overview

Implement **Herdr Auto Title**, a Herdr plugin that automatically generates concise, useful tab titles from the current context of each tab.

The plugin must be implemented in **Go** as a standalone long-running process.

The plugin must:

- run as a persistent Herdr plugin process;
- read the Herdr session through the Herdr socket API;
- read the whole session on each poll and keep almost nothing between them;
- rename only when the title it derives differs from the tab's current label;
- determine useful tab titles using a deterministic resolver;
- rename tabs through the Herdr socket API or Herdr CLI;
- require no Node.js, Bun, Python, or other runtime;
- work without any external AI/LLM service.

The first version prioritizes **low latency, determinism, reliability, and zero external runtime dependencies**.

---

## 2. Goals

### Primary goals

1. Automatically name Herdr tabs without manual naming.
2. React to context changes with very low latency.
3. Avoid unnecessary rename operations.
4. Avoid polling.
5. Avoid LLM calls.
6. Preserve manually assigned tab names.
7. Support multiple tabs independently.
8. Maintain runtime state in memory.
9. Recover from Herdr socket disconnects.
10. Ship as a standalone cross-platform Go binary.

### Non-goals

V1 must NOT:

- call OpenAI, Anthropic, Gemini, or any other LLM;
- inspect arbitrary terminal scrollback continuously;
- scan Claude Code JSONL transcripts;
- perform semantic inference with an AI model;
- require a database;
- require Redis or another external service;
- modify the Herdr core application.

---

## 3. Naming

Product/plugin name:

**Herdr Auto Title**

Recommended repository name:

```text
herdr-auto-title
```

Plugin ID:

```text
herdr.auto-title
```

The implementation and documentation should use "Auto Title" rather than "Auto Rename". The plugin's purpose is generating the current tab title; renaming is an implementation detail.

---

## 4. High-level architecture

```text
                         ┌─────────────────────┐
                         │        Herdr        │
                         └──────────┬──────────┘
                                    │
                              startup hook
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │ Auto Title (Go)     │
                         │                     │
                         │ long-running proc   │
                         └──────────┬──────────┘
                                    │
                            local Herdr socket
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │ session.snapshot    │
                         │   every 500 ms      │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │  Dominant pane per  │
                         │        tab          │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │    Deduplication    │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │   Title Resolver    │
                         └──────────┬──────────┘
                                    │
                              RenameDecision
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │     Tab Renamer     │
                         └─────────────────────┘
```

The plugin must use the Herdr raw Socket API. It uses exactly two methods: `session.snapshot` to read the session and `tab.rename` to act on it.

**Auto Title deliberately does not subscribe to events.** This reverses the original design, and the reason is measured rather than stylistic. `events.subscribe` replays a backlog before delivering anything live — roughly the last 95 revisions of every pane, paced at about ten a second, so about ten seconds of history for each active pane — and live events queue behind it. A change made two seconds after subscribing was observed arriving thirteen seconds later. The protocol offers no way out: `events.subscribe` takes only a subscription list, event envelopes carry no timestamp or sequence number, and no method exposes a cursor.

A snapshot has no such lag. It describes the present, and one request returns the whole session — 0.47 ms and 6 KB for a six-pane session — so polling twice a second costs about a thousandth of a core. Everything the resolver reads is in the snapshot, so the event stream would add latency and a large amount of machinery without adding information.

---

## 5. Technology

Use:

- Go 1.24+;
- Go standard library wherever practical;
- no framework;
- no third-party runtime;
- no Node/Bun/Python dependency.

Preferred standard-library packages:

```text
encoding/json
errors
fmt
log/slog
net
os
os/exec
os/signal
path/filepath
regexp
strings
sync
syscall
time
context
```

A small third-party dependency is acceptable only if it solves a platform-specific problem that cannot reasonably be implemented with the standard library.

The final release artifact must be a standalone executable.

---

## 6. Repository structure

Recommended structure:

```text
herdr-auto-title/
├── README.md
├── LICENSE
├── herdr-plugin.toml
├── go.mod
├── go.sum
├── cmd/
│   └── herdr-auto-title/
│       └── main.go
├── internal/
│   ├── herdr/
│   │   ├── client.go
│   │   ├── protocol.go
│   │   └── session.go
│   ├── state/
│   │   ├── changes.go
│   │   └── tab.go
│   ├── resolver/
│   │   ├── resolver.go
│   │   ├── agent.go
│   │   ├── agentname.go
│   │   ├── terminal.go
│   │   ├── processes.go
│   │   ├── ssh.go
│   │   ├── git.go
│   │   └── cwd.go
│   └── app/
│       ├── app.go
│       └── config.go
└── scripts/
```

The exact package layout may be adjusted, but responsibilities must remain separated.

---

## 7. Herdr plugin manifest

Create a valid `herdr-plugin.toml`.

Use a startup hook to launch the long-running process.

Conceptually:

```toml
id = "herdr.auto-title"
name = "Auto Title"
version = "0.1.0"
min_herdr_version = "0.7.0"
description = "Automatically generates contextual tab titles"
platforms = ["linux", "macos", "windows"]

[[startup]]
command = ["herdr-auto-title"]
```

Important:

- Herdr startup hooks are one-shot launch commands, not supervised daemons.
- The plugin process itself must remain alive after startup.
- Do not rely on per-event plugin process spawning for the main implementation.
- The startup command must be an executable available in the plugin's installation directory/path as appropriate for Herdr plugin packaging.

Before finalizing the manifest, verify the exact current Herdr manifest requirements and supported platform behavior.

---

## 8. Long-running process lifecycle

The plugin starts once through the Herdr startup hook.

Lifecycle:

```text
Herdr starts/restores
        ↓
startup hook
        ↓
herdr-auto-title process
        ↓
connect to socket
        ↓
first poll names what already exists
        ↓
poll loop, every 500 ms
        ↓
a failed poll is logged and retried
        ↓
graceful shutdown
```

The process must not exit after successful initialization.

---

## 9. Herdr socket client

Use `HERDR_SOCKET_PATH` to locate the Herdr socket/transport.

Do not hard-code a socket path.

The transport implementation must account for Herdr's platform-specific local transport. Do not assume that every platform uses Unix domain sockets.

Herdr serves **one request per connection** and closes it after answering, so each call dials a connection of its own. There is no long-lived connection to manage, and therefore no client lifecycle beyond the call:

```go
type HerdrClient interface {
    Call(ctx context.Context, method string, params any, result any) error
}
```

Do not reimplement Herdr's protocol from assumptions. Inspect the current socket schema using the installed Herdr API/schema facilities when implementing the client.

---

## 10. Reading the session

`session.snapshot` returns the whole session: every tab with its label, and every pane with its directory, terminal title, agent and agent status. It is the only read Auto Title performs.

The first poll happens before the loop starts, so tabs that already exist are named without waiting for a tick. If that first poll fails there is nothing to run for, and the process exits with the reason; every later failure is logged and retried on the next tick.

---

## 11. Why not events

Herdr exposes an event stream, and Auto Title does not use it. This reverses the original design. The reason is measured, and it is recorded here so the decision is not quietly re-litigated.

**Subscribing replays history first.** Measured against Herdr 0.8.2: a new subscriber receives roughly the last 95 revisions of every pane before any live event, paced at about ten a second. A six-pane session took 25 seconds to drain, and the drain grows by about ten seconds for every additional active pane. The replay includes panes that have since closed.

**Live events queue behind the replay.** A `tab.rename` issued two seconds after subscribing produced a `tab_renamed` event thirteen seconds later. Until the backlog drains, the stream describes a session that no longer exists.

**The protocol offers no way out.** `events.subscribe` takes only a subscription list — no cursor, no `since`, no sequence number. Event envelopes carry `event` and `data` and nothing else: no timestamp, no ordinal. No other method exposes a stream position.

**Events carry nothing a snapshot lacks.** Every field the resolver reads is in `PaneInfo`, which the snapshot returns in full.

So the stream would cost a subscription lifecycle, a backlog filter, and a second path into the state — in exchange for latency. Polling is both simpler and faster here.

---

## 12. The poll cycle

One tick does this and nothing else:

```text
session.snapshot
↓
record which panes changed  (revision advanced since the last poll)
↓
for each tab: select the context pane
↓
resolve a title
↓
rename if it differs from the label the snapshot reported
```

The snapshot's label is the deduplication baseline, so a tab already carrying the right name costs nothing but the comparison.

A rename that fails is logged and left for the next tick. A tab that closed between the snapshot and its rename answers `tab_not_found`, which is expected rather than an error.

---

## 13. State model

Almost nothing is kept between polls. A snapshot describes the whole session as it is right now, so each poll builds the tabs it needs and discards them:

```go
type TabState struct {
    ID          string
    CurrentName string
    Panes       map[string]*PaneState
}

type PaneState struct {
    ID string

    CWD           string
    ForegroundCWD string

    TerminalTitle    string
    TerminalTitleRaw string

    Agent        string
    DisplayAgent string
    AgentTitle   string
    AgentStatus  string

    Focused   bool
    ChangedAt time.Time
}
```

The one thing carried forward is `ChangedAt`. A snapshot says what a pane holds but not when that became true, and a tab with several panes and no focus is named after whichever pane changed most recently. Herdr's pane revisions are monotonic, so comparing one poll's revisions against the last says which panes moved. Panes the session no longer holds are forgotten.

That history must be concurrency-safe.

---

## 14. Dominant pane

A tab can contain multiple panes.

The resolver must define which pane provides the tab's primary context.

V1 should prefer:

1. focused pane;
2. active pane with an agent;
3. otherwise the most recently updated pane;
4. otherwise the first valid pane.

This logic must be isolated:

```go
func SelectContextPane(tab TabState) *PaneState
```

The resolver must not accidentally combine unrelated panes into a misleading title.

Future versions may support multi-pane-aware naming.

---

## 15. Title resolver

All naming decisions must go through:

```go
type TitleResolver interface {
    Resolve(ctx context.Context, tab TabState) RenameDecision
}
```

Result:

```go
type RenameDecision struct {
    Name       string
    Confidence int
    Reason     string
}
```

The resolver must be deterministic.

Identical input state must produce identical output.

---

## 16. Resolution priority

Use this priority order:

```text
1. Manual rename protection
2. Meaningful agent title
3. Meaningful terminal title
4. Known foreground process / command
5. SSH session
6. Git context
7. Current working directory
8. Generic fallback
```

A lower-priority source must never override a higher-priority meaningful source.

---

## 17. Manual rename protection

Manual names are a core UX requirement.

Example:

```text
automatic:
dashboard › Tests

user:
My Important Work
```

After the user manually renames the tab, the plugin must stop automatically changing that tab.

State:

```go
ManualName = true
```

However, the plugin's own rename must not be interpreted as manual.

Use expected-rename tracking:

```go
type ExpectedRename struct {
    TabID string
    Name  string
    ExpiresAt time.Time
}
```

Flow:

```text
plugin calls tab.rename
        ↓
expected rename recorded
        ↓
Herdr emits tab.renamed
        ↓
matches expected rename
        ↓
do not mark manual
```

User rename:

```text
Herdr emits tab.renamed
        ↓
does not match expected rename
        ↓
ManualName = true
```

Use a short expiration for expected renames to avoid stale state.

---

## 18. Resetting manual protection

V1 should support a way to re-enable automatic naming.

Preferred CLI:

```text
herdr-auto-title reset <tab-id>
```

The command should clear `ManualName` and trigger immediate reconciliation.

If implementing a separate CLI entrypoint is inconvenient in the first implementation, provide an equivalent documented mechanism, but the architecture should leave room for the command.

---

## 19. Agent context

Use agent information already exposed by Herdr.

Potential inputs:

```text
agent
agent_session
agent status
terminal title
```

For Claude Code, do NOT read Claude Code transcript JSONL files in V1.

Do NOT invoke an LLM.

A meaningful agent-provided title such as:

```text
Implement OAuth scopes
Fix OAuth redirect
Investigate login regression
```

should be preferred.

Generic values such as:

```text
Claude
Claude Code
Agent
Coding Agent
```

are not meaningful titles.

---

## 20. Terminal title

Use Herdr's terminal title fields when available.

Normalize and reject generic titles.

Generic titles include:

```text
bash
zsh
sh
fish
shell
terminal
Claude
Claude Code
node
```

A terminal title should only win when it contains meaningful context.

Examples:

```text
Fix OAuth redirect
auth.ts
prod-01
```

are meaningful.

---

## 21. Foreground process and command mapping

Implement a deterministic mapping for common development commands.

Examples:

```text
npm test              → Tests
pnpm test             → Tests
yarn test             → Tests

npm run dev           → Dev
pnpm dev              → Dev

npm run build         → Build
pnpm build            → Build

lazygit               → Git
git status            → Git
git commit            → Commit
git rebase             → Rebase

docker compose up     → Docker
docker compose logs   → Docker Logs

kubectl logs          → K8s Logs
kubectl get           → Kubernetes

terraform plan        → Terraform
terraform apply       → Terraform

nvim                  → Neovim
vim                   → Vim
```

Keep mappings data-driven.

Do not scatter command matching throughout resolver code.

Unknown processes should fall through to lower-priority sources.

---

## 22. SSH resolution

Detect SSH sessions where process information allows it.

Examples:

```text
ssh root@prod-01
→ prod-01 › SSH

ssh dev@production.example.com
→ production.example.com › SSH
```

Prefer hostname over username.

---

## 23. Git context

When CWD is inside a Git repository, Git information may be used as a fallback or enrichment.

Potential output:

```text
dashboard › MCP
dashboard › MC-13200
```

Avoid excessively long branch names.

Git lookup must not block the poll loop.

Cache Git information when appropriate.

---

## 24. CWD fallback

If no better context exists:

```text
~/projects/dashboard
→ dashboard
```

Use the basename of the current working directory.

For `$HOME`, `/`, or otherwise unusable paths:

```text
Shell
```

Use a safe generic fallback.

---

## 25. Formatting

Preferred format:

```text
<context> › <activity>
```

Examples:

```text
dashboard › Dev
dashboard › Tests
dashboard › OAuth scopes
api › Tests
infra › K8s Logs
prod-01 › SSH
```

Avoid exposing full commands:

```text
dashboard › pnpm test -- --watch --coverage
```

Instead:

```text
dashboard › Tests
```

Maximum title length:

```text
64 characters
```

Make this configurable internally.

---

## 26. Sanitization

Before using a generated title:

1. strip ANSI escape sequences;
2. remove newlines;
3. normalize whitespace;
4. collapse repeated separators;
5. trim whitespace;
6. truncate to the maximum length;
7. reject empty results.

Terminal titles, agent titles, branch names, process names, and CWD values must be treated as untrusted input.

Never construct a shell command by concatenating these values.

---

## 27. Rename operation

Prefer the Herdr socket API for the persistent plugin so the process uses one client for reading and renaming.

Use the raw `tab.rename` method.

If a CLI fallback is required, use:

```go
exec.Command(
    os.Getenv("HERDR_BIN_PATH"),
    ...
)
```

Never use:

```text
sh -c
bash -c
```

for rename operations.

---

## 28. Poll interval and rename rate

Default:

```text
500ms
```

The interval is the rename rate limit: a tab can change name at most once per poll, however fast its pane is changing. A burst of activity between two polls is invisible — only the state at the moment of the poll matters — so no separate debounce is needed.

A snapshot of a six-pane session measured 0.47 ms and 6 KB, so two polls a second cost about a thousandth of a core.

Shortening the interval makes renames land sooner and lets a fast-changing pane rename more often; lengthening it does the reverse.

---

## 29. Deduplication

Before issuing a rename:

```go
if decision.Name == tab.CurrentName {
    return
}
```

No rename request should be sent when the generated title is identical to the current title.

This is mandatory.

---

## 30. Rename loop prevention

The plugin must not create a loop:

```text
resolver
  ↓
tab.rename
  ↓
tab.renamed
  ↓
resolver
  ↓
same name
  ↓
no-op
```

Expected-rename tracking must also prevent plugin-generated renames from being interpreted as manual.

---

## 31. Connection failures

Every call dials its own connection, so there is no connection to lose and nothing to reconnect. A failed poll is a failed request:

1. log it;
2. keep the loop running;
3. try again on the next tick.

A temporary Herdr restart must not permanently terminate Auto Title. Because each poll dials afresh, recovery needs no backoff logic: the poll interval is the retry interval.

---

## 32. Graceful shutdown

Handle:

```text
SIGTERM
SIGINT
```

Shutdown sequence:

```text
stop the ticker
↓
let the poll in flight finish or be cancelled
↓
exit
```

Do not leave background goroutines running after shutdown.

---

## 33. Concurrency model

One goroutine. The poll loop reads the session, decides, and renames in sequence; a tab's rename is a single request that costs well under a millisecond.

Cancellation propagates through the context handed to `Run`, so a signal ends the poll in flight rather than waiting for it.

---

## 34. Logging

Use Go `log/slog`.

Default level:

```text
INFO
```

Support:

```text
DEBUG
INFO
WARN
ERROR
```

Example:

```text
INFO tab renamed
    tab_id=...
    old="dashboard"
    new="dashboard › Tests"
    reason="known_command"

DEBUG poll completed
    type="pane.updated"
    pane_id="..."

WARN rename failed
    tab_id="..."
    error="..."
```

Do not log raw terminal output or sensitive command arguments by default.

---

## 35. Configuration

Keep V1 configuration minimal.

Potential environment variables:

```text
HERDR_AUTO_TITLE_DEBUG
HERDR_AUTO_TITLE_POLL_MS
HERDR_AUTO_TITLE_MAX_LENGTH
```

Do not introduce a configuration file in V1 unless necessary.

---

## 36. Future LLM extension

The architecture must allow a future semantic fallback without changing the poll loop or the state it builds.

Current:

```text
DeterministicResolver
```

Future:

```text
DeterministicResolver
        ↓
confidence insufficient
        ↓
LLMResolver
```

The LLM resolver must remain optional and must never be required for normal operation.

V1 must contain no LLM implementation.

---

## 37. Testing

### Resolver tests

Cover:

- agent title;
- terminal title;
- known process;
- SSH;
- Git;
- CWD;
- generic fallback;
- title sanitization;
- title truncation.

### Manual rename tests

Cover:

```text
user rename → automatic naming disabled
plugin rename → automatic naming remains enabled
```

### Poll tests

```text
10 changes between two polls
→ one reconciliation
```

### Deduplication tests

```text
same generated name
→ zero rename calls
```

### Failure tests

```text
socket disconnect
→ retry on the next tick
→ snapshot
→ keep the loop alive
→ continue
```

### Concurrency tests

Run:

```text
go test -race ./...
```

The race detector must pass.

---

## 38. Stub Herdr client

Implement a stub client for tests. It describes a session and records what was
done to it:

```go
type StubClient struct {
    tabs    map[string]TabInfo
    panes   map[string]PaneInfo
    renames []RenameCall
}
```

A rename must change the stub's own label, so the next poll agrees with it the
way Herdr would; otherwise a test cannot tell deduplication from a bug.

Tests should be able to simulate:

```text
tab created
↓
pane created
↓
cwd = ~/work/dashboard
↓
dashboard

process changes to pnpm test
↓
dashboard › Tests

agent title changes
↓
dashboard › OAuth scopes
```

---

## 39. Performance requirements

Target startup time:

```text
< 50ms
```

on a typical development machine, excluding Herdr socket connection time.

Target change-to-title latency:

```text
< 300ms
```

under normal conditions, which the default 500 ms poll interval bounds.

The plugin must not continuously grow memory.

Closed tabs and their timers must be removed.

---

## 40. Security

Never execute terminal-derived values through a shell.

Bad:

```go
exec.Command("sh", "-c", "herdr tab rename "+name)
```

Good:

```go
exec.Command(
    herdrBin,
    "tab",
    "rename",
    tabID,
    "--name",
    name,
)
```

Prefer the raw Herdr socket API for the long-running process.

Do not send terminal content or agent context to external services.

---

## 41. Example behavior

### New project shell

```text
cwd: ~/work/dashboard
process: zsh

→ dashboard
```

### Test command

```text
cwd: ~/work/dashboard
process: pnpm test

→ dashboard › Tests
```

### Development server

```text
cwd: ~/work/dashboard
process: pnpm dev

→ dashboard › Dev
```

### Claude Code

```text
cwd: ~/work/dashboard
agent: Claude Code
terminal title: Implement OAuth scopes

→ dashboard › OAuth scopes
```

### SSH

```text
ssh root@prod-01

→ prod-01 › SSH
```

### Kubernetes

```text
kubectl logs deployment/api

→ infra › K8s Logs
```

### Manual rename

```text
automatic:
dashboard › Tests

user:
Important work

→ Important work
```

The plugin must stop modifying that tab until manual protection is reset.

---

## 42. Acceptance criteria

The implementation is complete when:

- [ ] plugin is named **Herdr Auto Title**;
- [ ] repository/package name is `herdr-auto-title`;
- [ ] plugin ID is `herdr.auto-title`;
- [ ] implementation is entirely Go;
- [ ] no Node/Bun/Python runtime is required;
- [ ] plugin is distributed as a standalone executable;
- [ ] plugin starts through a Herdr startup hook;
- [ ] plugin maintains a persistent process;
- [ ] plugin connects to the Herdr socket;
- [ ] plugin reads the session with `session.snapshot`;
- [ ] plugin polls `session.snapshot` on an interval;
- [ ] plugin decides from freshly read state, not from remembered state;
- [ ] plugin supports multiple tabs independently;
- [ ] a tab renames at most once per poll;
- [ ] plugin avoids duplicate rename calls;
- [ ] plugin detects manual tab renames;
- [ ] plugin preserves manual names;
- [ ] plugin does not interpret its own renames as manual;
- [ ] plugin uses deterministic title resolution;
- [ ] agent context is used when meaningful;
- [ ] terminal titles are used when meaningful;
- [ ] common development commands are recognized;
- [ ] SSH sessions are recognized;
- [ ] Git/CWD are available as fallbacks;
- [ ] generated titles are sanitized;
- [ ] generated titles are length-limited;
- [ ] a failed poll is retried on the next tick;
- [ ] every poll decides from freshly read state;
- [ ] graceful shutdown is implemented;
- [ ] `go test -race ./...` passes;
- [ ] no external API calls are made;
- [ ] README documents installation, architecture, configuration, and troubleshooting.

---

## 43. Core design principle

> **Auto Title reacts to changes in terminal context; it does not continuously try to understand the terminal.**

The core data flow is:

```text
Herdr session change
    ↓
update cached state
    ↓
poll
    ↓
deterministic resolver
    ↓
is title different?
    ├── no → nothing
    └── yes → tab.rename
```

There must be:

- no polling loop;
- no LLM call;
- no transcript scanning;
- no external service dependency.

The architecture must remain extensible so a future LLM resolver can be added as a low-priority fallback without changing the poll loop, the state it builds, or the rename path.
