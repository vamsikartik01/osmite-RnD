# Changelog

All notable changes to otmux are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and otmux uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Release tags are
`otmux/vX.Y.Z`.

## [Unreleased]

## [1.3.0] - 2026-10-02

### Added

- Remote mode: open this machine's workspaces from a browser, on desktop or
  phone, at otmux.osmite.site. Settings › Remote › Connect account links the
  machine to your account (approve a code in the browser; works over SSH
  too), and the Remote mode toggle keeps the daemon connected. The machine
  connects out, so there are no ports to open. The status bar shows when a
  browser is attached. Revoking the device in the portal turns remote mode
  off. The link lives in `remote.json` next to the settings, readable only by
  you.

## [1.2.0] - 2026-10-02

### Added

- `otmux restart` stops the background service and every shell, then opens
  otmux again, e.g. to finish an update. It asks first (`-y` skips that) and
  refuses to run from inside otmux, where it would close its own shell.

### Changed

- **A cleaner window.** Tabs sit in a strip along the top. Every pane has a
  title row showing what runs in it (the shell or coding agent) and its
  folder, with the focused pane's title lifted. The sidebar has an edge,
  the otmux mark and each watched agent's status ("working", "waiting").
  The bottom bar shows where you are and the main keys.
- The command palette and Settings use a quieter selection, show each
  group name once, and draw keys bright with the prefix dim.
- A working agent shows a spinner instead of a blinking dot; the logo and
  mark use a gradient drawn from the theme's accent; the welcome screen's
  keys look like keycaps.

## [1.1.4] - 2026-10-02

### Fixed

- `install.sh` no longer looks stuck on "Downloading otmux": it shows a
  progress bar, and when one of GitHub's download servers is unreachable it
  moves on to another after a few seconds instead of waiting a minute.

## [1.1.3] - 2026-10-02

### Added

- **Linux and macOS installer**, one line in a terminal:
  `curl -fsSL https://raw.githubusercontent.com/vamsikartik01/osmite-RnD/main/otmux/install.sh | sh`.
  It checks the download against the release's checksums and installs to
  `~/.local/bin`, no sudo needed.

### Fixed

- On Linux and macOS, Ctrl+C now stops the running command and Ctrl+Z / `fg`
  work: shells in panes run with the pane as their terminal. Before, panes
  printed "no job control in this shell" and ignored Ctrl+C.

## [1.1.2] - 2026-10-02

### Fixed

- Releases now include `version.txt`, so *Update now*, automatic updates,
  the download links and the installer find the latest version again
  (they got "404 not found" with 1.1.0 and 1.1.1). The release checks every
  file is present before it updates the download links.

## [1.1.1] - 2026-10-02

### Fixed

- The rolling "latest" release, which the download links, the installer and
  automatic updates use, wasn't updated for 1.1.0. It now carries 1.1.1.

### Changed

- Only official release builds update themselves; a copy built from source
  shows `(built from source)` in `otmux version` and is never replaced.

## [1.1.0] - 2026-10-02

### Added

- **Automatic updates.** otmux checks for a new release once a day, in the
  background, downloads it, verifies it against the release's SHA-256
  checksums, and swaps it in. Running shells are never restarted: the new
  version starts the next time you run otmux, and a note in the status bar
  says so. Copies installed by a package manager, or in folders you can't
  write to, aren't touched.
- **Settings › Updates**: turn automatic updates on or off, update now, and
  see the installed and latest versions.
- `otmux update` updates from the command line; `OTMUX_NO_UPDATE=1` turns
  automatic updates off.
- Releases publish `version.txt`, which running copies use to find out about
  new versions.

## [1.0.0] - 2026-10-02

The first release. Windows is the primary platform; Linux and macOS builds are
published from the same code.

### Added

- **Persistent workspaces.** A background daemon owns every shell, so closing
  the terminal or detaching (`ctrl+b d`) leaves everything running; `otmux`
  reattaches. Panes run on ConPTY (Windows) or a Unix PTY, each with its own
  terminal emulator in the daemon.
- **Tabs and split panes**: split side by side (`v`) or stacked (`h`), focus
  with the arrows or a click, resize with Shift+arrows or by dragging a
  divider, zoom (`z`), close (`x`).
- **Workspaces**: create, rename, switch and close them from the CLI
  (`otmux new / attach / ls / kill`) or inside otmux. Saved workspaces open
  in their own folder.
- **Watch list**: tabs running a coding agent (Claude Code, Codex, Gemini CLI,
  Aider, opencode, Cursor Agent, Goose, Amp, Qwen Code, Crush) join it on
  their own, across workspaces, showing whether the agent is working or
  waiting for you. Bells and desktop notifications flag a tab at once. Any
  tab can be watched by hand (`ctrl+b m`); `ctrl+b Tab` cycles through them.
- **Keys and mouse**: a prefix key with a key guide, a searchable command
  palette (`ctrl+b Space`), keys that need no Shift; click panes, tabs and
  workspaces, select to copy, right-click to paste, wheel to scroll back.
- **Layouts**: sidebar (default), two bars, or compact, falling back
  automatically in small windows.
- **Themes**: eight, including osmite-dark (default) and a light theme, each
  checked against WCAG contrast and colouring the whole window.
- **Settings panel** (`ctrl+b s`) and a settings file.
- **Welcome splash** in fresh tabs.
- **Install**: a one-line PowerShell installer and fixed download links to
  the newest version.

[Unreleased]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.3.0...HEAD
[1.3.0]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.2.0...otmux/v1.3.0
[1.2.0]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.1.4...otmux/v1.2.0
[1.1.4]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.1.3...otmux/v1.1.4
[1.1.3]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.1.2...otmux/v1.1.3
[1.1.2]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.1.1...otmux/v1.1.2
[1.1.1]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.1.0...otmux/v1.1.1
[1.1.0]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.0.0...otmux/v1.1.0
[1.0.0]: https://github.com/vamsikartik01/osmite-RnD/releases/tag/otmux/v1.0.0
