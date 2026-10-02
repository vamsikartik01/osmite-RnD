# Contributing

Thanks for helping out. Each project has its own README with build and test
instructions; this file covers what is common to the whole repo.

## Workflow

1. Open an issue first for anything bigger than a small fix, so we can agree on
   the approach.
2. Branch from `main`, keep the change focused on one project where possible.
3. Make sure that project's tests pass locally (see its README).
4. Open a pull request. CI runs only for the projects you touched.

## Adding a new project

1. Create a top-level folder with a short, lowercase name.
2. Add a `README.md` (what it is, status, how to build and test) and a `docs/`
   folder.
3. Add `.github/workflows/<project>-ci.yml` filtered on `paths: ['<project>/**']`.
4. Add a row to the project table in the root [README](README.md).

## Commit messages

Prefix the subject with the project name: `otmux: add tab rename prompt`.
Use `repo:` for changes that touch the repository as a whole.

## Design decisions

Significant decisions (architecture, dependencies, protocols) go in
`<project>/docs/adr/NNNN-title.md`: the context, the decision, and what we gave
up. Short is fine.
