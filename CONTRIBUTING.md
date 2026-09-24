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
