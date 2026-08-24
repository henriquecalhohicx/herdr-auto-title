# OpenSpec: Herdr Auto Title

## 1. Overview

Implement **Herdr Auto Title**, a Herdr plugin that automatically generates concise, useful tab titles from the current context of each tab.

The plugin must be implemented in **Go** as a standalone long-running process.

The plugin must:

- run as a persistent Herdr plugin process;
- subscribe to Herdr events through the Herdr socket API;
- maintain an in-memory state cache;
- debounce rapid event bursts;
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
                         │ events.subscribe    │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │    Event Router     │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │    State Cache      │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │ Debounce / Dedup    │
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

The plugin must use the Herdr raw Socket API for the persistent event subscription. Herdr explicitly documents the raw socket API as the integration layer for long-lived event subscriptions, and `session.snapshot` is intended for clients maintaining a local runtime cache. The socket API exposes `tab.rename` and `events.subscribe`.

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
│   │   └── events.go
│   ├── state/
│   │   ├── cache.go
│   │   └── tab.go
│   ├── resolver/
│   │   ├── resolver.go
│   │   ├── agents.go
│   │   ├── terminal.go
│   │   ├── processes.go
│   │   ├── ssh.go
│   │   ├── git.go
│   │   └── cwd.go
│   └── debounce/
│       └── manager.go
└── tests/
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
bootstrap session snapshot
        ↓
subscribe to events
        ↓
event loop
        ↓
reconnect on failure
        ↓
graceful shutdown
```

The process must not exit after successful initialization.

---

## 9. Herdr socket client

Use `HERDR_SOCKET_PATH` to locate the Herdr socket/transport.

Do not hard-code a socket path.

The transport implementation must account for Herdr's platform-specific local transport. Do not assume that every platform uses Unix domain sockets.

Create an abstraction such as:

```go
type HerdrClient interface {
    Call(ctx context.Context, method string, params any, result any) error
    Subscribe(ctx context.Context, subscriptions []Subscription) error
    Close() error
}
```

The implementation must support the current Herdr socket protocol.

Do not reimplement Herdr's protocol from assumptions. Inspect the current socket schema using the installed Herdr API/schema facilities when implementing the client.

---

## 10. Bootstrap with session snapshot

After connecting:

1. call `session.snapshot`;
2. build the complete local state cache;
3. establish event subscriptions;
4. reconcile existing tabs.

The snapshot must be treated as the initial state only.

Afterward, the cache must be updated exclusively from events and explicit refreshes.

On reconnect:

1. reconnect;
2. call `session.snapshot` again;
3. replace or reconcile the local cache;
4. resubscribe;
5. continue processing events.

This prevents missed events during a disconnect.

---

## 11. Event subscriptions

Subscribe to currently supported events relevant to title generation.

Candidate subscriptions:

```text
tab.created
tab.renamed
tab.closed
pane.created
pane.updated
pane.agent_detected
pane.agent_status_changed
```

Before implementation, verify the exact event names and payload schemas against the current Herdr Socket API. Do not implement unsupported event names.

Event purposes:

### `tab.created`

Create initial tab state and schedule reconciliation.

### `tab.closed`

Remove the tab from the cache.

### `tab.renamed`

Update the cached name and determine whether the rename was manual or plugin-generated.

### `pane.created`

Create/update pane state and reconcile the parent tab.

### `pane.updated`

Primary trigger for changes such as terminal title, process, cwd, and pane metadata.

### Agent-related events

Reconcile when an agent is detected or changes state/title/context.

---

## 12. Event router

The socket reader must never perform expensive work synchronously.

Use:

```text
socket reader
    ↓
event channel
    ↓
event router
    ↓
state update
    ↓
schedule reconciliation
```

Conceptually:

```go
func (r *Router) Handle(event Event) {
    switch event.Type {
    case TabCreated:
        r.handleTabCreated(event)

    case TabClosed:
        r.handleTabClosed(event)

    case TabRenamed:
        r.handleTabRenamed(event)

    case PaneCreated,
         PaneUpdated,
         PaneAgentDetected,
         PaneAgentStatusChanged:
        r.handlePaneChange(event)
    }
}
```

Unknown events must be ignored safely.

---

## 13. State model

Maintain all active state in memory.

Example:

```go
type TabState struct {
    ID string

    CurrentName string

    Panes map[string]*PaneState

    ManualName bool

    ExpectedRename *ExpectedRename

    Revision uint64

    LastReconciledAt time.Time
}
```

Pane state:

```go
type PaneState struct {
    ID string

    CWD string

    ForegroundProcess string

    TerminalTitle string

    Agent string

    AgentSession string

    AgentStatus string
}
```

The cache must be concurrency-safe.

Closed tabs must be removed immediately.

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
dashboard · Tests

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
→ prod-01 · SSH

ssh dev@production.example.com
→ production.example.com · SSH
```

Prefer hostname over username.

---

## 23. Git context

When CWD is inside a Git repository, Git information may be used as a fallback or enrichment.

Potential output:

```text
dashboard · MCP
dashboard · MC-13200
```

Avoid excessively long branch names.

Git lookup must not block the event loop.

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
<context> · <activity>
```

Examples:

```text
dashboard · Dev
dashboard · Tests
dashboard · OAuth scopes
api · Tests
infra · K8s Logs
prod-01 · SSH
```

Avoid exposing full commands:

```text
dashboard · pnpm test -- --watch --coverage
```

Instead:

```text
dashboard · Tests
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

Prefer the Herdr socket API for the persistent plugin so the process can use the same connection/event client.

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

## 28. Debouncing

Each tab needs an independent debounce timer.

Default:

```text
200ms
```

On a relevant event:

```text
cancel previous timer
start new 200ms timer
```

When the timer fires:

```text
read latest state
↓
select context pane
↓
resolve title
↓
rename if necessary
```

A burst of events:

```text
pane.updated
pane.updated
pane.updated
agent_status_changed
terminal_title changed
```

must result in at most one reconciliation after the burst settles.

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

## 31. Socket reconnect

If the socket disconnects:

1. log the disconnect;
2. stop using the old connection;
3. reconnect with exponential backoff;
4. call `session.snapshot`;
5. rebuild/reconcile the local cache;
6. resubscribe to events;
7. continue processing.

Suggested retry intervals:

```text
100ms
250ms
500ms
1s
2s
5s
10s
```

Cap at approximately 10 seconds.

A temporary Herdr restart must not permanently terminate Auto Title.

---

## 32. Graceful shutdown

Handle:

```text
SIGTERM
SIGINT
```

Shutdown sequence:

```text
stop accepting new events
↓
cancel debounce timers
↓
close socket
↓
wait for active reconciliation operations
↓
exit
```

Do not leave background goroutines running after shutdown.

---

## 33. Concurrency model

Recommended architecture:

```text
                 socket reader
                      │
                      ▼
                 event channel
                      │
                      ▼
                 event router
                      │
                      ▼
                state cache
                      │
                      ▼
               per-tab debounce
                      │
                      ▼
             bounded reconciliation
```

Use goroutines for:

- socket reading;
- reconciliation;
- timers;
- reconnect logic.

Do not create an unbounded number of goroutines.

Protect shared state with a mutex or another appropriate synchronization primitive.

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
    new="dashboard · Tests"
    reason="known_command"

DEBUG event received
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
HERDR_AUTO_TITLE_DEBOUNCE_MS
HERDR_AUTO_TITLE_MAX_LENGTH
```

Do not introduce a configuration file in V1 unless necessary.

---

## 36. Future LLM extension

The architecture must allow a future semantic fallback without changing the event/state infrastructure.

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

### Debounce tests

```text
10 events within 200ms
→ one reconciliation
```

### Deduplication tests

```text
same generated name
→ zero rename calls
```

### Reconnect tests

```text
socket disconnect
→ reconnect
→ snapshot
→ resubscribe
→ continue
```

### Concurrency tests

Run:

```text
go test -race ./...
```

The race detector must pass.

---

## 38. Fake Herdr client

Implement a fake client for tests:

```go
type FakeHerdrClient struct {
    Events  chan Event
    Renames []RenameCall
}
```

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
dashboard · Tests

agent title changes
↓
dashboard · OAuth scopes
```

---

## 39. Performance requirements

Target startup time:

```text
< 50ms
```

on a typical development machine, excluding Herdr socket connection time.

Target event-to-title latency:

```text
< 300ms
```

under normal conditions, including the default 200ms debounce.

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

→ dashboard · Tests
```

### Development server

```text
cwd: ~/work/dashboard
process: pnpm dev

→ dashboard · Dev
```

### Claude Code

```text
cwd: ~/work/dashboard
agent: Claude Code
terminal title: Implement OAuth scopes

→ dashboard · OAuth scopes
```

### SSH

```text
ssh root@prod-01

→ prod-01 · SSH
```

### Kubernetes

```text
kubectl logs deployment/api

→ infra · K8s Logs
```

### Manual rename

```text
automatic:
dashboard · Tests

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
- [ ] plugin uses `session.snapshot` for bootstrap;
- [ ] plugin subscribes to supported relevant events;
- [ ] plugin maintains an in-memory state cache;
- [ ] plugin supports multiple tabs independently;
- [ ] plugin has per-tab debounce;
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
- [ ] socket reconnect works;
- [ ] state is refreshed after reconnect;
- [ ] graceful shutdown is implemented;
- [ ] `go test -race ./...` passes;
- [ ] no external API calls are made;
- [ ] README documents installation, architecture, configuration, and troubleshooting.

---

## 43. Core design principle

> **Auto Title reacts to changes in terminal context; it does not continuously try to understand the terminal.**

The core data flow is:

```text
Herdr event
    ↓
update cached state
    ↓
debounce
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

The architecture must remain extensible so a future LLM resolver can be added as a low-priority fallback without changing the event subscription, state cache, debounce, or rename infrastructure.
