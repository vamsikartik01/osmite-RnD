# Security policy

## Supported versions

Security fixes go into the latest release of each project. For otmux, that is
the newest `otmux/vX.Y.Z` release.

## Reporting a vulnerability

Please **don't open a public issue** for security problems. Instead, report
them privately through GitHub:
[Security → Report a vulnerability](https://github.com/vamsikartik01/osmite-RnD/security/advisories/new).

Include what you found, how to reproduce it, and the version you used. You'll
get a reply within a week. Once a fix is released, the advisory is published
and you're credited, unless you'd rather not be.

## Scope notes for otmux

The otmux daemon listens on a local socket that only your user can access
(`%LOCALAPPDATA%\otmux\` on Windows, `$XDG_RUNTIME_DIR/otmux/` or
`/tmp/otmux-$UID/` elsewhere). Anything that lets another local user or a
remote party talk to that socket, read pane contents, or run commands in your
panes is in scope.
