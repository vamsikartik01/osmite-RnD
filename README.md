# osmite R&D

Open-source research and development projects from osmite. Each project lives
in its own top-level folder and is self-contained: its own build, docs, tests
and release tags.

## Projects

| Project | What it is | Status |
|---|---|---|
| [`otmux/`](otmux/) | A cross-platform terminal workspace (multiplexer) in Go: persistent tabs and panes, tmux-style keys plus mouse, built for remote control. Windows first, then Linux and macOS. | 1.0.0 |

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

## License

[MIT](LICENSE)
