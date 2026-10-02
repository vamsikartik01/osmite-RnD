# otmux

[![otmux CI](https://github.com/vamsikartik01/osmite-RnD/actions/workflows/otmux-ci.yml/badge.svg)](https://github.com/vamsikartik01/osmite-RnD/actions/workflows/otmux-ci.yml)
[![Latest otmux release](https://img.shields.io/github/v/release/vamsikartik01/osmite-RnD?filter=otmux%2Fv*&label=otmux)](https://github.com/vamsikartik01/osmite-RnD/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/vamsikartik01/osmite-RnD?filename=otmux%2Fgo.mod)](go.mod)
[![License: MIT](https://img.shields.io/github/license/vamsikartik01/osmite-RnD)](../LICENSE)

A terminal workspace for developers. Workspaces, tabs and split panes keep
running when you close the terminal, and you can attach again from anywhere.
Drive it with tmux-style keys, the mouse, or a searchable command palette.
Windows first, with Linux and macOS from the same codebase.

> **Version 1.0.0**: the first release. Workspaces, tabs, split panes,
> detach/reattach, the watch list for coding agents, layouts, themes and
> settings all work on Windows; Linux and macOS builds are published from the
> same code. See the [changelog](CHANGELOG.md) and the [roadmap](docs/roadmap.md).

## Install

**Windows**, one line in PowerShell (installs for your user, no admin; adds
otmux to your PATH):

```powershell
irm https://raw.githubusercontent.com/vamsikartik01/osmite-RnD/main/otmux/install.ps1 | iex
```

Or download the program directly. These links always give the newest version:

| Platform | Download |
|---|---|
| Windows (x64) | [otmux-windows-amd64.exe](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-windows-amd64.exe) |
| Windows (ARM) | [otmux-windows-arm64.exe](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-windows-arm64.exe) |
| macOS (Apple silicon) | [otmux-darwin-arm64](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-darwin-arm64) |
| macOS (Intel) | [otmux-darwin-amd64](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-darwin-amd64) |
| Linux (x64) | [otmux-linux-amd64](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-linux-amd64) |
| Linux (ARM) | [otmux-linux-arm64](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-linux-arm64) |

Checksums are in [checksums.txt](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/checksums.txt);
every version is also on the [releases page](https://github.com/vamsikartik01/osmite-RnD/releases).
On macOS and Linux, make the file executable (`chmod +x otmux-*`) and move it
onto your PATH as `otmux`.

The Windows build isn't code-signed yet, so SmartScreen may say "Windows
protected your PC" the first time: choose *More info* → *Run anyway*.

### Build from source

Requires Go 1.26 or later; an older Go downloads the right toolchain
automatically.

```sh
cd otmux
go build -o bin/ ./cmd/otmux     # produces bin/otmux (bin/otmux.exe on Windows)
./bin/otmux                      # attach to the "default" workspace
```

## Quick start

Run `otmux`. Press `ctrl+b` then `v` to split, `ctrl+b` then `d` to detach,
and run `otmux` again: everything is where you left it. Press `ctrl+b` then
`Space` to see every command.

## Usage

```
otmux                   attach to the "default" workspace, creating it if needed
otmux new [name]        create a workspace and attach (name defaults to the current folder)
otmux attach <name>     attach to an existing workspace           (alias: a)
otmux ls                list workspaces                           (alias: list)
otmux kill <name>       close a workspace and every shell in it
otmux kill-server       stop the daemon and every shell
otmux keys              print all key bindings
otmux version
```

A new workspace starts its shells in the directory you ran `otmux new` from,
so `cd ~/src/api && otmux new` gives you a workspace called `api`.

## Keys

Everything starts with the prefix, `ctrl+b`. Press it and the bottom bar
turns into a guide to the most useful keys; `ctrl+b Space` opens the command
palette, which lists every command with its key. None of the main keys need
Shift, except Shift+arrows for resizing.

**Panes**

| Key | Action |
|---|---|
| `v` | split side by side (vertical divider) |
| `h` | split stacked (horizontal divider) |
| `←` `→` `↑` `↓` | focus the pane in that direction |
| `Shift` + arrows | grow the focused pane (`Alt` + arrows for bigger steps) |
| `o` | focus the next pane |
| `z` | zoom the pane to fill the tab, or unzoom it |
| `x` | close the pane |

Arrow and Shift+arrow keys repeat: after the first press, keep pressing them
for a moment without the prefix.

**Tabs**

| Key | Action |
|---|---|
| `c` or `t` | new tab: you're asked for a name; leave it empty for a random "-ite" word like *finite*, *satellite* or *claudite* |
| `n` / `p` | next / previous tab |
| `l` | last tab |
| `0`–`9` | jump to tab |
| `r` | rename tab |
| `q` | close tab |

**Watch list** (across all workspaces)

| Key | Action |
|---|---|
| `m` | watch / unwatch the current tab |
| `Tab` / `Shift+Tab` | next / previous watched tab, in any workspace (repeats) |

**Workspaces**

| Key | Action |
|---|---|
| `w` | workspace picker; type a new name to create one |
| `a` | new workspace |
| `e` | rename workspace |
| `[` / `]` | previous / next workspace |
| `b` | switch between the Sidebar and Two bars layouts |

Switching to a workspace brings back the tab and pane you last used there.

**General**

| Key | Action |
|---|---|
| `Space` or `/` | command palette: search every command, tab and workspace |
| `s` | settings |
| `d` | detach |
| `ctrl+b` | send a literal `ctrl+b` to the shell |

The classic tmux keys still work as aliases: `%` `"` `,` `&` `$` `(` `)` `:`
`?`.

**Mouse**

- Everything in the bars and sidebar is clickable: workspaces, tabs, `+`
  for a new tab or workspace, **detach**, and **Settings**.
- Click a pane to focus it; drag a divider to resize.
- Drag to select text; letting go copies it. Right-click pastes, or copies
  the selection if there is one, as in Windows Terminal.
- The wheel scrolls back through a pane's history, and a badge shows how far
  back you are. Typing returns to the live screen. Full-screen programs
  (`less`, `man`) get arrow keys instead, and programs that use the mouse
  (`vim`, `htop`) get the mouse events themselves.

## Watch: coding agents and tabs you're keeping an eye on

The **WATCH** list at the top of the sidebar (or after the workspaces in the
two-bars layout) holds tabs from every workspace that you want to keep an eye
on. It shows tab names only; click one to jump to it.

- Tabs running an AI coding agent (Claude Code, Codex, Gemini CLI, Aider,
  opencode, Cursor Agent, Goose, Amp, Qwen Code, Crush) **join it by
  themselves**, and leave when the agent exits. otmux checks the processes in
  each pane every 1.5 seconds, matching the program name, or the script when
  an agent runs through node or python.
- `ctrl+b m` watches or unwatches any tab. Unwatching an agent's tab keeps it
  off the list.

| | |
|---|---|
| ● (blinking) | the agent is thinking / working: producing output by itself |
| ◉ (yellow) | the agent is idle at its prompt, waiting for you, whether or not you're on the tab |
| ○ | a tab you watch by hand, with no agent in it |

Output right after you type or resize doesn't count as working, so typing
into an agent's prompt doesn't make it blink.

A bell or desktop notification (BEL, OSC 9, OSC 777) from a tab you're not on
marks it ◉ at once, even mid-spinner (for example a permission prompt), and
works for watched tabs without an agent too; Windows Terminal progress-bar
updates (OSC 9;4) are ignored. To get these from Claude Code, set its
notification channel to the terminal bell (`/config` → notifications). Going to
the tab clears ◉. `ctrl+b Tab` / `Shift+Tab` cycle through the list.

## Layouts

Choose one in **Settings › Appearance**, or press `ctrl+b b` to switch
between Sidebar and Two bars.

| Layout | Workspaces | Tabs |
|---|---|---|
| **Sidebar** (default) | sidebar on the left: the watch list on top, then workspaces (current one highlighted, saved ones dimmed) | bottom bar |
| **Two bars** | bottom bar | top bar |
| **Compact** | bottom bar | bottom bar, after the workspaces |

Windows narrower than 90 columns fall back from Sidebar to Two bars, and
windows shorter than 12 rows fall back to Compact. The bottom bar always ends
with the **detach** button, the prefix hint and the clock.

## Settings

`ctrl+b s` opens the settings panel. Use the menu on the left, `←` `→` to move
between the menu and the list, and `enter` to choose.

- **Workspaces:** save a workspace with a name and a folder. Saved
  workspaces appear in the sidebar, in the picker (`w`) and in search, and
  opening one starts its shells in that folder. `otmux new <name>` uses the
  saved folder too. Remove one with its **✕** button, or select it and press
  `d`; in the workspace picker, `ctrl+d` removes the selected saved workspace.
  Removing only forgets the saved entry: a running workspace keeps running
  (use `otmux kill <name>` to close it).
- **Appearance:** the theme, how pane colours are handled, and the layout.
  Themes colour the whole window, panes included: the background, text and
  the 16 ANSI colours programs use. Set *Pane colours* to *terminal's own*
  to keep your terminal's scheme inside panes.

  | Theme | Look |
  |---|---|
  | osmite-dark (default) | Apple-style dark greys, purple accent |
  | graphite | the same greys, blue accent |
  | calcite | light |
  | azurite | deep navy, sky blue |
  | malachite | forest green |
  | rhodonite | warm rose |
  | pyrite | gold on charcoal |
  | sugilite | deep purple |

  Every theme is checked by a test against WCAG contrast: at least 7:1 for
  text on the main backgrounds, 4.5:1 for secondary text, and 3:1 for ANSI
  colours.
- **Keys:** choose the prefix: `ctrl+b`, `ctrl+a`, `ctrl+space` or `ctrl+g`.
- **About:** the version, and where the settings, socket and log are.

Settings are stored as JSON in `%AppData%\otmux\config.json` on Windows,
`~/Library/Application Support/otmux/config.json` on macOS, and
`~/.config/otmux/config.json` on Linux (`OTMUX_CONFIG` overrides the path).

## Environment

| Variable | Effect |
|---|---|
| `OTMUX_PREFIX` | prefix key, e.g. `ctrl+a`; overrides the one chosen in settings |
| `OTMUX_CONFIG` | path of the settings file |
| `OTMUX_SHELL` | shell for new panes (default: `pwsh`, then `powershell`, then `cmd` on Windows; `$SHELL` elsewhere) |
| `OTMUX_SOCKET` | daemon socket path, for running an isolated daemon while developing |

The daemon log is next to the socket:
`%LOCALAPPDATA%\otmux\otmux.log` on Windows, and
`$XDG_RUNTIME_DIR/otmux/otmux.log` or `/tmp/otmux-$UID/otmux.log` elsewhere.

The UI uses 24-bit colour. It looks best in Windows Terminal, iTerm2,
Ghostty, Kitty, WezTerm or Alacritty.

## How it works

```
 otmux (TUI client)        future: remote / web client
        │                          │
        └──── otmux protocol ──────┘   unix socket today; TLS later
                   │
          ┌────────▼──────────┐
          │   otmux daemon    │  keeps running after clients detach
          │ workspaces › tabs │
          │ › split tree      │
          │ › panes, with a   │
          │   VT emulator     │
          │   each            │
          └───┬───────────┬───┘
            ConPTY     Unix PTY
```

The first `otmux` you run starts a background daemon, and every later `otmux`
connects to it. The daemon owns the shells and keeps an emulated copy of each
pane's screen. That copy is what lets you detach, reattach and, later, attach
remotely. See [docs/architecture.md](docs/architecture.md) for the details,
and [ADR 0001](docs/adr/0001-own-daemon-instead-of-tmux.md) for why otmux
doesn't use tmux underneath.

## Development

```sh
go test ./...          # unit tests plus end-to-end tests: real daemon, real shell, real PTY
go test -short ./...   # skip the test that builds and drives the binary
go vet ./...
```

Layout:

```
cmd/otmux/           CLI entry point; tui_test.go drives the real binary in a PTY
internal/client/     terminal UI: pane mirrors, dividers, status bar, palette, mouse
internal/daemon/     session server: workspaces, tabs, panes, broadcasting
internal/layout/     split tree: geometry, focus by direction, resize (pure, unit tested)
internal/keys/       prefix-key state machine, default keymap, command catalog
internal/protocol/   client <-> daemon wire format
internal/pty/        PTY interface (ConPTY / Unix PTY via charmbracelet/x/xpty)
internal/platform/   per-OS paths, default shell, daemonizing (build tags)
internal/version/    build version
docs/                architecture, ADRs, roadmap
```

### Releasing

Push a tag like `otmux/v1.0.1` (after bumping `internal/version`). The
[release workflow](../.github/workflows/otmux-release.yml) tests, builds every
platform, publishes the version's release, and updates the rolling
`otmux/latest` release that the download links and `install.ps1` use.
`./release.ps1` builds the same files locally into `dist/`, to try a release
build first.

Only `internal/platform` has OS-specific files. Keep it that way: everything
else must build and behave the same on every OS. CI runs the tests on Windows,
Linux and macOS for every change.
