package ui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"io"
	"quick-review-cli/internal/domain"
	"strings"
	"testing"
	"time"
)

func testState() domain.State {
	return domain.State{
		Snapshot:     domain.Snapshot{PR: domain.PR{Owner: "acme", Repo: "widget", Number: 42}, Title: "Fix the widget", State: "open", HeadSHA: "1234567890abcdef", Checks: []domain.Check{{Name: "unit", State: "success", URL: "https://checks/1"}}, Files: []domain.File{{Path: "main.go", Additions: 4, Deletions: 2}}},
		ReviewedHead: "abcdef0123456789", Connection: "watching", Diff: "@@ -1 +1 @@\n-old\n+new",
		Events:    []domain.Event{{ID: 1, Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Source: "Codex", Kind: "review", Text: "Found a bug", Detail: "line 4", SHA: "1234567890"}, {ID: 2, Source: "GitHub", Kind: "check", Text: "checks passed"}},
		Agents:    []domain.Agent{{ID: "a1", Name: "Scout", Scope: "main.go", Status: "done", Detail: "No risks"}},
		Reports:   []domain.Report{{Path: "/tmp/report.md", HeadSHA: "12345678", BaseSHA: "87654321", Text: "Looks good", Stale: true}, {Path: "/tmp/report2.md", HeadSHA: "12345678", Text: "Current report", CreatedAt: time.Now()}},
		Questions: []domain.Question{{ID: "q1", Title: "Review focus", Prompt: "What should I prioritize?", Options: []string{"Correctness", "Performance"}}},
	}
}
func apply(m model, msg tea.Msg) model { updated, _ := m.update(msg); return updated.(model) }
func key(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	if s == "tab" {
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	if s == "shift+tab" {
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	}
	if s == "ctrl+p" {
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	}
	if s == "ctrl+c" {
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	if s == "up" {
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	if s == "down" {
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	if s == "left" {
		return tea.KeyMsg{Type: tea.KeyLeft}
	}
	if s == "right" {
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	if s == "pgup" {
		return tea.KeyMsg{Type: tea.KeyPgUp}
	}
	if s == "pgdown" {
		return tea.KeyMsg{Type: tea.KeyPgDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
func typeText(m model, text string) model {
	for _, r := range text {
		m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestViewTabsAndResize(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m = apply(m, tea.WindowSizeMsg{Width: 100, Height: 28})
	for i := 0; i < 6; i++ {
		m.active = tab(i)
		v := m.compactView()
		if !strings.Contains(v, tabNames[i]) {
			t.Fatalf("missing tab %s: %s", tabNames[i], v)
		}
		if !strings.Contains(v, "acme/widget #42") || !strings.Contains(v, "OPEN") || !strings.Contains(v, "reviewed abcdef01") {
			t.Fatalf("header missing review details: %s", v)
		}
	}
	m = apply(m, tea.WindowSizeMsg{Width: 24, Height: 8})
	if got := m.compactView(); !strings.Contains(got, "#42") {
		t.Fatal("narrow render omitted PR header")
	}
}
func TestTabsAndMouse(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.width = 100
	m.height = 20
	m.active = changesTab
	m = apply(m, key("tab"))
	if m.active != checksTab {
		t.Fatal(m.active)
	}
	m = apply(m, key("shift+tab"))
	if m.active != changesTab {
		t.Fatal(m.active)
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'5'}})
	if m.active != reportTab {
		t.Fatalf("alt tab: %v", m.active)
	}
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 65, Y: 2})
	if m.active != activityTab {
		t.Fatalf("clicked tab: %v", m.active)
	}
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: 4})
	if !m.eventDetail {
		t.Fatal("event click did not open details")
	}
	m.active = activityTab
	_ = m.compactView()
}
func TestChatQuestionAndDraft(t *testing.T) {
	actions := []domain.Action{}
	m := NewModel(testState(), func(a domain.Action) { actions = append(actions, a) }).(model)
	for _, r := range []rune("hello jk") {
		m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = apply(m, key("enter"))
	if len(actions) != 1 || actions[0].Kind != "message" || actions[0].Text != "hello jk" {
		t.Fatalf("message dispatch: %+v", actions)
	}
	m.composer.SetValue("")
	m = apply(m, key("f2"))
	m = apply(m, key("down"))
	m = apply(m, key("enter"))
	if actions[len(actions)-1].Kind != "answer" || actions[len(actions)-1].Text != "Performance" || actions[len(actions)-1].ID != "q1" {
		t.Fatalf("answer dispatch: %+v", actions)
	}
	m = apply(m, key("f2"))
	m = apply(m, key("a"))
	m.answer.SetValue("focus on race conditions")
	m = apply(m, key("enter"))
	if actions[len(actions)-1].Text != "focus on race conditions" {
		t.Fatal(actions)
	}
	m.composer.SetValue("keep draft")
	m.active = changesTab
	m = apply(m, key("tab"))
	if m.active != checksTab || m.composer.Value() != "keep draft" {
		t.Fatal("draft did not survive tab switch")
	}
	s := testState()
	s.DraftReply = "controller draft"
	m = apply(m, stateMsg(s))
	if m.composer.Value() != "keep draft" {
		t.Fatal("streaming text overwrote the local composer draft")
	}
	m.setTab(chatTab)
	m.height = 30
	m.width = 100
	if !strings.Contains(m.compactView(), "Codex · streaming") || !strings.Contains(m.compactView(), "controller draft") {
		t.Fatalf("streaming assistant text was not rendered: %q", m.compactView())
	}
}
func TestLauncherAndCommands(t *testing.T) {
	actions := []domain.Action{}
	m := NewModel(domain.State{}, func(a domain.Action) { actions = append(actions, a) }).(model)
	m = typeText(m, "https://github.com/acme/widget/pull/7")
	if m.launcher.Value() == "" {
		t.Fatal("focused launcher did not accept keystrokes")
	}
	m = apply(m, key("enter"))
	if actions[0].Kind != "start" || !m.launcherMode || !m.launching {
		t.Fatal(actions)
	}
	state := testState()
	m = apply(m, stateMsg(state))
	m = apply(m, key("ctrl+p"))
	m.command.SetValue("pause")
	m = apply(m, key("enter"))
	if actions[len(actions)-1].Kind != "pause" {
		t.Fatal(actions)
	}
	m.runCommand("refresh")
	m.runCommand("resume")
	m.runCommand("report")
	if len(actions) < 5 {
		t.Fatal(actions)
	}
	m.active = changesTab
	m.state.Paused = true
	m = apply(m, key("p"))
	if actions[len(actions)-1].Kind != "resume" {
		t.Fatal(actions)
	}
	m.state.Paused = false
	m = apply(m, key("r"))
	if actions[len(actions)-1].Kind != "refresh" {
		t.Fatal(actions)
	}
}
func TestActivityFilteringScrollAndDetails(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.active = activityTab
	m = apply(m, key("/"))
	m.search.SetValue("bug")
	m = apply(m, key("enter"))
	if len(m.filteredEvents()) != 1 || m.filteredEvents()[0].ID != 1 {
		t.Fatal(m.filteredEvents())
	}
	m = apply(m, key("s"))
	if len(m.filteredEvents()) != 0 {
		t.Fatalf("source filter did not apply: %+v", m.filteredEvents())
	}
	m = apply(m, key("esc"))
	if m.filter != "" || m.sourceFilter != "" {
		t.Fatal("escape did not clear filters")
	}
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if m.scroll == 0 {
		t.Fatal("wheel did not scroll back")
	}
	updatedState := m.state
	updatedState.Events = append(updatedState.Events, domain.Event{ID: 3, Source: "You", Text: "new"})
	m = apply(m, stateMsg(updatedState))
	if m.newActivity != 1 {
		t.Fatalf("new activity badge: %d", m.newActivity)
	}
	m = apply(m, key("pgdown"))
	m = apply(m, key("pgdown"))
	m = apply(m, key("pgdown"))
	m = apply(m, key("pgdown"))
	if m.newActivity != 0 {
		t.Fatal("scroll to latest did not clear badge")
	}
}
func TestChecksReportsQuitAndContext(t *testing.T) {
	actions := []domain.Action{}
	m := NewModel(testState(), func(a domain.Action) { actions = append(actions, a) }).(model)
	m.active = checksTab
	m = apply(m, key("enter"))
	if actions[len(actions)-1].Kind != "open" || actions[len(actions)-1].Text != "https://checks/1" {
		t.Fatal(actions)
	}
	m.active = reportTab
	m = apply(m, key("left"))
	if m.reportIndex != 0 {
		t.Fatal(m.reportIndex)
	}
	m = apply(m, key("enter"))
	if actions[len(actions)-1].Text != "/tmp/report.md" {
		t.Fatal(actions)
	}
	m = apply(m, key("ctrl+c"))
	m = apply(m, key("n"))
	if actions[len(actions)-1].Kind != "cancel-quit" || m.quitDialog {
		t.Fatal(actions)
	}
	s := m.state
	s.ClosedPrompt = true
	m = apply(m, stateMsg(s))
	if !m.quitDialog {
		t.Fatal("closed session confirmation missing")
	}
	m = apply(m, key("d"))
	if actions[len(actions)-1].Kind != "confirm-quit-remove" {
		t.Fatal(actions)
	}
	updated, _ := m.Update(contextDoneMsg{})
	m = updated.(model)
	if m.active != reportTab {
		t.Fatal(m.active)
	}
}
func TestCiAndEventHelpers(t *testing.T) {
	for _, tc := range []struct {
		checks []domain.Check
		want   string
	}{{nil, "pending"}, {[]domain.Check{{State: "in_progress"}}, "running"}, {[]domain.Check{{State: "success"}}, "passing"}, {[]domain.Check{{State: "failure"}, {State: "queued"}}, "failing"}} {
		if got := ciSummary(tc.checks); got != tc.want {
			t.Errorf("ciSummary=%q, want %q", got, tc.want)
		}
	}
	if short("123456789") != "12345678" || short("short") != "short" || first("", "x") != "x" || first("a", "x") != "a" {
		t.Fatal("string helpers")
	}
	if checkIcon("running") == "" || checkIcon("unknown") == "" || tabAt(0, 0) != -1 || tabAt(0, 100) != 0 {
		t.Fatal("display helper")
	}
	m := NewModel(testState(), nil).(model)
	if !strings.Contains(formatEvent(m.state.Events[0]), "Codex") {
		t.Fatal("event source missing")
	}
}

func TestInitializationAndModalEditing(t *testing.T) {
	m := NewModel(domain.State{}, nil).(model)
	if !m.launcherMode {
		t.Fatal("launcher should appear without a PR")
	}
	initCmd := m.Init()
	if initCmd == nil {
		t.Fatal("textarea blink command missing")
	}
	m = apply(m, tea.WindowSizeMsg{Width: 42, Height: 12})
	if m.width != 42 || m.height != 12 {
		t.Fatal("resize was not retained")
	}
	m.launcher.Focus()
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("url")})
	if m.launcher.Value() == "" {
		t.Fatal("launcher input did not accept text")
	}
	m = apply(m, key("esc"))
	if !m.launcherMode {
		t.Fatal("escape should leave launcher view active")
	}
	s := testState()
	m = apply(m, stateMsg(s))
	if m.launcherMode {
		t.Fatal("PR update should enter review")
	}
	m.active = activityTab
	m = apply(m, key("/"))
	if !m.searchMode {
		t.Fatal("activity search did not open")
	}
	m.search.SetValue("assistant")
	m = apply(m, key("esc"))
	if m.searchMode {
		t.Fatal("search escape failed")
	}
	m = apply(m, key("f"))
	m.search.SetValue("assistant")
	m = apply(m, key("enter"))
	if m.filter != "assistant" {
		t.Fatal("source search not applied")
	}
	m.active = chatTab
	m.answerMode = true
	m = apply(m, key("esc"))
	if m.answerMode {
		t.Fatal("answer escape failed")
	}
	m.commandMode = true
	m = apply(m, key("esc"))
	if m.commandMode {
		t.Fatal("command escape failed")
	}
}

func TestEmptyViewsNoColorAndHeaderStates(t *testing.T) {
	s := domain.State{Snapshot: domain.Snapshot{PR: domain.PR{Owner: "o", Repo: "r", Number: 2}, HeadSHA: "abc", Checks: []domain.Check{{State: "failure"}}}, ReviewedHead: "old", Error: "offline", ClosedPrompt: true}
	m := NewModel(s, nil).(model)
	m.noColor = true
	m.width = 90
	m.height = 12
	if got := m.compactView(); strings.Contains(got, "\x1b[") || !strings.Contains(got, "STALE") || !strings.Contains(got, "error: offline") {
		t.Fatalf("header or NO_COLOR: %q", got)
	}
	s.Reports = nil
	m = apply(m, stateMsg(s))
	for i := 0; i < 6; i++ {
		m.active = tab(i)
		_ = m.compactView()
	}
	m.active = activityTab
	if m.visibleEventIndex(0) != -1 {
		t.Fatal("missing event row should not select an event")
	}
	if reportsPath(s) != "" {
		t.Fatal("empty report path")
	}
	m.quitDialog = true
	m = apply(m, key("y"))
	if m.quitDialog {
		t.Fatal("confirm dialog should close")
	}
}

func TestActionCommandVariantsAndScrolling(t *testing.T) {
	actions := []domain.Action{}
	m := NewModel(testState(), func(a domain.Action) { actions = append(actions, a) }).(model)
	m.active = changesTab
	m.height = 20
	m.runCommand("/refresh")
	m.runCommand("pause")
	m.runCommand("resume")
	m.runCommand("quit")
	m.runCommand("unknown")
	m.runCommand("")
	if len(actions) != 3 || actions[0].Kind != "refresh" || actions[1].Kind != "pause" || actions[2].Kind != "resume" || !m.quitDialog {
		t.Fatalf("commands: %+v, dialog %v", actions, m.quitDialog)
	}
	m.quitDialog = false
	m.state.Paused = false
	m = apply(m, key("p"))
	if actions[len(actions)-1].Kind != "pause" {
		t.Fatal(actions)
	}
	m = apply(m, key("r"))
	if actions[len(actions)-1].Kind != "refresh" {
		t.Fatal(actions)
	}
	m = apply(m, key("pgup"))
	if m.scroll == 0 || m.follow {
		t.Fatal("page up did not preserve a scrolled position")
	}
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.follow {
		t.Fatal("wheel down unexpectedly followed latest")
	}
}

func TestRunStopsOnCanceledContext(t *testing.T) {
	old := newProgram
	newProgram = func(m tea.Model) *tea.Program {
		return tea.NewProgram(m, tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard))
	}
	defer func() { newProgram = old }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestInputComponentsAndStateEdgeCases(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.answerMode = true
	m.answer.Focus()
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("answer")})
	if m.answer.Value() == "" {
		t.Fatal("answer input not updated")
	}
	m = apply(m, key("enter"))
	m.searchMode = true
	m.search.Focus()
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("query")})
	if m.search.Value() == "" {
		t.Fatal("search input not updated")
	}
	m = apply(m, key("enter"))
	m.commandMode = true
	m.command.Focus()
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("unknown")})
	m = apply(m, key("enter"))
	m.active = chatTab
	m.composer.SetValue("draft remains")
	s := m.state
	s.DraftReply = ""
	s.Questions = nil
	m = apply(m, stateMsg(s))
	if m.questionIndex != -1 || m.composer.Value() != "draft remains" {
		t.Fatal("state update should preserve local draft")
	}
	m.active = activityTab
	m.filter = ""
	m.eventDetail = true
	m.selectedEvent = 99
	m = apply(m, key("enter"))
	m.selectedEvent = 0
	_ = m.compactView()
	if m.selectedEventID() != 1 {
		t.Fatal("selected event resolution")
	}
	if tabAt(100, 80) != -1 {
		t.Fatal("tab click outside bounds")
	}
	if reportsPath(testState()) != "/tmp/report2.md" {
		t.Fatal("latest report path")
	}
}

func TestRunClosedUpdates(t *testing.T) {
	old := newProgram
	newProgram = func(m tea.Model) *tea.Program {
		return tea.NewProgram(m, tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard))
	}
	defer func() { newProgram = old }()
	updates := make(chan domain.State, 1)
	updates <- testState()
	close(updates)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	defer cancel()
	if err := Run(ctx, updates, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMoreKeyboardAndRenderBranches(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.height = 16
	m.width = 80
	m.active = reportTab
	m.state.Reports = []domain.Report{{Text: "no timestamp"}}
	_ = m.compactView()
	m.active = activityTab
	m.filter = "bug"
	m.sourceFilter = "assistant"
	_ = m.compactView()
	m.active = chatTab
	m.composer.SetValue("")
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if !strings.Contains(m.composer.Value(), "\n") {
		t.Fatal("alt-enter newline missing")
	}
	m.active = activityTab
	m.filter = ""
	m.sourceFilter = ""
	m.eventDetail = true
	m = apply(m, key("esc"))
	if m.eventDetail {
		t.Fatal("escape did not close detail")
	}
	m.selectedEvent = 0
	m = apply(m, key("right"))
	if m.selectedEvent != 1 {
		t.Fatal("right did not move event selection")
	}
	m = apply(m, key("left"))
	if m.selectedEvent != 0 {
		t.Fatal("left did not move event selection")
	}
	m.active = reportTab
	m.state.Reports = testState().Reports
	m.reportIndex = 0
	m = apply(m, key("right"))
	if m.reportIndex != 1 {
		t.Fatal("right report history")
	}
	m = apply(m, key("left"))
	if m.reportIndex != 0 {
		t.Fatal("left report history")
	}
	m.active = chatTab
	m = apply(m, key("f2"))
	if m.questionIndex != 0 {
		t.Fatal("question shortcut")
	}
	m.composer.SetValue("x")
	m = apply(m, key("down"))
	if m.questionChoice != 0 {
		t.Fatal("composer down key should remain in editor")
	}
	m.active = changesTab
	m = apply(m, key("enter"))
	m = apply(m, key("down"))
	m = apply(m, key("up"))
	if m.selectedFile != 0 {
		t.Fatal("file selection")
	}
	m.commandMode = true
	m.command.SetValue("pause")
	_ = m.compactView()
	m.commandMode = false
	m.searchMode = true
	m.search.SetValue("x")
	_ = m.compactView()
	m.noColor = true
	m.active = reportTab
	m.state.Reports = testState().Reports
	m.reportIndex = 1
	_ = m.compactView()
	m.state.Reports = []domain.Report{{Text: "no timestamp"}}
	_ = m.compactView()
	m.active = activityTab
	m.filter = "bug"
	m.sourceFilter = "assistant"
	_ = m.compactView()
	m.active = chatTab
	m.composer.SetValue("")
	_ = m.compactView()
	blank := NewModel(domain.State{}, nil).(model)
	blank.noColor = true
	blank.width = 20
	blank.height = 3
	_ = blank.compactView()
	blank.active = activityTab
	_ = blank.compactView()
	if selected := (model{}).selectedEventID(); selected != -1 {
		t.Fatal(selected)
	}
	if got := formatEvent(domain.Event{}); !strings.Contains(got, "review") || !strings.Contains(got, "event") {
		t.Fatal(got)
	}
}

func TestDefaultProgramFactoryConstructs(t *testing.T) {
	p := newProgram(NewModel(testState(), nil))
	if p == nil {
		t.Fatal("program not created")
	}
}

func TestRemainingInteractionBranches(t *testing.T) {
	actions := []domain.Action{}
	s := testState()
	s.Snapshot.Checks[0].StartedAt = "started"
	s.Snapshot.Checks[0].CompletedAt = "done"
	s.Reports = []domain.Report{{Text: "without timestamp"}}
	m := NewModel(s, func(a domain.Action) { actions = append(actions, a) }).(model)
	m.width = 240
	m.height = 20
	m.state.Paused = true
	m.newActivity = 2
	_ = m.compactView()
	m.active = activityTab
	m.filter = "does not match"
	m = apply(m, key("enter"))
	if !m.eventDetail {
		t.Fatal("enter should reveal no-result activity details")
	}
	m.filter = ""
	m.eventDetail = false
	m.selectedEvent = 1
	m = apply(m, key("up"))
	m = apply(m, key("down"))
	m.active = checksTab
	m.selectedFile = 0
	_ = m.compactView()
	m = apply(m, key("up"))
	m.state.Snapshot.Checks = []domain.Check{{Name: "running", State: "running", StartedAt: "now"}}
	_ = m.compactView()
	m = apply(m, key("down"))
	_ = m.compactView()
	m.active = reportTab
	m.state.Reports = []domain.Report{{Text: "no timestamp"}}
	_ = m.compactView()
	m.active = activityTab
	m.filter = "bug"
	m.sourceFilter = "assistant"
	_ = m.compactView()
	m.active = chatTab
	m.composer.SetValue("")
	m = apply(m, key("up"))
	s = m.state
	s.Questions = []domain.Question{{ID: "empty", Title: "Freeform", Prompt: "Tell me", Options: nil}}
	m = apply(m, stateMsg(s))
	m = apply(m, key("down"))
	m.composer.SetValue("answer freeform")
	m = apply(m, key("enter"))
	if actions[len(actions)-1].Kind != "message" {
		t.Fatalf("empty option list should leave composer usable: %+v", actions)
	}
	m.active = changesTab
	m = apply(m, key("q"))
	if !m.quitDialog {
		t.Fatal("q should open quit dialog outside chat")
	}
	m.quitDialog = false
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	empty := NewModel(domain.State{}, nil).(model)
	empty.active = checksTab
	_ = empty.compactView()
	empty.active = agentsTab
	_ = empty.compactView()
	empty.active = reportTab
	_ = empty.compactView()
}

func TestProducerSourceFiltersAndMouseTargets(t *testing.T) {
	actions := []domain.Action{}
	s := testState()
	s.Events = append(s.Events, domain.Event{ID: 6, Source: "You", Text: "user note"}, domain.Event{ID: 3, Source: "Agents", Text: "agent note"}, domain.Event{ID: 4, Source: "App", Text: "session note"}, domain.Event{ID: 5, Source: "CI", Text: "job passed"})
	m := NewModel(s, func(a domain.Action) { actions = append(actions, a) }).(model)
	for _, source := range []string{"You", "Codex", "Agents", "App", "GitHub", "CI"} {
		m.sourceFilter = source
		if len(m.filteredEvents()) == 0 {
			t.Fatalf("no events for producer source %q", source)
		}
	}
	for _, w := range []int{36, 100} {
		m.width = w
		m.noColor = true
		for i, label := range tabLabels(w) {
			x := 0
			for n := 0; n < i; n++ {
				x += len(tabLabels(w)[n]) + 1
			}
			x += len(label) / 2
			m.active = chatTab
			updated := apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: 2})
			if updated.active != tab(i) {
				t.Fatalf("tab hitbox %d at width %d selected %d; labels %v", i, w, updated.active, tabLabels(w))
			}
			m = updated
		}
	}
	m.active = checksTab
	m.height = 20
	m.width = 100
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 4})
	if len(actions) == 0 || actions[len(actions)-1].Kind != "open" || actions[len(actions)-1].Text != "https://checks/1" {
		t.Fatalf("check click did not open correct target: %+v", actions)
	}
	m.active = reportTab
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 4})
	if actions[len(actions)-1].Text != "/tmp/report.md" {
		t.Fatalf("report click target: %+v", actions)
	}
	m.active = chatTab
	m.questionFocus = true
	m.height = 30
	_, _, _, questionMap, bodyHeight := m.chatGeometry(effectiveWidth(m.width), m.height)
	optionLine := -1
	for i, option := range questionMap {
		if option == 0 {
			optionLine = i
			break
		}
	}
	if optionLine < 0 {
		t.Fatal("first option not rendered")
	}
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 3 + bodyHeight + optionLine})
	if actions[len(actions)-1].Kind != "answer" || actions[len(actions)-1].Text != "Correctness" || actions[len(actions)-1].ID != "q1" {
		t.Fatalf("question option click: %+v", actions)
	}
}

func TestStreamingAndUserDraftAreSeparateAndComposerPinned(t *testing.T) {
	s := testState()
	s.DraftReply = "Codex is still typing"
	m := NewModel(s, nil).(model)
	m.width = 90
	m.height = 10
	if m.composer.Value() != "" {
		t.Fatal("streaming assistant text initialized user composer")
	}
	m = typeText(m, "my draft jk")
	local := m.composer.Value()
	updated := s
	updated.DraftReply = "Codex added another thought"
	m = apply(m, stateMsg(updated))
	if m.composer.Value() != local {
		t.Fatal("streaming update replaced the user draft")
	}
	for i := 0; i < 30; i++ {
		m.state.Events = append(m.state.Events, domain.Event{ID: 10 + i, Source: "Codex", Text: strings.Repeat("long timeline item ", 2)})
	}
	m.scrollBy(-10)
	view := m.compactView()
	if !strings.Contains(view, "my draft jk") || !strings.Contains(view, "Enter sends") {
		t.Fatalf("composer should stay pinned while timeline scrolls: %q", view)
	}
	before := m.scroll
	m.setTab(changesTab)
	m.scrollBy(-2)
	changesScroll := m.scroll
	m.setTab(chatTab)
	if m.scroll != before {
		t.Fatalf("chat position was not preserved: got %d want %d", m.scroll, before)
	}
	m.setTab(changesTab)
	if m.scroll != changesScroll {
		t.Fatal("changes position was not preserved")
	}
}

func TestStaleConnectionOverridesGreenChecks(t *testing.T) {
	s := testState()
	s.Connection = "GitHub stale after network error"
	m := NewModel(s, nil).(model)
	view := m.compactView()
	if !strings.Contains(view, "CI stale") || strings.Contains(view, "CI passing") {
		t.Fatalf("stale poll should override last green status: %q", view)
	}
}

func TestQuitDialogMouseChoices(t *testing.T) {
	actions := []domain.Action{}
	m := NewModel(testState(), func(a domain.Action) { actions = append(actions, a) }).(model)
	m.width = 100
	s := m.state
	s.QuitRequested = true
	m = apply(m, stateMsg(s))
	if !m.quitDialog {
		t.Fatal("quit prompt should open as soon as state arrives")
	}
	line := quitLine(100)
	x := strings.Index(line, "[d]") + 2
	m = apply(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: 3})
	if len(actions) == 0 || actions[0].Kind != "confirm-quit-remove" {
		t.Fatalf("remove button click: %+v", actions)
	}
}

func TestTerminalControlSequencesAreRemoved(t *testing.T) {
	hostile := testState()
	hostile.Snapshot.Title = "bad\x1b[31mRED\x1b[0m\x1b]52;c;PAYLOAD\x07"
	hostile.Diff = "\x1b[2J+safe\x01text"
	hostile.Events[0].Detail = "\x1b]52;c;SECRET\x07details\x1b[?25l"
	hostile.Agents[0].Detail = "agent\x1b[8m output"
	hostile.Reports[0].Text = "report\x1b[31mtext\x1b[0m"
	m := NewModel(hostile, nil).(model)
	m.width = 100
	m.height = 40
	m.active = changesTab
	view := m.compactView()
	m.active = activityTab
	view += m.compactView()
	m.active = agentsTab
	view += m.compactView()
	m.active = reportTab
	view += m.compactView()
	if strings.ContainsAny(view, "\x1b\x01\x07") || strings.Contains(view, "PAYLOAD") || strings.Contains(view, "SECRET") {
		t.Fatalf("terminal controls escaped sanitization: %q", view)
	}
	if strings.Contains(hostile.Snapshot.Title, "PAYLOAD") == false {
		t.Fatal("safe rendering mutated controller snapshot")
	}
}

func TestRenderedScreenNeverWrapsOrPushesHeaderOffscreen(t *testing.T) {
	s := testState()
	s.Snapshot.Title = strings.Repeat("long pull request title ", 8)
	s.DraftReply = strings.Repeat("Codex streaming detail ", 12)
	s.Diff = strings.Repeat("+a very long diff line with source text ", 12)
	s.Events = append(s.Events, domain.Event{ID: 9, Source: "Codex", Text: strings.Repeat("a long timeline event ", 8), Detail: strings.Repeat("detail ", 20)})
	for _, width := range []int{100, 36, 24} {
		m := NewModel(s, nil).(model)
		m.width = width
		m.height = 32
		m.noColor = true
		for pane := tab(0); pane < tab(len(tabNames)); pane++ {
			m.active = pane
			assertFits(t, m.compactView(), width, 32)
		}
		m.quitDialog = true
		assertFits(t, m.compactView(), width, 32)
		m.quitDialog = false
		m.commandMode = true
		assertFits(t, m.compactView(), width, 32)
	}
}
func assertFits(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Fatalf("render has %d lines at %dx%d", len(lines), width, height)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("render line %d has width %d > %d: %q", i, got, width, line)
		}
	}
	if len(lines) < 3 || (!strings.Contains(lines[0], "QUICK REVIEW") && !strings.Contains(lines[0], "#42")) || !strings.Contains(lines[1], "CI ") {
		t.Fatalf("persistent header is missing: %q", view)
	}
}

func TestLauncherKeepsInvalidURLAvailableForRetry(t *testing.T) {
	actions := []domain.Action{}
	m := NewModel(domain.State{}, func(a domain.Action) { actions = append(actions, a) }).(model)
	m.width = 100
	m.height = 32
	m = typeText(m, "not a PR URL")
	m = apply(m, key("enter"))
	if !m.launcherMode || !m.launching || m.launcher.Value() != "not a PR URL" {
		t.Fatal("launcher should retain submitted URL while connecting")
	}
	s := domain.State{Error: "invalid pull request URL"}
	m = apply(m, stateMsg(s))
	if !m.launcherMode || m.launcher.Value() != "not a PR URL" || !strings.Contains(m.compactView(), "invalid pull request URL") {
		t.Fatalf("invalid URL should remain editable with error: %q", m.compactView())
	}
	m = apply(m, key("enter"))
	if len(actions) != 2 || actions[1].Kind != "start" {
		t.Fatalf("retry did not dispatch start: %+v", actions)
	}
	s = testState()
	m = apply(m, stateMsg(s))
	if m.launcherMode || m.launching {
		t.Fatal("accepted PR should enter the review")
	}
}
