# Live battle test — 2026-09-24

Fixture: private [`maxmeis/quick-review-example`](https://github.com/maxmeis/quick-review-example), [PR #1](https://github.com/maxmeis/quick-review-example/pull/1).

The real TUI ran in a PTY using existing GitHub CLI and Codex CLI authentication, with a five-second poll interval. No API keys or mocked service responses were used in this live run. GitHub Actions ran the fixture's Go tests. The model reviewers remained read-only and did not execute the fixture tests.

## Scenarios observed

- An intentional `done / total * 100` regression failed CI and produced a P1 finding for integer truncation.
- Two real review subagents inspected arithmetic correctness and test coverage; the lead validated the finding.
- Two pushes during an active review updated the watcher and queued the latest revision. The original checkout retained its original HEAD and remained clean; the next review used a separate clean checkout.
- The corrected revision passed CI and received a complete “No actionable findings” report.
- Exiting and resuming restored the Codex thread and prior reports. A push while stopped caused the retained reports to become stale and a new review to start.
- Closing the PR produced the exit prompt. Choosing to keep watching and reopening the PR allowed the review to continue.
- The corrected report accumulator preserved both the complete P1 report and a later watcher acknowledgement in the same turn. See [the actual saved report](example-review.md).

- Merging the corrected fixture produced the merge exit prompt.
- A deliberately untracked file in a retired checkout prevented removal of every checkout. After removing only that fixture-owned file, cleanup removed all four checkouts and retained all four Markdown reports. The TUI exited successfully.
- Linux and macOS GitHub Actions passed for the fixes, including the strict coverage gate.

## Failures found and fixed

1. **Saved report overwritten by a watcher acknowledgement.** Codex emitted the full review, then additional final messages in the same steered turn. Only the last message was saved. The controller now accumulates final messages. A regression test and a second live run both verified that findings survive.
2. **Composer hidden in short terminals.** Long questions could consume the visible area. The composer now adapts to height and question rows respect its reserved space.
3. **Long unbroken text exceeded the terminal width.** Wrapped output now clips remaining overlong runs to the viewport.
4. **Unicode truncation split a UTF-8 character.** Short labels preserve character boundaries.

UI stress tests cover 21 widths, 14 heights, all six tabs, long Unicode content, and out-of-bounds mouse coordinates. `make verify` includes the race detector, vet, build, and a strict per-block 100% Go statement-coverage gate.

## Limits

`terminal-notifier` was not installed, so native desktop notification delivery could not be observed. Its command arguments, missing-package behavior, and timeout are covered by tests. Coverage does not guarantee every possible model response, terminal emulator, network outage, or future app-server protocol version.
