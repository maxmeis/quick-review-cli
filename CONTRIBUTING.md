# Contributing

## Requirements

- Go 1.26.5
- Node.js 24.10 or newer
- pnpm 12.4.2

Run `pnpm install` after cloning to install the Git hooks. Commit messages are
checked with Conventional Commits, and every push runs `make verify` locally.
CI applies the same checks to pull requests.

## Commit messages and releases

Use the Conventional Commits format: `type(scope): summary`. Keep the summary
short and imperative. Common types include `feat`, `fix`, `docs`, `refactor`,
`test`, and `chore`.

The release process uses commit types to determine the next version:

- `fix:` produces a patch release.
- `feat:` produces a minor release.
- A breaking change produces a major release. Mark it with `!` before the
  colon, such as `feat!: change the output format`, or add a `BREAKING CHANGE:`
  footer.

Reverts should use the `revert:` type. Validate the commit message rules with
`node --test scripts/test-commitlint.mjs`.

## Before pushing

Run `make verify`. It builds the CLI, runs `go vet`, and checks that every
executable Go statement is covered by the test suite.

## Release pipeline

After the required Linux and macOS checks and commit checks pass on `main`,
semantic-release publishes a GitHub release for releasable changes. The first
release is `v1.0.0`; later versions follow the rules above. `perf:` also produces
a patch; documentation, tests, CI, and chores alone do not publish a release.
Release notes, four archives (macOS/Linux × amd64/arm64), and SHA-256 checksums
are generated automatically. `package.json` is private development tooling;
nothing is published to npm. GitHub Actions uses its built-in token.

Commit validation starts after `fc3a39f`, preserving earlier project history.
PR titles are checked too, because squash merge titles become commit messages.
Use squash merges with a Conventional Commit title. Hooks are a local aid;
GitHub's required checks enforce the policy even when hooks are skipped.

Run `pnpm test` for commit-hook and release-rule tests, and `make dist` to build
all release archives locally. Local hooks never publish a release.

## Offline end-to-end tests

`make e2e` builds the real CLI with the race detector, launches it in a
pseudo-terminal, and drives its launcher, keyboard, mouse, and dialogs. Mock
`gh` and `codex` executables provide deterministic GitHub snapshots and Codex
JSONL events. Git cloning/fetching, session persistence, and terminal rendering
remain real. Notification and file-opening commands are intercepted.

The suite covers a review with agents and questions, CI updates, watcher
steering, a new push, the single report and Mermaid rendering, pause/resume,
GitHub and Codex failures, saved-session resume, closed/merged prompts, and
checkout cleanup with protection for local edits. It runs without GitHub or
Codex credentials and makes no model calls. It validates our integration
against scripted protocol behavior, not the availability or compatibility of
live services. The opt-in live Codex contract test remains separate.

`make verify` includes these tests, so Husky's pre-push hook and the required
Linux/macOS CI jobs enforce both 100% Go statement coverage and the E2E suite.
Set `E2E_ARTIFACT_DIR=/tmp/review-e2e` to save terminal transcripts. CI uploads
those transcripts on failure. Run `make e2e` repeatedly when changing timing or
watcher behavior; tests use condition-based waits with bounded timeouts.
