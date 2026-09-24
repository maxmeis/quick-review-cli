package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	charmansi "github.com/charmbracelet/x/ansi"
	"quick-review-cli/internal/domain"
	"strings"
	"testing"
)

func TestAdversarialDimensionsAndUnicodeNeverOverflowRows(t *testing.T) {
	widths := []int{0, 1, 2, 19, 20, 21, 23, 24, 35, 36, 37, 49, 50, 51, 81, 82, 83, 179, 180, 181, 512}
	heights := []int{0, 1, 2, 5, 6, 7, 8, 12, 13, 14, 15, 24, 32, 100}
	s := testState()
	s.Snapshot.Title = "修复 widget 👩🏽‍💻 é"
	s.Snapshot.PR.Owner = "例子"
	s.DraftReply = strings.Repeat("🙂界é", 30)
	s.Events = append(s.Events, domain.Event{ID: 3, Source: "Agent🧪", Kind: "review", Text: strings.Repeat("長い説明🙂", 30)})
	s.Questions[0].Title = strings.Repeat("選択🙂", 30)
	s.Questions[0].Prompt = strings.Repeat("説明 é 👩🏽‍💻 ", 30)
	s.Questions[0].Options = []string{strings.Repeat("yes🙂", 30), "no"}
	for _, w := range widths {
		for _, h := range heights {
			m := NewModel(s, nil).(model)
			m = apply(m, tea.WindowSizeMsg{Width: w, Height: h})
			for _, page := range []tab{chatTab, changesTab, checksTab, agentsTab, reportTab, activityTab} {
				m.active = page
				v := m.View()
				if got, want := len(strings.Split(v, "\n")), max(6, h); got > want {
					t.Fatalf("width=%d height=%d page=%d produced %d rows, max %d", w, h, page, got, want)
				}
				for i, line := range strings.Split(v, "\n") {
					if cells := charmansi.StringWidth(line); cells > effectiveWidth(w) {
						t.Fatalf("width=%d height=%d page=%d row=%d uses %d cells: %q", w, h, page, i, cells, line)
					}
				}
			}
		}
	}
}

func TestShortNDoesNotSplitUnicode(t *testing.T) {
	got := shortN("界🙂x", 4)
	if !strings.HasPrefix(got, "界") || strings.ToValidUTF8(got, "�") != got {
		t.Fatalf("invalid unicode truncation: %q", got)
	}
	if shortN("a", 0) != "" || shortN("界", 3) != "界" {
		t.Fatal("shortN did not handle zero width and an exact UTF-8 byte boundary")
	}
}

func TestMinimumChatHeightKeepsComposerVisible(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.width, m.height = 24, 6
	view := m.View()
	if !strings.Contains(view, "Write a reply") {
		t.Fatalf("composer is inaccessible in the minimum supported terminal: %q", view)
	}
}

func TestAdversarialMouseCoordinatesStayBounded(t *testing.T) {
	for _, pane := range []tab{chatTab, changesTab, checksTab, agentsTab, reportTab, activityTab} {
		m := NewModel(testState(), nil).(model)
		m.width, m.height, m.active = 37, 8, pane
		for _, x := range []int{-100, -1, 0, 36, 37, 100000} {
			for _, y := range []int{-100, -1, 0, 2, 3, 4, 7, 8, 100000} {
				m = apply(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				m = apply(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
				if m.selectedEvent < -1 || m.selectedFile < 0 || m.reportIndex < 0 {
					t.Fatalf("pane=%d mouse=(%d,%d) produced invalid selection", pane, x, y)
				}
				if viewRows := len(strings.Split(m.View(), "\n")); viewRows > m.height {
					t.Fatalf("pane=%d mouse=(%d,%d) produced %d rows", pane, x, y, viewRows)
				}
			}
		}
	}
}

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
