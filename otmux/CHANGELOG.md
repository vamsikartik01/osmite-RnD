# Changelog

All notable changes to otmux are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and otmux uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Release tags are
`otmux/vX.Y.Z`.

## [Unreleased]

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

[Unreleased]: https://github.com/vamsikartik01/osmite-RnD/compare/otmux/v1.0.0...HEAD
[1.0.0]: https://github.com/vamsikartik01/osmite-RnD/releases/tag/otmux/v1.0.0
