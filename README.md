# Quick Review

Quick Review is a Go terminal app for reviewing GitHub pull requests with a live, read-only Codex review. It uses your existing logged-in GitHub CLI (`gh`) and Codex CLI (`codex`); it does not need a separate GitHub token or OpenAI API key setup.

The interface has six tabs: Chat, Changes, Checks, Agents, Report, and Activity. It supports keyboard and mouse navigation. Review checkouts live in a dedicated directory for each revision. Completed Markdown reports and session history are saved separately from those checkouts.

## Requirements

- Go 1.24 or newer
- GitHub CLI (`gh`), installed and authenticated with `gh auth login`
- Codex CLI (`codex`), installed and authenticated with `codex login`
- A terminal that supports Bubble Tea's alternate screen and mouse input

The Codex integration uses `codex app-server` over stdio. Version 0.151.0 has been tested with the live contract test. Regular tests and CI do not require GitHub or Codex authentication.

## Install and run

Build the CLI locally:

```sh
go build -o bin/review ./cmd/review
./bin/review
```

Install it into your Go binary directory:

```sh
go install ./cmd/review
review
```

Running without a URL opens the launcher. Paste a PR URL such as `https://github.com/OWNER/REPO/pull/123` and press Enter to start. You can also pass the URL on the command line:

```sh
./bin/review https://github.com/OWNER/REPO/pull/123
```

Try the interface without network access or model calls:

```sh
./bin/review --demo
```

## Options

```text
--data-dir DIR   Session storage root (default: user config/quick-review/sessions)
--resume DIR     Resume a saved session directory
--poll DURATION  GitHub polling interval (default: 30s, minimum: 5s)
--demo           Explore the interface with simulated events
```

For example, use a custom storage directory and a 60 second polling interval:

```sh
./bin/review --data-dir "$HOME/reviews" --poll 60s
```

To resume, pass the session directory shown in the Activity view or in the saved session state:

```sh
./bin/review --resume "/path/to/session-directory"
```

By default, sessions are stored under `os.UserConfigDir()/quick-review/sessions`. Each session contains its state, activity log, retained revision checkouts, and saved reports. You can remove a session directory when you no longer need its history or reports.

## Working in the interface

- Select tabs with `Alt+1` through `Alt+6`, `Tab`/`Shift+Tab` outside Chat, or a mouse click.
- In Chat, press Enter to send a message; use `Alt+Enter` to add a line. Press `F2` to focus question options, use the arrow keys to choose, and press Enter to answer. Press `a` for a freeform answer and Esc to leave question focus.
- Press `Ctrl+P` for commands: `refresh`, `pause`, `resume`, `report`, or `quit`.
- In Activity, press `/` or `f` to search, `s` to cycle source filters (All, You, Codex, Agents, App, GitHub, CI), and Enter or click an event to expand its details.
- Use `q` outside Chat or `Ctrl+C` to open the quit dialog. Choose `y` or Enter to keep checkouts and exit, `d` to remove clean checkouts and exit, or `n`, Esc, or `q` to keep watching.
- Use the mouse wheel, arrow keys, or Page Up/Page Down to scroll. Tab scroll positions are remembered, and new Activity entries show a badge when you have scrolled away from the latest events.

The Changes tab shows changed files and the diff. Checks and reports can be opened with Enter. Reports include the exact reviewed head and merge-base.

Quick Review polls GitHub at the configured interval. Head, base, CI, and PR state changes appear in Activity with their source and revision SHA. When the head or base changes, the current review stays on its existing checkout until its turn ends; Quick Review then prepares the latest revision and starts another review. Older reports are marked stale. The header shows both the latest head and the revision currently reviewed, and indicates when GitHub data is stale.

If `terminal-notifier` is installed, Quick Review also sends best-effort desktop notifications for completed reviews and PR revision, check, or state updates. The title includes a notification bell, and the subtitle identifies the repository and PR number. A notifier process is stopped after three seconds; missing or failed notifications do not interrupt the review.

When quitting, `d` removes the session's checkouts only after every checkout passes `git status --porcelain --untracked-files=all`. If a checkout is dirty, contains untracked files, is a symlink, or cannot be checked, Quick Review preserves the whole checkout directory and reports the error. Session state, Activity history, and saved reports are retained either way.

## Review safety

Quick Review fetches PR data through `gh` and prepares a dedicated checkout for the reviewed revision. Codex receives read-only sandbox access to that checkout. The review instructions prohibit modifying files, running project scripts or tests, installing packages, pushing commits, or posting GitHub comments unless the user explicitly asks for that action. PR descriptions, diffs, and watcher updates are treated as data, not instructions. The app does not post review comments.

## Development and verification

```sh
make build
make test
make coverage
make verify
```

`make coverage` runs the Go tests with the race detector and an atomic coverage profile, then fails if any executable statement block was not exercised. It writes `coverage.out` and prints per-function coverage.

The live Codex contract test is opt-in and makes one small model turn. It is not part of CI:

```sh
QUICK_REVIEW_LIVE_CODEX=1 go test ./internal/codex -run TestLiveCodexContract
```

Run it only with Codex installed and logged in. It checks the local app-server contract and does not use GitHub.

See [Architecture and verification](docs/architecture.md) for the session lifecycle, watcher behavior, storage, and test boundaries.
