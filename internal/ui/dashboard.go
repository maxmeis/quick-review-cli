package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
)

var ink = lipgloss.AdaptiveColor{Light: "#263247", Dark: "#E3E8F2"}
var muted = lipgloss.AdaptiveColor{Light: "#637088", Dark: "#8794AD"}
var accent = lipgloss.AdaptiveColor{Light: "#6740C8", Dark: "#B69CFF"}
var edge = lipgloss.AdaptiveColor{Light: "#C3CCDA", Dark: "#35415A"}
var good = lipgloss.AdaptiveColor{Light: "#147555", Dark: "#79DBB6"}

func (m model) dashboard() bool { return m.width >= 72 && m.height >= 20 }
func (m model) dashboardSize() (w, main, side, body, content int) {
	w = effectiveWidth(m.width)
	main = w
	body = m.height - 8
	content = body - 4
	if w >= 110 && !m.launcherMode {
		side = 30
		main = w - side - 2
	}
	return
}
func tone(text string, color lipgloss.TerminalColor, plain bool) string {
	if plain {
		return text
	}
	return lipgloss.NewStyle().Foreground(color).Render(text)
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mouse, ok := msg.(tea.MouseMsg); ok && m.dashboard() {
		return m.dashboardMouse(mouse)
	}
	next, cmd := m.update(msg)
	n := next.(model)
	if _, ok := msg.(tea.WindowSizeMsg); ok && n.dashboard() {
		_, main, _, _, _ := n.dashboardSize()
		n.composer.SetWidth(main - 6)
		n.launcher.Width = main - 10
	}
	return n, cmd
}
func (m model) View() string {
	if !m.dashboard() {
		return m.compactView()
	}
	view := m.dashboardView()
	if m.noColor {
		return ansi.Strip(view)
	}
	return view
}
func (m model) dashboardMouse(v tea.MouseMsg) (tea.Model, tea.Cmd) {
	w, main, _, _, content := m.dashboardSize()
	if m.quitDialog || ((m.state.QuitRequested || m.state.ClosedPrompt) && !m.suppressQuit) {
		if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft {
			x, y := dialogOrigin(w, m.height)
			if v.Y == y+7 {
				start := x + 2
				for i, label := range dialogButtons() {
					if v.X >= start && v.X < start+len(label) {
						m.quitDialog = true
						return m.update(keyForDialog(i))
					}
					start += len(label) + 2
				}
			}
		}
		return m, nil
	}
	if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft && v.Y == 4 {
		x := 0
		for i, label := range dashboardLabels(w) {
			if v.X >= x && v.X < x+len(label) {
				m.setTab(tab(i))
				return m, nil
			}
			x += len(label) + 1
		}
		return m, nil
	}
	if v.X < 2 || v.X >= main-2 || v.Y < 9 || v.Y >= 9+content {
		return m, nil
	}
	v.X -= 2
	v.Y = v.Y - 9 + 3
	outerW, outerH, outerFramed := m.width, m.height, m.framed
	m.framed = true
	m.width = main - 4
	m.height = content + 3
	next, cmd := m.update(v)
	n := next.(model)
	n.width = outerW
	n.height = outerH
	n.framed = outerFramed
	return n, cmd
}
func keyForDialog(i int) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{[]rune("ydn")[i]}}
}
func dashboardLabels(w int) []string {
	labels := make([]string, len(tabNames))
	for i, name := range tabNames {
		labels[i] = " " + name + " "
		if w >= 82 {
			labels[i] = fmt.Sprintf(" %d %s ", i+1, name)
		}
	}
	return labels
}
func (m model) dashboardView() string {
	w, main, side, body, content := m.dashboardSize()
	if m.quitDialog || ((m.state.QuitRequested || m.state.ClosedPrompt) && !m.suppressQuit) {
		return m.dialogView(w, m.height)
	}
	brand := tone("◈ QUICK REVIEW", accent, m.noColor)
	identity := "A focused workspace for pull requests"
	if pr := m.state.Snapshot.PR; pr.Number > 0 {
		identity = fmt.Sprintf("%s / %s   #%d", pr.Owner, pr.Repo, pr.Number)
	}
	header := brand + "    " + tone(identity, ink, m.noColor)
	title := first(m.state.Snapshot.Title, "Start a review with your GitHub pull request URL")
	ci := ciSummary(m.state.Snapshot.Checks)
	if strings.Contains(strings.ToLower(m.state.Connection), "stale") {
		ci = "stale"
	}
	status := dot(ci) + " CI " + ci + "   /   " + first(m.state.Phase, "Ready") + "   /   " + first(m.state.Snapshot.State, "LOCAL")
	if m.launcherMode {
		status = "○ Ready  /  Using your GitHub and Codex logins"
	}
	if m.state.Error != "" {
		status = "! " + m.state.Error
	}
	nav := []string{}
	for i, label := range dashboardLabels(w) {
		if tab(i) == m.active {
			if m.noColor {
				label = "[" + strings.TrimSpace(label) + "]"
			} else {
				label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#6740C8")).Render(label)
			}
		} else {
			label = tone(label, muted, m.noColor)
		}
		nav = append(nav, label)
	}
	inner := m
	inner.framed = true
	inner.width = main - 4
	inner.height = content + 3
	inner.composer.SetWidth(main - 6)
	view := inner.tabView()
	name := tabNames[m.active]
	subtitle := ""
	if m.active == chatTab {
		name = "Conversation"
		subtitle = "YOU + CODEX"
	}
	if m.launcherMode {
		view = inner.welcomeView()
		name = "New review"
		subtitle = "GH + CODEX"
	}
	if m.commandMode {
		view = "Command  " + m.command.View() + "\n" + view
	}
	if m.searchMode {
		view = "Filter  " + m.search.View() + "\n" + view
	}
	// The workspace owns the help bar; reserve the inner renderer's rows to keep hit targets stable.
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "↑/↓ scroll") || strings.HasPrefix(line, "Enter sends ·") {
			lines[i] = ""
		}
	}
	view = decorateContent(strings.Join(lines, "\n"), m.active, m.noColor)
	pane := panel(name, subtitle, view, main, body, accent, m.noColor)
	if side > 0 {
		pane = lipgloss.JoinHorizontal(lipgloss.Top, pane, "  ", m.sidebar(side, body))
	}
	footer := tone(" tab", accent, m.noColor) + " switch   " + tone("enter", accent, m.noColor) + " select / send   " + tone("ctrl+p", accent, m.noColor) + " commands   " + tone("ctrl+c", accent, m.noColor) + " quit"
	return fitScreen(strings.Join([]string{header, tone(title, muted, m.noColor), status, "", strings.Join(nav, " "), "", pane, footer}, "\n"), w, m.height)
}
func panel(title, subtitle, body string, w, h int, color lipgloss.TerminalColor, plain bool) string {
	inner := w - 4
	heading := title + strings.Repeat(" ", max(1, inner-ansi.StringWidth(title)-ansi.StringWidth(subtitle))) + subtitle
	heading = tone(ansi.Truncate(heading, inner, "…"), color, plain)
	lines := strings.Split(body, "\n")
	limit := max(0, h-4)
	if len(lines) > limit {
		lines = lines[:limit]
	}
	for len(lines) < limit {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, inner, "…")
	}
	text := heading + "\n" + tone(strings.Repeat("─", inner), edge, plain) + "\n" + strings.Join(lines, "\n")
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(w - 2).Height(h - 2)
	if !plain {
		style = style.BorderForeground(color)
	}
	return style.Render(text)
}
func (m model) sidebar(w, h int) string {
	s := m.state
	rows := []string{tone("REVISION", muted, m.noColor), "Head   " + first(short(s.Snapshot.HeadSHA), "—"), "Review " + first(short(s.ReviewedHead), "—"), "", tone("CHECKS", muted, m.noColor)}
	for _, c := range s.Snapshot.Checks {
		rows = append(rows, checkIcon(c.State)+" "+c.Name)
	}
	rows = append(rows, "", tone("REVIEWERS", muted, m.noColor))
	for _, a := range s.Agents {
		rows = append(rows, tone("● "+a.Name, good, m.noColor), "  "+a.Status)
	}
	rows = append(rows, "", tone("SAVED REPORTS", muted, m.noColor), fmt.Sprintf("%d Markdown reports", len(s.Reports)), "", tone(first(s.Connection, "Ready to connect"), muted, m.noColor))
	return panel("Overview", "LIVE", strings.Join(rows, "\n"), w, h, muted, m.noColor)
}
func fitScreen(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return strings.Join(lines, "\n")
}
func dialogOrigin(w, h int) (int, int) { return max(0, (w-66)/2), max(0, (h-10)/2) }
func dialogButtons() []string {
	return []string{"[y] Keep checkouts", "[d] Remove clean", "[n] Keep watching"}
}
func (m model) dialogView(w, h int) string {
	buttons := dialogButtons()
	for i, label := range buttons {
		buttons[i] = tone(label, accent, m.noColor)
	}
	text := "Your reports and conversation stay saved.\n\nOnly clean checkouts are removed. Local edits stay protected.\n\n" + strings.Join(buttons, "  ")
	box := panel("Leave this review?", "", text, 66, 10, accent, m.noColor)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

func (m model) editorView() string {
	if !m.framed {
		return m.composer.View()
	}
	input := m.composer
	input.SetWidth(max(10, m.width-4))
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(m.width - 2)
	if !m.noColor {
		style = style.BorderForeground(accent)
	}
	return style.Render(input.View())
}
func (m model) welcomeView() string {
	input := m.launcher
	input.Width = min(64, m.width-8)
	field := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if !m.noColor {
		field = field.BorderForeground(accent)
	}
	status := "Enter to start  ·  Esc to exit"
	if m.launching {
		status = "Connecting to the pull request…"
	}
	if m.state.Error != "" {
		status = m.state.Error
	}
	return tone("Review a pull request", ink, m.noColor) + "\n" + tone("Paste a GitHub pull request URL to begin.", muted, m.noColor) + "\n\n" + field.Render(input.View()) + "\n" + tone(status, muted, m.noColor)
}
