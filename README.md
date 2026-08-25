<div align="center">

# Herdr Auto Title

**Tabs that say what you are doing.**

</div>

A [Herdr](https://herdr.dev) plugin that reads your session twice a second and
keeps every tab's title in step with the work in it. Rename a tab yourself and
it leaves that tab alone from then on.

## Demo

<!-- Drag the video into a GitHub issue or PR comment, then paste the
     https://github.com/user-attachments/assets/... URL it gives you here. -->

## Quick start

Requires Herdr 0.8.2+ and Go 1.24+ — the plugin is compiled from source on your
machine when Herdr installs it.

```sh
herdr plugin install kryptamine/herdr-auto-title
```

That is the whole setup. Herdr clones the repository, builds the binary and
starts it; `herdr plugin list` shows it, `herdr plugin disable` turns it off.

## What your tabs will be called

```
~/work/dashboard                       →  dashboard
nvim editing auth.provider.ts          →  nvim › auth.provider.ts
an agent working on OAuth scopes       →  dashboard › claude › Implement OAuth scopes
ssh into prod-01                       →  ssh › prod-01
$HOME                                  →  Shell
```

Titles read `<context> › <activity>`, capped at 64 columns of the tab bar. The
activity is the first thing that has something to say — what an agent reports it
is working on, then the terminal title, then a lone program in the pane. The
context is the directory you are in, or the machine you reached over `ssh`.

Four rules explain most surprises:

- Your workspace name is not repeated — Herdr already shows it above the tabs.
- Paths, shell prompts and bare program names are dropped; they say where you
  are, which the context has said already.
- A tab with several panes is named after one: the focused one, the one running
  a busy agent, or the one that changed last.
- **A tab you renamed is yours.** Auto Title never touches it again.

## Configuration

No configuration file. Four environment variables, and an unusable value is a
warning rather than a failure to start.

| Variable | Default | Meaning |
|----------|---------|---------|
| `HERDR_AUTO_TITLE_DEBUG` | `false` | Log at DEBUG instead of INFO |
| `HERDR_AUTO_TITLE_POLL_MS` | `500` | How often the session is read; also the fastest a tab can be renamed |
| `HERDR_AUTO_TITLE_MAX_LENGTH` | `64` | Title width in tab-bar columns; CJK and emoji take two each |
| `HERDR_AUTO_TITLE_MANUAL_FILE` | `<config>/herdr-auto-title/manual-names.json` | Where tabs you renamed by hand are remembered |

## Privacy

Auto Title makes no network request and runs no subprocess. It reads the Herdr
socket, and the only thing it ever writes back is a tab label.

There is no LLM, no telemetry and no transcript reading. Every value that
reaches a title is stripped of escapes and control characters first, and nothing
derived from terminal output is passed to a shell — renames go over the socket
API. Raw terminal output is never logged.

## Troubleshooting

- **Nothing is renamed.** That is the normal state once every tab is right; the
  loop logs only when it acts. Run with `HERDR_AUTO_TITLE_DEBUG=1` to watch it.
- **A tab renames whenever I switch files.** It follows your editor's title.
  Raise `HERDR_AUTO_TITLE_POLL_MS` to slow it down.
- **A tab keeps the name I gave it.** By design. To hand it back: stop the
  plugin, remove that tab's entry from `manual-names.json`, start it again.
  Editing the file while it runs does nothing — the locks are held in memory.
- **A tab I renamed lost its name.** A rename made while the plugin was not
  running is not remembered; Herdr reuses tab ids between sessions, and a lock
  is only trusted while the tab still carries the name it was locked with.
- **It exits with "HERDR_SOCKET_PATH is not set".** It has to be started by
  Herdr, or from inside a Herdr pane.

## Documentation

- [Architecture](docs/architecture/) — how it works and why: the poll loop, how
  a tab becomes a name, and the measured facts about the Herdr socket API.
- [Development](docs/development.md) — working on it.

MIT — see [LICENSE](LICENSE).
