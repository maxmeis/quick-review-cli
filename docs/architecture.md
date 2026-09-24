# Architecture and verification

## Session lifecycle

1. Parse a GitHub PR URL and check the existing `gh` login.
2. Read a snapshot through `gh pr view`: title, description, state, exact head/base, files, commits, and checks.
3. Prepare a dedicated checkout for that revision. Fetch PR and base refs, verify the fetched SHAs against the snapshot, and refuse to overwrite local changes.
4. Launch the installed `codex app-server` over JSONL stdio. Verify its account, then start or resume a thread using the existing Codex login and configuration.
5. Send the exact commit range and review playbook. The lead reviewer assigns bounded subagent scopes when supported, validates their findings, and returns Markdown.
6. Persist the report with its head and merge-base. Continue polling and accepting user messages in the same thread.

The controller owns mutable session state in one event loop. Network and checkout work run as background jobs; the Bubble Tea interface consumes immutable state snapshots and sends typed actions. The UI never launches Git or model commands directly.

## New revisions and watcher messages

A push or base update marks the live report stale and queues a review of the latest observed revision. Existing reviewers retain their checkout until their turn ends. Intermediate pushes are coalesced. The next turn gets a fresh checkout and explicit commit identifiers.

CI and PR-state transitions appear in Chat and Activity. During an active turn, the controller submits observed facts through `turn/steer`; if submission fails, it retains them for a later turn. User messages follow the same queue fallback. Queued messages and watcher context are saved for resume.

Merge and closure open an exit prompt. Keeping the session open continues watching. Exit preserves reports and activity. Optional checkout removal first checks all checkout directories; local changes, unknown files, symlinks, or Git failures prevent removal.

## Protocol and permissions

The app uses the installed CLI's app-server interface, without embedding an API key or copying credentials. The thread starts with a read-only sandbox and on-request approval policy. Approval and input requests appear in the interface; unsupported requests are explicitly rejected. Repository content is review evidence and cannot authorize writes or override the review playbook.

Subagents belong to the Codex thread. The Agents tab reflects the collaboration events the installed server exposes. The lead's playbook requires waiting for or stopping reviewers before reporting; application shutdown also interrupts tracked turns and closes the server process group on Unix.

## Stored artifacts

Each session directory contains a JSON state snapshot, JSONL activity history, revision checkouts, and one canonical Markdown report at `reports/report.md`. Report updates publish through a synced temporary file and rename. Private session files use restrictive permissions. Resuming restores the thread and queues, then refreshes GitHub state before reviewing.

## Test boundaries

`make verify` builds, runs `go vet`, and executes all Go tests with the race detector and statement coverage instrumentation. Its gate examines every executable block in the generated profile; rounded percentage output is insufficient. Domain structs have no executable statements. Platform-specific source is measured on the operating system that compiles it; CI runs on Linux and macOS.

Tests include fake GitHub commands and Codex protocol messages, real local Git fixtures, filesystem failures, state transitions, queue behavior, report persistence, and terminal interaction/rendering regressions. The opt-in live Codex contract test checks initialization, authentication, thread creation, and a streamed model turn using the installed CLI.

100% statement coverage means the instrumented statements ran. It does not exhaust every input, terminal emulator, network condition, model decision, or future CLI version. Live GitHub PR reviews require an actual PR and authenticated services; ordinary CI remains deterministic and offline from those services.
