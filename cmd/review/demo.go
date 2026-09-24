package main

import (
	"context"
	"quick-review-cli/internal/domain"
	"time"
)

func demoState() domain.State {
	now := time.Now()
	head := "a81cd92123aa"
	base := "bc110a219b1a"
	return domain.State{
		Snapshot: domain.Snapshot{PR: domain.PR{Owner: "example", Repo: "checkout", Number: 123}, Title: "Refresh authentication tokens before expiry", URL: "https://github.com/example/checkout/pull/123", State: "OPEN", HeadSHA: head, BaseSHA: base, BaseBranch: "main", Checks: []domain.Check{{Name: "Unit tests", State: "SUCCESS"}, {Name: "Integration tests", State: "IN_PROGRESS"}, {Name: "Lint", State: "SUCCESS"}}, Files: []domain.File{{Path: "auth/token.go", Additions: 32, Deletions: 8}, {Path: "auth/token_test.go", Additions: 68}}, Commits: []domain.Commit{{SHA: head, Title: "Refresh tokens ahead of expiry"}}},
		Phase:    "Demo · Reviewing", Connection: "Simulated · no network", ReviewedHead: head,
		Events: []domain.Event{
			{ID: 1, Time: now.Add(-3 * time.Minute), Source: "You", Kind: "message", Text: "Review this PR, especially token renewal."},
			{ID: 2, Time: now.Add(-2 * time.Minute), Source: "App", Kind: "checkout", Text: "Review started at " + head, SHA: head},
			{ID: 3, Time: now.Add(-time.Minute), Source: "Agents", Kind: "started", Text: "Security reviewer started", Detail: "Review token expiry, concurrent refresh, and session invalidation."},
			{ID: 4, Time: now, Source: "Codex", Kind: "message", Text: "I’m checking the refresh boundary and how concurrent requests share a token. The security and regression reviewers are examining separate paths."},
		},
		Agents:  []domain.Agent{{ID: "security", Name: "Security", Scope: "Token expiry and concurrent refresh", Status: "running"}, {ID: "regressions", Name: "Regressions", Scope: "Callers and error handling", Status: "running"}},
		Diff:    "diff --git a/auth/token.go b/auth/token.go\n--- a/auth/token.go\n+++ b/auth/token.go\n@@ -10,3 +10,4 @@\n- if token.Expired() {\n+ if token.ExpiresWithin(refreshWindow) {\n     return refresh(ctx)\n  }",
		Reports: []domain.Report{{HeadSHA: base, BaseSHA: base, Text: "# Previous review\n\nNo actionable findings.\n\n## Verification limits\nCode inspection only; tests were not run.", Stale: true, CreatedAt: now.Add(-time.Hour)}},
	}
}
func runDemo(ctx context.Context, updates chan domain.State, actions <-chan domain.Action) error {
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	return demoLoop(ctx, updates, actions, ticker.C)
}
func demoLoop(ctx context.Context, updates chan domain.State, actions <-chan domain.Action, ticks <-chan time.Time) error {
	defer close(updates)
	state := demoState()
	n := 0
	publish := func() {
		copyState := state
		copyState.Events = append([]domain.Event(nil), state.Events...)
		select {
		case updates <- copyState:
		default:
			select {
			case <-updates:
			default:
			}
			updates <- copyState
		}
	}
	add := func(source, kind, text string) {
		state.Events = append(state.Events, domain.Event{ID: len(state.Events) + 1, Time: time.Now(), Source: source, Kind: kind, Text: text, SHA: state.Snapshot.HeadSHA})
	}
	publish()
	for {
		select {
		case <-ctx.Done():
			return nil
		case a, ok := <-actions:
			if !ok {
				return nil
			}
			switch a.Kind {
			case "confirm-quit", "confirm-quit-remove":
				return nil
			case "quit":
				state.QuitRequested = true
			case "cancel-quit":
				state.QuitRequested = false
				state.ClosedPrompt = false
			case "message":
				add("You", "message", a.Text)
				add("Codex", "message", "This is a simulated session. Start review with a real PR URL to chat with your logged-in Codex agent.")
			case "pause":
				state.Paused = true
				state.Phase = "Demo · Paused"
			case "resume":
				state.Paused = false
				state.Phase = "Demo · Watching"
			case "refresh":
				add("App", "refresh", "Demo state refreshed")
			case "open":
				add("App", "demo", "Links are disabled in the demo")
			}
			publish()
		case <-ticks:
			if state.Paused {
				continue
			}
			n++
			switch n {
			case 1:
				state.Snapshot.Checks = append([]domain.Check(nil), state.Snapshot.Checks...)
				state.Snapshot.Checks[1].State = "SUCCESS"
				add("CI", "ci", "All checks passed for "+state.Snapshot.HeadSHA)
			case 2:
				state.Snapshot.HeadSHA = "b72fe10998cc"
				state.Snapshot.Checks = []domain.Check{{Name: "Unit tests", State: "QUEUED"}}
				add("GitHub", "push", "New push detected: a81cd92123aa → b72fe10998cc")
				add("App", "queued", "Latest revision queued; active reviewers keep their current checkout")
			case 3:
				state.ReviewedHead = state.Snapshot.HeadSHA
				add("App", "checkout", "Latest checkout ready; new review started")
			}
			publish()
		}
	}
}
