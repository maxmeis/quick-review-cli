package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"quick-review-cli/internal/domain"
)

// Opt-in captures use the production View and Update methods, not a UI mock.
func TestExportVisualFixtures(t *testing.T) {
	dir := os.Getenv("QUICK_REVIEW_VISUAL_DIR")
	if dir == "" {
		t.Skip("set QUICK_REVIEW_VISUAL_DIR to export terminal views")
	}
	t.Setenv("NO_COLOR", "")
	oldDark := lipgloss.HasDarkBackground()
	lipgloss.SetHasDarkBackground(true)
	defer lipgloss.SetHasDarkBackground(oldDark)
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	s := testState()
	s.Questions = nil
	s.Phase = "Reviewing"
	s.Connection = "Connected"
	s.ReviewedHead = s.Snapshot.HeadSHA
	s.Snapshot.Title = "Refresh authentication tokens before expiry"
	s.Snapshot.Checks = []domain.Check{{Name: "Unit tests", State: "SUCCESS"}, {Name: "Integration tests", State: "IN_PROGRESS"}, {Name: "Lint", State: "SUCCESS"}}
	s.Events = []domain.Event{{ID: 1, Time: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), Source: "You", Kind: "message", Text: "Review token renewal and concurrent requests."}, {ID: 2, Source: "App", Kind: "checkout", Text: "Checkout ready at 12345678"}, {ID: 3, Source: "Codex", Kind: "message", Text: "I am checking token expiry and callers. Two reviewers are inspecting concurrency and test coverage."}}
	s.Reports[0].Text = "# Review summary\n\n## P1 — Avoid concurrent token refresh\n\nLocation: auth/token.go:42\nTwo requests can refresh the same expired token. Guard refresh with a shared lock.\n\n## Style and maintainability\nNo actionable suggestions.\n\nTests were not run."
	captures := map[string]string{}
	for i, name := range tabNames {
		m := NewModel(s, nil).(model)
		m = apply(m, tea.WindowSizeMsg{Width: 120, Height: 34})
		m.active = tab(i)
		captures[name] = m.View()
	}
	for _, name := range []string{"Launcher", "Question", "Quit", "Narrow", "Compact", "Disconnected"} {
		state := s
		if name == "Launcher" {
			state = domain.State{}
		}
		m := NewModel(state, nil).(model)
		m = apply(m, tea.WindowSizeMsg{Width: 120, Height: 34})
		switch name {
		case "Question":
			m.state.Questions = testState().Questions
			m.questionFocus = true
		case "Quit":
			m.quitDialog = true
		case "Compact":
			m = apply(m, tea.WindowSizeMsg{Width: 80, Height: 28})
		case "Narrow":
			m = apply(m, tea.WindowSizeMsg{Width: 40, Height: 20})
		case "Disconnected":
			m.state.Connection = "Codex disconnected"
			m.state.Error = "Codex disconnected. Use /resume to reconnect."
			m = apply(m, tea.WindowSizeMsg{Width: 40, Height: 20})
		}
		captures[name] = m.View()
	}
	lipgloss.SetHasDarkBackground(false)
	light := NewModel(s, nil).(model)
	light = apply(light, tea.WindowSizeMsg{Width: 120, Height: 34})
	captures["Light"] = light.View()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(captures, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "screens.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
