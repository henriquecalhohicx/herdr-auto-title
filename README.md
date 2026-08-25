# Herdr Auto Title

A [Herdr](https://herdr.dev) plugin that names your tabs for you.

Auto Title runs as a long-lived process next to Herdr, reads the session twice a
second, and keeps every tab's title in step with what that tab is actually
doing. It is written in Go and ships as a single executable: no Node, Bun or
Python runtime, no database, no external service, and no LLM.

```
~/work/dashboard                                →  dashboard
~/work/dashboard on branch feature/MC-13200     →  dashboard › MC-13200
nvim editing auth.provider.ts                   →  nvim › auth.provider.ts
an agent working on OAuth scopes                →  dashboard › claude › Implement OAuth scopes
ssh into prod-01                                →  ssh › prod-01
$HOME                                           →  Shell
```

Rename a tab yourself and Auto Title leaves it alone.

## Installation

```sh
herdr plugin install kryptamine/herdr-auto-title
```

Herdr clones the repository, builds the binary and launches it through its
startup hook. It is built from source at install time, so **Go 1.24 or newer has
to be on the machine** — there is nothing else to install.

Add `--ref <tag>` to pin a version. `herdr plugin list` shows it once it is in,
`herdr plugin disable` turns it off without removing it, and
`herdr plugin uninstall` takes it out.

Auto Title reads `HERDR_SOCKET_PATH` from the environment Herdr gives it and
refuses to start without it, so it only runs as a plugin or from inside a Herdr
pane.

### From a checkout

To work on it, link a clone instead of installing a copy:

```sh
git clone https://github.com/kryptamine/herdr-auto-title
herdr plugin link herdr-auto-title
```

Or build and run it by hand in a Herdr pane, where the environment is already
set for you:

```sh
go build -o herdr-auto-title ./cmd/herdr-auto-title
./herdr-auto-title
```

## What your tabs will be called

A title reads from the general to the particular, `<context> › <activity>`,
capped at 64 columns of the tab bar.

The **activity** is the first of these that has something to say:

1. what an agent reports it is working on;
2. the terminal title, if it says more than where you are;
3. the program running in the pane, when it is the only one — `nvim`, `psql`;
4. the git branch, when nothing above spoke.

The **context** is the directory you are in, or the machine you reached over
`ssh`. Either half can come from a different source than the other, so an agent
working in `~/work/dashboard` gives `dashboard › claude › …`.

A few rules are worth knowing because they explain a title that surprised you:

- **Your workspace name is not repeated.** Herdr shows it above the tabs
  already, so in a workspace called `dashboard` a tab reads `nvim › auth.ts`.
- **Branches are shortened to what identifies the work.** An issue key wins
  (`bugfix-asa-cpanel-uapi-mc-13675` → `MC-13675`); otherwise the beginning is
  kept and cut at a whole word. `main` and `master` contribute nothing.
- **Paths, prompts and program names are dropped.** `auth.ts (~/work/src) - Nvim`
  becomes `auth.ts - Nvim`; `root@psi:` and a bare `zsh` say nothing a tab
  needs.
- **The ssh user is dropped.** `root@prod-01` and `deploy@prod-01` are the same
  machine.
- **A tab with several panes is named after one of them** — the focused one, or
  the one running a busy agent, or the one that changed last.

Why each of those is the way it is: [docs/architecture](docs/architecture/).

## Configuration

There is no configuration file. Five environment variables are read at startup;
an unusable value is logged as a warning and the default is kept, so a typo
never stops the plugin from running.

| Variable | Default | Meaning |
|----------|---------|---------|
| `HERDR_AUTO_TITLE_DEBUG` | `false` | Log at DEBUG instead of INFO |
| `HERDR_AUTO_TITLE_POLL_MS` | `500` | How often the session is read; also the fastest a tab can be renamed |
| `HERDR_AUTO_TITLE_MAX_LENGTH` | `64` | Maximum title width in tab-bar columns; CJK and emoji take two each |
| `HERDR_AUTO_TITLE_BRANCH_MAX` | `12` | Most a git branch may add to a title, in columns; `0` leaves branches out |
| `HERDR_AUTO_TITLE_MANUAL_FILE` | `<config>/herdr-auto-title/manual-names.json` | Where tabs you renamed by hand are remembered |

Auto Title logs to stderr through `log/slog`. Raw terminal output and command
arguments are never logged.

## Troubleshooting

**The plugin exits immediately with "HERDR_SOCKET_PATH is not set".**
Auto Title has to be started by Herdr, or from inside a Herdr pane where the
variable is already exported. That is the only thing that stops it: a Herdr
that is down, or not up yet, is waited out rather than given up on.

**Nothing is renamed.**
That is the normal state once every tab carries the name it should — the loop
logs only when it acts. Run with `HERDR_AUTO_TITLE_DEBUG=1` and watch stderr:
`poll failed` means the snapshot is not coming back, and silence otherwise means
the resolver keeps producing the title each tab already has.

**A tab renames every time I switch files in my editor.**
Expected: the tab follows the editor's title, which follows the buffer. Raise
`HERDR_AUTO_TITLE_POLL_MS` to slow it down — the interval is also the fastest a
tab can change name.

**A tab keeps the name I gave it.**
That is the point. To hand it back, stop the plugin, remove that tab's entry
from `manual-names.json` — or delete the file — and start it again. Editing the
file while the plugin runs does nothing: the locks live in memory and the file
is rewritten from them. Renaming the tab again does not release it either; the
new name is simply locked in turn.

**A tab I renamed lost its name.**
A rename made while the plugin was not running is not remembered. Herdr's tab
ids belong to a session, so a stored lock is only trusted while the tab still
carries the name it was locked with — otherwise a restart of Herdr would lock
whichever unrelated tabs inherited those ids.

**Everything is called `Shell`.**
The pane's working directory is the home directory or the filesystem root, and
nothing is running in it that names the pane, so there is no context to use.

## Documentation

- [docs/architecture](docs/architecture/) — how it works and why: the poll loop,
  title resolution, sanitizing untrusted values, manual rename protection, and
  the verified facts about the Herdr socket API.
- [docs/development.md](docs/development.md) — working on the code: the loops,
  running against your own session, and what each log line means.

```sh
make          # list every target
make check    # fmt + vet + go test -race ./...
make run      # build and run in your Herdr session with DEBUG logging
make dev      # the same, restarting on every source change
```

## License

MIT — see [LICENSE](LICENSE).
