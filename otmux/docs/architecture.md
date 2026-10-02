# otmux architecture

## Components

```
┌──────────────┐  frames over a unix socket   ┌────────────────────────────────┐
│ otmux client │ ───────────────────────────► │ otmux daemon                   │
│  (TUI)       │ ◄─────────────────────────── │                                │
└──────────────┘                              │  Server ── Workspace ── Tab    │
                                              │                      └─ Pane   │
                                              │                          │     │
                                              │        VT emulator ◄─────┤     │
                                              │                          │     │
                                              │                PTY (ConPTY/unix)│
                                              └──────────────────────────┼─────┘
                                                                         ▼
                                                                  shell process
```

- **Client** (`internal/client`): a full-screen TUI built on
  [ultraviolet](https://github.com/charmbracelet/ultraviolet). It keeps a
  local *mirror* emulator of the active pane, draws it plus the tab bar, and
  sends input and commands to the daemon. It holds no state the daemon can't
  rebuild.
- **Daemon** (`internal/daemon`): owns workspaces → tabs → panes. It keeps
  running when no client is attached and exits when the last workspace closes.
- **Layout** (`internal/layout`): each tab arranges its panes as a binary
  split tree (Row = side by side, Column = stacked), each split holding a
  ratio. The daemon computes pane rectangles and dividers from it and sends
  them in every `State`, so clients only draw what they're told. Focus moves
  by geometry, to the adjacent pane sharing the longest edge. Zoom shows the
  focused pane alone without changing the tree.
- **Pane**: a process on a PTY plus the daemon's
  [x/vt](https://github.com/charmbracelet/x/tree/main/vt) emulator, which
  holds the authoritative copy of the screen.
- **PTY** (`internal/pty`): one interface. ConPTY on Windows and a classic PTY
  on Unix, both via `charmbracelet/x/xpty`.
- **Platform** (`internal/platform`): the only package with OS-specific files.

## Data flow

**Output.** The daemon reads PTY output, writes it into the pane's emulator,
and forwards the same bytes to the workspace's clients if the pane is on
screen: in the active tab, and not hidden behind a zoomed pane. Off-screen
panes keep updating in the daemon only. Each client
feeds them into its own mirror emulator and redraws, at most about 120 times
per second.

**Attach.** When a client attaches, switches tabs or resizes, the daemon sends
a `Snapshot`: VT sequences that redraw the pane from a blank screen, plus the
cursor position. Live `Output` follows. Both are queued under the same lock,
so a client never sees output that belongs before its snapshot.

**Input.** The client sends input as structured events (`Key`, `Paste`,
`Mouse`), not raw bytes, and the **daemon** encodes them for the pane. Only
the daemon's emulator knows the pane's live terminal modes (application cursor
keys, bracketed paste, mouse tracking), so it is the only place that can encode
input correctly. A remote client therefore doesn't need to know anything about
terminal modes.

**Terminal queries.** Programs ask the terminal questions, for example
cursor position or device attributes. The daemon's emulator answers them into
the PTY. Client emulators also generate answers, and the client throws those
away, so each query gets exactly one reply.

## Protocol

Frames are `[u32 length][u8 type][payload]`, defined in `internal/protocol`.
Control messages carry JSON. Pane output is the hot path, so it uses a binary
payload: `[u32 pane id][bytes]`. A `Hello`/`Welcome` handshake carries a
protocol version. A client and daemon with different versions refuse to talk,
and the user is told to run `otmux kill-server`.

The protocol assumes nothing about the transport, so remote access is planned
as the same frames over TLS or WebSocket, with authentication added in front.

## Concurrency

The daemon uses **one mutex** (`Server.mu`) for the whole model, including
every emulator. It is simple and keeps ordering correct. If many panes are
producing heavy output at once, it becomes the bottleneck. The fix then is a
lock per pane, with a channel to the broadcaster.

Each pane runs three goroutines:

1. `pumpOutput`: PTY → emulator → clients.
2. `drainInput`: emulator input pipe → PTY. The pipe is unbuffered, so this
   must always be running, or emulator writes block.
3. A wait goroutine: on process exit it closes the PTY (on Windows the ConPTY
   output pipe doesn't reach EOF by itself) and removes the tab.

Each client has a buffered queue and a writer goroutine. The daemon never
blocks on a slow client. If the queue fills, that client is disconnected. It
can reattach and gets a fresh snapshot, so nothing is lost.

## Per-OS differences

| Concern | Windows | Linux / macOS | Where |
|---|---|---|---|
| PTY | ConPTY | `creack/pty` | `internal/pty` (via xpty) |
| Daemon socket | AF_UNIX in `%LOCALAPPDATA%\otmux\` | `$XDG_RUNTIME_DIR/otmux/` or `/tmp/otmux-$UID/` | `internal/platform` |
| Default shell | `pwsh` → `powershell` → `%ComSpec%` | `$SHELL` → `/bin/sh` | `internal/platform` |
| Background daemon | `DETACHED_PROCESS` + new process group | `setsid` | `internal/platform` |
| Process wait | `xpty.WaitProcess` (Go's `cmd.Wait` doesn't support ConPTY) | `cmd.Wait` | `internal/pty` |
| Kill process tree | *planned:* Job Objects | *planned:* process groups | — |

Windows has supported AF_UNIX sockets since Windows 10 1803, and ConPTY since
1809, so one transport works everywhere.
