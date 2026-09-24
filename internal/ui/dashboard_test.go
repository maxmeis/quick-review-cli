package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"quick-review-cli/internal/domain"
)

func dashboardModel(s domain.State, width, height int, dispatch func(domain.Action)) model {
	m := NewModel(s, dispatch).(model)
	m.noColor = false
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(model)
}

func dashboardKey(m model, msg tea.Msg) model {
	updated, _ := m.Update(msg)
	return updated.(model)
}

func TestDashboardProductionViewsAcrossTabsAndSizes(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	oldDark := lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	defer lipgloss.SetColorProfile(oldProfile)
	defer lipgloss.SetHasDarkBackground(oldDark)

	for _, width := range []int{72, 110} {
		for page := tab(0); page < tab(len(tabNames)); page++ {
			m := dashboardModel(testState(), width, 28, nil)
			m.active = page
			view := m.View()
			if !strings.Contains(view, tabNames[page]) && page != chatTab {
				t.Errorf("width=%d page=%s is not shown in dashboard: %q", width, tabNames[page], view)
			}
			assertDashboardFits(t, view, width, 28)
		}
	}

	m := dashboardModel(testState(), 110, 28, nil)
	wide, main, side, body, content := m.dashboardSize()
	if wide != 110 || main != 78 || side != 30 || body != 20 || content != 16 {
		t.Fatalf("wide layout sizes = %d/%d/%d/%d/%d", wide, main, side, body, content)
	}
	m = dashboardModel(testState(), 72, 20, nil)
	_, main, side, body, content = m.dashboardSize()
	if main != 72 || side != 0 || body != 12 || content != 8 {
		t.Fatalf("single-pane layout sizes = %d/%d/%d/%d", main, side, body, content)
	}
	if got := dashboardModel(testState(), 71, 28, nil).View(); strings.Contains(got, "Overview") {
		t.Fatalf("compact fallback rendered dashboard sidebar: %q", got)
	}
	if got := dashboardModel(testState(), 100, 19, nil).View(); strings.Contains(got, "Overview") {
		t.Fatalf("short fallback rendered dashboard sidebar: %q", got)
	}
}

func TestDashboardMouseNavigationContentAndSidebarBoundaries(t *testing.T) {
	var actions []domain.Action
	m := dashboardModel(testState(), 110, 28, func(a domain.Action) { actions = append(actions, a) })
	// Tabs occupy row 4. Click the third (Checks) tab using its rendered label bounds.
	labels := dashboardLabels(110)
	x := len(labels[0]) + 1 + len(labels[1]) + 1 + 1
	m = dashboardKey(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: 4})
	if m.active != checksTab {
		t.Fatalf("tab-row click selected %v, want Checks", m.active)
	}
	// Content begins on row 9; the first check row follows the pane heading.
	m = dashboardKey(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: 10})
	if len(actions) != 1 || actions[0].Kind != "open" || actions[0].Text != "https://checks/1" {
		t.Fatalf("content click did not open the check: %+v", actions)
	}
	// The overview sidebar must not route clicks into the underlying check list.
	m.active = checksTab
	actions = nil
	m = dashboardKey(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 77, Y: 10})
	if len(actions) != 0 || m.selectedFile != 0 {
		t.Fatalf("sidebar click escaped into content: actions=%+v selection=%d", actions, m.selectedFile)
	}
	// Content boundary and non-press mouse events are inert.
	before := m.active
	m = dashboardKey(m, tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 3, Y: 4})
	m = dashboardKey(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: 8})
	m = dashboardKey(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1000, Y: 4})
	if m.active != before || len(actions) != 0 {
		t.Fatalf("out-of-pane mouse input changed model: tab=%v actions=%+v", m.active, actions)
	}
}

func TestDashboardDialogBlocksBackgroundAndRoutesButtons(t *testing.T) {
	for i, want := range []string{"confirm-quit", "confirm-quit-remove", "cancel-quit"} {
		var actions []domain.Action
		m := dashboardModel(testState(), 110, 28, func(a domain.Action) { actions = append(actions, a) })
		m.quitDialog = true
		x, y := dialogOrigin(110, 28)
		buttonX := x + 3
		for previous := 0; previous < i; previous++ {
			buttonX += len(dialogButtons()[previous]) + 2
		}
		// A click away from the dialog is swallowed without closing it.
		m = dashboardKey(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 4})
		if !m.quitDialog || len(actions) != 0 {
			t.Fatalf("dialog allowed background input: %+v", actions)
		}
		m = dashboardKey(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: buttonX, Y: y + 7})
		if m.quitDialog || len(actions) != 1 || actions[0].Kind != want {
			t.Fatalf("button %d = dialog=%v actions=%+v, want %s", i, m.quitDialog, actions, want)
		}
	}
	dialog := dashboardModel(testState(), 110, 28, nil)
	dialog.quitDialog = true
	if got := dialog.View(); !strings.Contains(got, "Leave this review?") {
		t.Fatalf("production view omitted the dialog: %q", got)
	}
	// A closed PR prompt uses the same modal gate and ignores outside clicks.
	m := dashboardModel(testState(), 110, 28, nil)
	m.state.ClosedPrompt = true
	updated, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 4})
	if got := updated.(model); !got.state.ClosedPrompt {
		t.Fatal("closed prompt did not block background input")
	}
}

func TestDashboardLauncherSearchAndCommandInteractions(t *testing.T) {
	var actions []domain.Action
	launcher := dashboardModel(domain.State{}, 110, 28, func(a domain.Action) { actions = append(actions, a) })
	launcher = dashboardKey(launcher, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://github.com/acme/widget/pull/42")})
	if !strings.Contains(launcher.View(), "https://github.com/acme/widget/pull/42") {
		t.Fatalf("launcher text missing from production view: %q", launcher.View())
	}
	if strings.Contains(launcher.View(), "Overview") {
		t.Fatal("launcher should hide the workspace sidebar")
	}
	launcher = dashboardKey(launcher, tea.KeyMsg{Type: tea.KeyEnter})
	if len(actions) != 1 || actions[0].Kind != "start" {
		t.Fatalf("launcher submit = %+v", actions)
	}

	m := dashboardModel(testState(), 110, 28, func(a domain.Action) { actions = append(actions, a) })
	m.active = activityTab
	m = dashboardKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = dashboardKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Codex")})
	if !m.searchMode || !strings.Contains(m.View(), "Filter") {
		t.Fatalf("activity search mode not reflected in production view: %q", m.View())
	}
	m = dashboardKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.searchMode || m.filter != "Codex" {
		t.Fatalf("search submit state = mode %v filter %q", m.searchMode, m.filter)
	}
	m = dashboardKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	if !m.commandMode || !strings.Contains(m.View(), "Command") {
		t.Fatalf("command mode not reflected in production view: %q", m.View())
	}
	m = dashboardKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("refresh")})
	m = dashboardKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.commandMode || len(actions) < 2 || actions[len(actions)-1].Kind != "refresh" {
		t.Fatalf("command submit state/actions: mode=%v actions=%+v", m.commandMode, actions)
	}
}

func TestDashboardStatusDialogSuppressionAndEditorHelpers(t *testing.T) {
	for _, state := range []domain.State{
		{Snapshot: testState().Snapshot, Connection: "GitHub stale", Phase: "Waiting"},
		{Snapshot: testState().Snapshot, Error: "offline"},
		{Snapshot: testState().Snapshot},
	} {
		m := dashboardModel(state, 110, 28, nil)
		_ = m.View()
	}
	m := dashboardModel(testState(), 110, 28, nil)
	m.state.QuitRequested, m.suppressQuit = true, true
	if strings.Contains(m.View(), "Leave this review?") {
		t.Fatal("suppressed quit prompt is still visible")
	}
	if got := m.dialogView(10, 4); got == "" {
		t.Fatal("dialog view should render even when screen is smaller than its dialog")
	}
	m.framed = false
	if got := m.editorView(); got != m.composer.View() {
		t.Fatal("unframed editor changed the composer")
	}
	m.framed, m.noColor = true, true
	if got := m.editorView(); got == "" || strings.Contains(got, "\x1b[") {
		t.Fatalf("plain framed editor = %q", got)
	}
	m.launcherMode, m.launching = true, true
	if got := m.welcomeView(); !strings.Contains(got, "Connecting") {
		t.Fatalf("welcome connecting status = %q", got)
	}
	m.state.Error = "cannot connect"
	if got := m.welcomeView(); !strings.Contains(got, "cannot connect") {
		t.Fatalf("welcome error status = %q", got)
	}
	m.state.Error, m.launching = "", false
	if got := m.welcomeView(); !strings.Contains(got, "Enter to start") {
		t.Fatalf("welcome idle status = %q", got)
	}
}

func TestDashboardThemeFallbackAndFit(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	oldDark := lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(false)
	defer lipgloss.SetColorProfile(oldProfile)
	defer lipgloss.SetHasDarkBackground(oldDark)

	light := dashboardModel(testState(), 110, 28, nil).View()
	if !strings.Contains(light, "Overview") || !strings.Contains(light, "QUICK REVIEW") {
		t.Fatalf("light dashboard missing identity/sidebar: %q", light)
	}
	plain := dashboardModel(testState(), 110, 28, nil)
	plain.noColor = true
	plainView := plain.View()
	if strings.Contains(plainView, "\x1b[") {
		t.Fatalf("no-color dashboard emitted ANSI: %q", plainView)
	}
	assertDashboardFits(t, light, 110, 28)
	assertDashboardFits(t, plainView, 110, 28)

	// Exercise local heading/body truncation, row padding, and both panel borders.
	short := panel("Overview", "LIVE", "one\ntwo\nthree", 30, 8, accent, true)
	styled := panel("A very long heading", "LIVE", "body", 30, 8, accent, false)
	if ansi.StringWidth(strings.Split(short, "\n")[0]) > 30 || !strings.Contains(styled, "╭") {
		t.Fatalf("panel failed to constrain or style output: %q / %q", short, styled)
	}
	if fit := fitScreen("one\ntwo\nthree", 3, 2); fit != "one\ntwo" {
		t.Fatalf("fitScreen rows = %q", fit)
	}
	if got := fitScreen("界🙂", 2, 1); ansi.StringWidth(got) > 2 {
		t.Fatalf("fitScreen exceeded cell width: %q", got)
	}
}

func TestDashboardHelpersAllBranches(t *testing.T) {
	if !dashboardModel(testState(), 72, 20, nil).dashboard() || dashboardModel(testState(), 71, 20, nil).dashboard() || dashboardModel(testState(), 72, 19, nil).dashboard() {
		t.Fatal("dashboard threshold is incorrect")
	}
	for _, width := range []int{72, 81, 82, 110} {
		labels := dashboardLabels(width)
		if len(labels) != len(tabNames) {
			t.Fatalf("label count at %d = %d", width, len(labels))
		}
	}
	for i, want := range []rune{'y', 'd', 'n'} {
		if got := keyForDialog(i).String(); got != string(want) {
			t.Fatalf("dialog key %d = %q", i, got)
		}
	}
	if got := dialogButtons(); len(got) != 3 || !strings.Contains(got[1], "Remove") {
		t.Fatalf("dialog buttons = %v", got)
	}
	if x, y := dialogOrigin(10, 4); x != 0 || y != 0 {
		t.Fatalf("dialog origin did not clamp: %d,%d", x, y)
	}
	if got := tone("plain", accent, true); got != "plain" {
		t.Fatalf("plain tone changed content: %q", got)
	}
	if got := tone("colored", accent, false); ansi.Strip(got) != "colored" {
		t.Fatalf("colored tone changed content: %q", got)
	}
}

func assertDashboardFits(t *testing.T, view string, width, height int) {
	t.Helper()
	rows := strings.Split(view, "\n")
	if len(rows) > height {
		t.Fatalf("dashboard has %d rows for %d-row screen", len(rows), height)
	}
	for i, row := range rows {
		if cells := ansi.StringWidth(row); cells > width {
			t.Errorf("dashboard row %d is %d cells for width %d: %q", i, cells, width, row)
		}
	}
}

func TestCompactLauncherConnectionStatus(t *testing.T) {
	m := NewModel(domain.State{}, nil).(model)
	m.width, m.height, m.launching = 60, 18, true
	if !strings.Contains(m.View(), "Connecting to the pull request") {
		t.Fatal(m.View())
	}
}
