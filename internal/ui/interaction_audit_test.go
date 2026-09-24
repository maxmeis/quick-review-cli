package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"quick-review-cli/internal/domain"
)

// These tests exercise the coordinates a person actually sees after scrolling,
// resizing, and opening the quit prompt. They complement helper-level coverage.
func TestScrolledChecksClickOpensVisibleCheck(t *testing.T) {
	state := testState()
	state.Snapshot.Checks = make([]domain.Check, 18)
	for i := range state.Snapshot.Checks {
		state.Snapshot.Checks[i] = domain.Check{Name: fmt.Sprintf("check-%02d", i), State: "success", URL: fmt.Sprintf("https://checks/%d", i)}
	}
	var got domain.Action
	m := NewModel(state, func(a domain.Action) { got = a }).(model)
	m.width, m.height, m.active = 80, 10, checksTab
	m = m.scrollBy(5)
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) < 10 {
		t.Fatalf("short checks view: %q", view)
	}
	// At this scroll offset row 4 is the second visible check.
	if !strings.Contains(lines[4], "check-14") {
		t.Fatalf("expected scrolled view to show check-14 at row 4; got %q\n%s", lines[4], view)
	}
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 4})
	if got.Kind != "open" || got.Text != "https://checks/14" {
		t.Fatalf("click opened wrong check: %+v", got)
	}
}

func TestNarrowTerminalShowsActionableError(t *testing.T) {
	s := testState()
	s.Error = "GitHub refresh failed: connection refused"
	m := NewModel(s, nil).(model)
	m.width, m.height = 42, 12
	view := m.View()
	if !strings.Contains(view, "connection refused") {
		t.Fatalf("narrow PR view hides the actionable error: %q", view)
	}
}

func TestScrolledActivityClickSelectsVisibleEvent(t *testing.T) {
	state := testState()
	state.Events = make([]domain.Event, 20)
	for i := range state.Events {
		state.Events[i] = domain.Event{ID: i + 1, Source: "GitHub", Kind: "push", Text: fmt.Sprintf("event-%02d", i)}
	}
	m := NewModel(state, nil).(model)
	m.width, m.height, m.active = 80, 10, activityTab
	m = m.scrollBy(5)
	view := strings.Split(m.View(), "\n")
	if len(view) <= 4 {
		t.Fatalf("activity view too short: %q", view)
	}
	var visibleID int
	for i, event := range state.Events {
		if strings.Contains(view[4], fmt.Sprintf("event-%02d", i)) {
			visibleID = event.ID
			break
		}
	}
	if visibleID == 0 {
		t.Fatalf("could not identify visible event row 4: %q\n%s", view[4], strings.Join(view, "\n"))
	}
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 4})
	if got := m.selectedEventID(); got != visibleID {
		t.Fatalf("click selected event %d, but row 4 showed event %d", got, visibleID)
	}
}

func TestScrolledReportClickOpensCurrentReport(t *testing.T) {
	state := testState()
	state.Reports = []domain.Report{
		{Path: "/tmp/old.md", Text: strings.Repeat("old report body ", 30)},
		{Path: "/tmp/current.md", Text: strings.Repeat("current report body ", 30)},
	}
	var got domain.Action
	m := NewModel(state, func(a domain.Action) { got = a }).(model)
	m.width, m.height, m.active, m.reportIndex = 80, 10, reportTab, 1
	m = m.scrollBy(2)
	view := m.View()
	if !strings.Contains(view, "current report body") {
		t.Fatalf("selected report body missing after scroll: %s", view)
	}
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 4})
	if got.Kind != "open" || got.Text != "/tmp/current.md" {
		t.Fatalf("click opened wrong report: %+v", got)
	}
}

func TestQuitDialogMouseCoordinatesDoNotSelectUnderlyingRows(t *testing.T) {
	state := testState()
	state.Snapshot.Checks = []domain.Check{
		{Name: "first", State: "success", URL: "https://checks/first"},
		{Name: "second", State: "success", URL: "https://checks/second"},
	}
	var actions []domain.Action
	m := NewModel(state, func(a domain.Action) { actions = append(actions, a) }).(model)
	m.width, m.height, m.active, m.quitDialog = 80, 14, checksTab, true
	// The first check appears at terminal row 5 because the dialog occupies row 3.
	// Clicking visible content while the dialog is open should not open hidden rows.
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 5})
	for _, action := range actions {
		if action.Kind == "open" {
			t.Fatalf("click through quit dialog dispatched an action: %+v", action)
		}
	}
}

func TestNarrowQuitDialogMouseButtonCancelsPrompt(t *testing.T) {
	var got domain.Action
	m := NewModel(testState(), func(a domain.Action) { got = a }).(model)
	m.width, m.height, m.quitDialog = 28, 10, true
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 13, Y: 3})
	if got.Kind != "cancel-quit" || !m.suppressQuit {
		t.Fatalf("narrow [n] mouse target did not cancel: action=%+v suppress=%v", got, m.suppressQuit)
	}
}

func TestChatSeparatesMessagesFromDetailedActivity(t *testing.T) {
	s := testState()
	s.Questions = nil
	s.Events = []domain.Event{{Source: "Codex", Kind: "activity", Text: "thread/tokenUsage/updated"}, {Source: "Codex", Kind: "tool", Text: "git diff command output"}, {Source: "Codex", Kind: "message", Text: "A clear review explanation."}, {Source: "GitHub", Kind: "push", Text: "New push detected."}}
	m := NewModel(s, nil).(model)
	m = apply(m, tea.WindowSizeMsg{Width: 40, Height: 24})
	chat := m.View()
	if strings.Contains(chat, "tokenUsage") || strings.Contains(chat, "command output") || !strings.Contains(chat, "A clear review explanation.") || !strings.Contains(chat, "New push detected.") {
		t.Fatal(chat)
	}
	m.active = activityTab
	m.width = 120
	m.height = 32
	if !strings.Contains(m.View(), "thread/tokenUsage/updated") {
		t.Fatal("technical activity lost")
	}
}

func TestLauncherShowsCompleteURLPlaceholder(t *testing.T) {
	m := NewModel(domain.State{}, nil).(model)
	m = apply(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	if !strings.Contains(m.View(), "github.com/owner/repo/pull/123") {
		t.Fatal(m.View())
	}
}
