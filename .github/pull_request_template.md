## Summary

Describe the change and why it is needed. Use a Conventional Commit PR title
(`fix:`, `feat:`, `test:`, etc.); it becomes the squash commit title.

## Validation

- [ ] `make verify` passes (100% Go statement coverage, race tests, mocked E2E).
- [ ] `pnpm test` passes (hooks, coverage rejection, release rules).
- [ ] Documentation and regression tests match the change.

## Compatibility

Describe any migration needed. For a breaking change, put `!` in the PR title
(e.g. `feat!: replace session format`) so release automation makes a major bump.
