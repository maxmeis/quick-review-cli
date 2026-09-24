# TUI redesign and inspection — 2026-09-24

[Open the complete gallery](index.html). Individual captures: [Chat](chat.png), [Checks](checks.png), [Question](question.png), [Narrow](narrow.png), [Disconnected](disconnected.png), [Launcher](launcher.png), [Light theme](light.png).

These images render actual production `View()` output and its ANSI styles with a monospace font. They are terminal-output captures in a browser, not screenshots of a native terminal window. Terminal fonts and palettes can differ. `screens.json` contains the captured ANSI output for all six tabs and additional states.

The visual pass found and fixed hidden errors in narrow windows, mouse clicks passing through the quit dialog, a one-character URL placeholder, poor header contrast in light terminals, and noisy tabular chat. Chat now keeps conversational messages and meaningful watcher updates; detailed protocol and tool output remains in Activity. Review phase is visible in the header.

Interaction tests exercise actual rendered row coordinates after scrolling Checks, Activity, and Report; modal blocking; narrow errors; the launcher; and preservation of technical details in Activity. Existing size/Unicode stress tests and the strict coverage gate remain enabled.

Regenerate the ANSI fixtures:

```sh
QUICK_REVIEW_VISUAL_DIR=/tmp/quick-review-visual go test ./internal/ui -run TestExportVisualFixtures -count=1
```

Run all checks with `make verify`. The PNG captures were visually inspected; they are not an automated pixel-diff test.

## Redesigned workspace

The new default layout uses Bubble Tea for events, Bubbles for text inputs, and Lip Gloss for rounded panels, responsive columns, adaptive colors, and modal placement. The wide layout adds a live overview; the 80-column layout keeps one framed workspace. Very small terminals retain the compact renderer.

New captures: [Report](report.png), [Changes](changes.png), [Agents](agents.png), [Activity](activity.png), [Exit dialog](quit.png), [80-column workspace](compact.png).

Production View/Update tests cover panel hit coordinates, sidebar exclusion, modal button placement, light/no-color modes, all tabs, and dimension limits. Compact-renderer tests remain separate from the dashboard integration tests. A real PTY smoke test also exercised typing, a mouse tab click, and mouse-confirmed exit.
