package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"quick-review-cli/internal/domain"
	"strings"
	"testing"
)

func TestPreserveScrollWithoutInitializedTabMap(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.follow = false
	m.scrollByTab = nil
	s := testState()
	s.Events = append(s.Events, domain.Event{Text: "new"})
	m = apply(m, stateMsg(s))
	if m.scrollByTab[m.active] != 1 || m.newActivity != 1 {
		t.Fatalf("scroll not retained: %+v", m.scrollByTab)
	}
	m.scrollByTab = nil
	m = m.scrollBy(-2)
	if m.scrollByTab[m.active] != 3 {
		t.Fatal("scrollBy did not initialize map")
	}
	m.scrollByTab = nil
	m.resetTabScroll()
	if m.scrollByTab[m.active] != 0 || !m.follow {
		t.Fatal("reset failed")
	}
	m.scrollByTab = nil
	m.setTab(checksTab)
	if m.active != checksTab {
		t.Fatal("tab failed")
	}
}

func TestSafeCommitTitleAndEmptySelection(t *testing.T) {
	s := testState()
	s.Snapshot.Commits = []domain.Commit{{Title: "hello\x1b[31m world"}}
	clean := safeState(s)
	if clean.Snapshot.Commits[0].Title != "hello world" {
		t.Fatal(clean.Snapshot.Commits)
	}
	m := NewModel(domain.State{}, nil).(model)
	if m.selectedEventID() != -1 {
		t.Fatal("empty event selection")
	}
}

func TestMouseOutsideActivityRowsAndChatEscape(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.width = 100
	m.height = 32
	m.active = activityTab
	m = apply(m, tea.MouseMsg{X: 3, Y: 100, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.selectedEvent != -1 {
		t.Fatal("outside click selected event")
	}
	m.active = chatTab
	m.questionFocus = true
	m = apply(m, key("esc"))
	if m.questionFocus {
		t.Fatal("escape retained question focus")
	}
	m = apply(m, key("right"))
	if m.active != chatTab {
		t.Fatal("right changed chat tab")
	}
}

func TestCheckTimingRowsAndWrapping(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.state.Snapshot.Checks = []domain.Check{{Name: "done", CompletedAt: "now"}, {Name: "running", StartedAt: "earlier"}}
	rows, mapping := m.checkRows()
	if !strings.Contains(strings.Join(rows, "\n"), "completed now") || !strings.Contains(strings.Join(rows, "\n"), "running since earlier") || len(mapping) != len(rows) {
		t.Fatal(rows, mapping)
	}
	rows = wrapRows([]string{"ab"}, 0)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
}

func TestCompactQuitMouseActions(t *testing.T) {
	for _, width := range []int{24, 100} {
		for _, tc := range []struct{ token, kind string }{{"y", "confirm-quit"}, {"d", "confirm-quit-remove"}, {"n", "cancel-quit"}} {
			var got domain.Action
			m := NewModel(testState(), func(a domain.Action) { got = a }).(model)
			m.width = width
			m.height = 32
			m.quitDialog = true
			token := "[" + tc.token + "]"
			if width < 36 {
				token = tc.token + " "
			}
			x := strings.Index(quitLine(width), token)
			m = apply(m, tea.MouseMsg{X: x, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if got.Kind != tc.kind || m.quitDialog {
				t.Fatalf("width%d token%s: %+v", width, token, got)
			}
		}
	}
}

func TestFreeformAnswerRenderingAndOptionNavigation(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.width = 100
	m.height = 32
	m.questionFocus = true
	m.questionChoice = 1
	m.composer.SetValue("")
	m = apply(m, key("up"))
	if m.questionChoice != 0 {
		t.Fatal("up did not select previous option")
	}
	m.answerMode = true
	m.answer.SetValue("Inspect retries")
	if !strings.Contains(m.View(), "Inspect retries") {
		t.Fatal("freeform answer missing")
	}
	if m.visibleEventIndex(1) != 1 {
		t.Fatal("visible event mapping lost")
	}
}

func TestColorTabsRetainAllLabels(t *testing.T) {
	got := safeText(renderTabs(100, checksTab, false))
	for _, name := range tabNames {
		if !strings.Contains(got, name) {
			t.Fatalf("missing tab %s: %s", name, got)
		}
	}
}

func TestLauncherShowsConnectingStatus(t *testing.T) {
	m := NewModel(domain.State{}, nil).(model)
	m = apply(m, tea.WindowSizeMsg{Width: 100, Height: 32})
	m.launcher.SetValue("https://github.com/acme/widget/pull/42")
	m = apply(m, key("enter"))
	if !strings.Contains(m.View(), "Connecting to the pull request") {
		t.Fatal("connection progress missing")
	}
}
