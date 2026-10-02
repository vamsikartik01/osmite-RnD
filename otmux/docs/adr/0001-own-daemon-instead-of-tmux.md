# ADR 0001: Own daemon and PTY layer instead of tmux control mode

**Status:** accepted · **Date:** 2026-10-01

## Context

The first plan was to drive tmux in control mode on Linux and macOS, and to
write a separate ConPTY backend for Windows, with both behind one Go
interface. Three requirements push against that:

1. **Windows is the first target.** tmux doesn't run natively on Windows, so
   the ConPTY backend would have to be built first anyway.
2. **Our own UI needs a terminal emulator regardless.** tmux control mode
   sends each pane's raw output (`%output %3 ...`). To draw panes ourselves we
   still need to parse VT sequences into a cell grid. That emulator is the hard
   part. Spawning a process on a PTY is the easy part: a few dozen lines per
   OS.
3. **Remote control is planned.** That needs a server we own: our own
   protocol, authentication and multi-client semantics. Building it around
   tmux would tie every remote feature to tmux's model.

Two backends would also mean two sets of behaviour for detach, resize,
scrollback and process exit, and the workspace layer would have to hide every
difference.

## Decision

otmux has its own small daemon, the same model WezTerm's mux server and Zellij
use:

- A single PTY interface, using ConPTY on Windows and Unix PTYs elsewhere
  (`charmbracelet/x/xpty`).
- A VT emulator for each pane **inside the daemon** (`charmbracelet/x/vt`).
  It is the source of truth for snapshots on attach, reattach and, later,
  remote viewing.
- A versioned client↔daemon protocol that doesn't assume a local transport.
- tmux key bindings as the default, to keep muscle memory, but no tmux at
  runtime.

## Consequences

- One engine and one behaviour on all three OSes. Linux and macOS cost little
  extra, because only `internal/platform` differs.
- We own terminal-emulation correctness. We depend on `x/vt` and will
  contribute fixes upstream where we can.
- Processes don't survive a reboot, and they don't with tmux either. Restoring
  layouts after a reboot is a separate, planned feature.
- A "tmux import / attach to existing tmux session" feature can still be added
  later as an extra, rather than as the core.
