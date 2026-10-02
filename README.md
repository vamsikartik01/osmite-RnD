# osmite R&D

[![otmux CI](https://github.com/vamsikartik01/osmite-RnD/actions/workflows/otmux-ci.yml/badge.svg)](https://github.com/vamsikartik01/osmite-RnD/actions/workflows/otmux-ci.yml)
[![Latest otmux release](https://img.shields.io/github/v/release/vamsikartik01/osmite-RnD?filter=otmux%2Fv*&label=otmux)](https://github.com/vamsikartik01/osmite-RnD/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/vamsikartik01/osmite-RnD?filename=otmux%2Fgo.mod)](otmux/go.mod)
[![License: MIT](https://img.shields.io/github/license/vamsikartik01/osmite-RnD)](LICENSE)

Open-source research and development projects from osmite. Each project lives
in its own top-level folder and is self-contained: its own build, docs, tests
and release tags.

## Projects

| Project | What it is | Status |
|---|---|---|
| [`otmux/`](otmux/) | A cross-platform terminal workspace (multiplexer) in Go: persistent tabs and panes, tmux-style keys plus mouse, built for remote control. Windows first, then Linux and macOS. | 1.0.0 |

## Download otmux

**Windows**, one line in PowerShell (installs for your user, no admin, and
adds `otmux` to your PATH):

```powershell
irm https://raw.githubusercontent.com/vamsikartik01/osmite-RnD/main/otmux/install.ps1 | iex
```

Or download the program directly; these links always give the newest version:

| Platform | Download |
|---|---|
| Windows (x64) | [otmux-windows-amd64.exe](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-windows-amd64.exe) |
| Windows (ARM) | [otmux-windows-arm64.exe](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-windows-arm64.exe) |
| macOS (Apple silicon) | [otmux-darwin-arm64](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-darwin-arm64) |
| macOS (Intel) | [otmux-darwin-amd64](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-darwin-amd64) |
| Linux (x64) | [otmux-linux-amd64](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-linux-amd64) |
| Linux (ARM) | [otmux-linux-arm64](https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest/otmux-linux-arm64) |

Every version, with release notes and checksums, is on the
[releases page](https://github.com/vamsikartik01/osmite-RnD/releases). See the
[otmux README](otmux/README.md) for how to use it.

## Repository layout

```
osmite-RnD/
├── README.md              this index
├── CONTRIBUTING.md        how to work in this repo
├── LICENSE                MIT, covers every project unless a project says otherwise
├── .github/workflows/     CI, one set of workflows per project (<project>-*.yml)
└── <project>/             one folder per project, fully self-contained
    ├── README.md
    ├── docs/              design notes and ADRs for that project
    └── ...                the project's own build files (go.mod, package.json, ...)
```

Conventions:

- **Nothing is shared between projects implicitly.** A project builds and tests
  from its own folder. If two projects need common code, it gets its own folder
  and is depended on explicitly.
- **CI is per project.** Workflows are named `<project>-ci.yml` /
  `<project>-release.yml` and filter on `paths: <project>/**`, so a change to
  one project doesn't run another's pipeline.
- **Releases are tagged per project:** `<project>/vX.Y.Z`, e.g. `otmux/v0.1.0`.
- **Design decisions are written down** as ADRs in `<project>/docs/adr/`.

## Contributing

Contributions are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md), and please
follow the [code of conduct](CODE_OF_CONDUCT.md). Report security problems
privately as described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
