# otmux roadmap

## 1.0.0: first release (Windows)

- [x] Daemon with workspaces and tabs; shells keep running after detach
- [x] ConPTY panes with a VT emulator for each pane in the daemon
- [x] Attach / detach / reattach with screen snapshots
- [x] Split panes (`v` / `h`), focus by direction or click, resize by keys or by dragging a divider, zoom, close
- [x] Several workspaces: `otmux new/attach/ls/kill`, picker, create / rename / switch from inside
- [x] Saved workspaces with a default folder
- [x] Keys that need no Shift, a key guide on the prefix, and a command palette listing every command
- [x] Mouse: click panes, tabs and workspaces; select to copy, right-click to paste; wheel scrollback
- [x] Watch list: coding agents join automatically across workspaces, with working / waiting status from activity, bells and notifications
- [x] Layouts (sidebar, two bars, compact), with automatic fallback for small windows
- [x] Eight themes, checked for WCAG contrast, colouring the whole window
- [x] Settings panel and settings file
- [x] Welcome splash in fresh tabs
- [x] End-to-end tests driving a real daemon, shell, PTY and the real binary

## Next

- [ ] Copy mode with vi keys, for keyboard-only scrollback and selection
- [ ] Custom key bindings and custom themes in the settings file
- [ ] Bindings that don't need the prefix (e.g. `alt+h/j/k/l`)
- [ ] `otmux notify` for agent hooks (e.g. Claude Code), for exact agent status
- [ ] Workspace templates: `otmux up` opens tabs that are already running commands
- [ ] Layout save and restore, so workspaces come back after a reboot
- [ ] Kill whole process trees when a pane closes (Job Objects / process groups)

## Linux and macOS

- [ ] Test by hand on common terminals (iTerm2, Ghostty, Kitty, Alacritty, GNOME Terminal)
- [ ] Package releases: winget, scoop, Homebrew, .deb/.rpm

## Remote control

- [ ] The protocol over TLS / WebSocket, with token authentication
- [ ] Several clients with roles: owner, collaborator, read-only viewer
- [ ] Share a session link with a teammate
