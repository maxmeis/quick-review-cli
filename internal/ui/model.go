package ui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	charmansi "github.com/charmbracelet/x/ansi"
	"os"
	"quick-review-cli/internal/domain"
	"strings"
)

type tab int

const (
	chatTab tab = iota
	changesTab
	checksTab
	agentsTab
	reportTab
	activityTab
)

var tabNames = []string{"Chat", "Changes", "Checks", "Agents", "Report", "Activity"}

type model struct {
	framed                                           bool
	state                                            domain.State
	active                                           tab
	width, height                                    int
	scroll, newActivity                              int
	follow                                           bool
	dispatch                                         func(domain.Action)
	composer                                         textarea.Model
	launcher, search, command, answer                textinput.Model
	launcherMode, launching, searchMode, commandMode bool
	filter                                           string
	sourceFilter                                     string
	quitDialog                                       bool
	suppressQuit                                     bool
	questionIndex, questionChoice                    int
	answerMode                                       bool
	selectedEvent, selectedFile, reportIndex         int
	eventDetail                                      bool
	questionFocus                                    bool
	scrollByTab                                      map[tab]int
	noColor                                          bool
}

func safeText(value string) string {
	value = charmansi.Strip(value)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, value)
}
func safeState(s domain.State) domain.State {
	s.Snapshot.Title = safeText(s.Snapshot.Title)
	s.Snapshot.Body = safeText(s.Snapshot.Body)
	s.Snapshot.BaseBranch = safeText(s.Snapshot.BaseBranch)
	s.Snapshot.State = safeText(s.Snapshot.State)
	s.Snapshot.HeadSHA = safeText(s.Snapshot.HeadSHA)
	s.Snapshot.BaseSHA = safeText(s.Snapshot.BaseSHA)
	s.Snapshot.URL = safeText(s.Snapshot.URL)
	s.Snapshot.PR.Owner = safeText(s.Snapshot.PR.Owner)
	s.Snapshot.PR.Repo = safeText(s.Snapshot.PR.Repo)
	s.Snapshot.Checks = append([]domain.Check(nil), s.Snapshot.Checks...)
	for i := range s.Snapshot.Checks {
		c := &s.Snapshot.Checks[i]
		c.Name = safeText(c.Name)
		c.State = safeText(c.State)
		c.URL = safeText(c.URL)
		c.StartedAt = safeText(c.StartedAt)
		c.CompletedAt = safeText(c.CompletedAt)
	}
	s.Snapshot.Files = append([]domain.File(nil), s.Snapshot.Files...)
	for i := range s.Snapshot.Files {
		s.Snapshot.Files[i].Path = safeText(s.Snapshot.Files[i].Path)
	}
	s.Snapshot.Commits = append([]domain.Commit(nil), s.Snapshot.Commits...)
	for i := range s.Snapshot.Commits {
		s.Snapshot.Commits[i].Title = safeText(s.Snapshot.Commits[i].Title)
	}
	s.Events = append([]domain.Event(nil), s.Events...)
	for i := range s.Events {
		e := &s.Events[i]
		e.Source = safeText(e.Source)
		e.Kind = safeText(e.Kind)
		e.Text = safeText(e.Text)
		e.Detail = safeText(e.Detail)
		e.SHA = safeText(e.SHA)
	}
	s.Agents = append([]domain.Agent(nil), s.Agents...)
	for i := range s.Agents {
		a := &s.Agents[i]
		a.Name = safeText(a.Name)
		a.Scope = safeText(a.Scope)
		a.Status = safeText(a.Status)
		a.Detail = safeText(a.Detail)
	}
	s.Reports = append([]domain.Report(nil), s.Reports...)
	for i := range s.Reports {
		r := &s.Reports[i]
		r.Path = safeText(r.Path)
		r.Text = safeText(r.Text)
		r.HeadSHA = safeText(r.HeadSHA)
		r.BaseSHA = safeText(r.BaseSHA)
	}
	s.Questions = append([]domain.Question(nil), s.Questions...)
	for i := range s.Questions {
		q := &s.Questions[i]
		q.Title = safeText(q.Title)
		q.Prompt = safeText(q.Prompt)
		q.Options = append([]string(nil), q.Options...)
		for j := range q.Options {
			q.Options[j] = safeText(q.Options[j])
		}
	}
	s.Diff = safeText(s.Diff)
	s.ReviewedHead = safeText(s.ReviewedHead)
	s.DraftReply = safeText(s.DraftReply)
	s.Error = safeText(s.Error)
	s.Connection = safeText(s.Connection)
	s.Phase = safeText(s.Phase)
	return s
}

func NewModel(initial domain.State, dispatch func(domain.Action)) tea.Model {
	initial = safeState(initial)
	m := model{state: initial, dispatch: dispatch, follow: true, questionIndex: -1, noColor: os.Getenv("NO_COLOR") != "", scrollByTab: map[tab]int{}}
	m.launcherMode = initial.Snapshot.PR.Number == 0
	m.launcher = textinput.New()
	m.launcher.Placeholder = "https://github.com/owner/repo/pull/123"
	m.launcher.CharLimit = 512
	m.launcher.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#435466", Dark: "#B8C5D2"})
	m.launcher.Focus()
	m.search = textinput.New()
	m.search.Placeholder = "Search activity…"
	m.command = textinput.New()
	m.command.Placeholder = "refresh · pause · resume · report · quit"
	m.answer = textinput.New()
	m.answer.Placeholder = "Type your answer…"
	m.composer = textarea.New()
	m.composer.Placeholder = "Write a reply…"
	m.composer.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#435466", Dark: "#B8C5D2"})
	m.composer.BlurredStyle.Placeholder = m.composer.FocusedStyle.Placeholder
	m.composer.SetWidth(70)
	m.composer.SetHeight(2)
	m.composer.ShowLineNumbers = false
	m.composer.CharLimit = 10000
	m.composer.Focus()
	if len(initial.Questions) > 0 {
		m.questionIndex = 0
	}
	return m
}

// Run starts the terminal interface. The controller remains the owner of review work.
func Run(ctx context.Context, updates <-chan domain.State, dispatch func(domain.Action)) error {
	m := NewModel(domain.State{}, dispatch).(model)
	p := newProgram(m)
	go func() {
		for {
			select {
			case <-ctx.Done():
				p.Send(contextDoneMsg{})
				return
			case s, ok := <-updates:
				if !ok {
					p.Send(contextDoneMsg{})
					return
				}
				p.Send(stateMsg(s))
			}
		}
	}()
	_, err := p.Run()
	return err
}

type stateMsg domain.State
type contextDoneMsg struct{}

var newProgram = func(m tea.Model) *tea.Program {
	return tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
}

func (m model) Init() tea.Cmd { return textarea.Blink }

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case stateMsg:
		oldN := len(m.state.Events)
		preserveReportPosition := m.active == reportTab
		reportStart := m.reportTop()
		m.state = safeState(domain.State(v))
		if preserveReportPosition {
			m.setReportTop(reportStart)
		}
		if m.questionIndex == -1 && len(m.state.Questions) > 0 {
			m.questionIndex = 0
		}
		m.suppressQuit = false
		if m.state.QuitRequested || m.state.ClosedPrompt {
			m.quitDialog = true
		}
		if len(m.state.Events) > oldN && !m.follow {
			added := len(m.state.Events) - oldN
			m.newActivity += added
			m.scroll += added
			if m.scrollByTab == nil {
				m.scrollByTab = map[tab]int{}
			}
			m.scrollByTab[m.active] = m.scroll
		}
		if m.state.Snapshot.PR.Number > 0 {
			m.launcherMode = false
			m.launching = false
		}
		if m.questionIndex >= len(m.state.Questions) {
			m.questionIndex = len(m.state.Questions) - 1
		}
		return m, nil
	case contextDoneMsg:
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.composer.SetWidth(max(20, v.Width-4))
		m.composer.SetHeight(max(1, min(2, v.Height-6)))
		m.launcher.Width = max(10, v.Width-8)
		m.search.Width = max(20, v.Width-6)
		m.command.Width = max(20, v.Width-6)
		m.answer.Width = max(20, v.Width-6)
		return m, nil
	case tea.MouseMsg:
		if m.quitDialog && (v.Y != 3 || v.Button != tea.MouseButtonLeft) {
			return m, nil
		}
		if v.Action == tea.MouseActionPress {
			if v.Button == tea.MouseButtonWheelUp {
				return m.scrollBy(-3), nil
			}
			if v.Button == tea.MouseButtonWheelDown {
				return m.scrollBy(3), nil
			}
			if v.Button == tea.MouseButtonLeft {
				w := max(20, min(180, m.width))
				if m.quitDialog && v.Y == 3 {
					line := quitLine(w)
					keep := strings.Index(line, "[y]")
					remove := strings.Index(line, "[d]")
					watch := strings.Index(line, "[n]")
					if keep < 0 {
						keep = strings.Index(line, "y ")
					}
					if remove < 0 {
						remove = strings.Index(line, "d ")
					}
					if watch < 0 {
						watch = strings.Index(line, "n ")
					}
					switch {
					case v.X >= keep && v.X < remove:
						m.emit("confirm-quit", "", "")
					case v.X >= remove && v.X < watch:
						m.emit("confirm-quit-remove", "", "")
					case v.X >= watch:
						m.emit("cancel-quit", "", "")
						m.suppressQuit = true
					}
					m.quitDialog = false
					return m, nil
				}
				if v.Y == 2 {
					i := tabAt(v.X, w)
					if i >= 0 {
						m.setTab(tab(i))
						m.newActivity = 0
					}
					return m, nil
				}
				if m.active == activityTab && v.Y >= 4 {
					rows, mapping := m.activityRows()
					rows, mapping = wrapMappedRows(rows, mapping, effectiveWidth(m.width))
					visible := max(0, m.height-5-m.overlayRows())
					end := max(0, len(rows)-m.scroll)
					start := max(0, end-visible)
					row := start + v.Y - 3
					if v.Y-3 >= 0 && v.Y-3 < visible && row >= 0 && row < len(mapping) {
						m.selectedEvent = mapping[row]
					} else {
						m.selectedEvent = -1
					}
					if m.selectedEvent >= 0 {
						m.eventDetail = true
					}
				}
				if m.active == checksTab && v.Y >= 4 {
					rows, mapping := m.checkRows()
					rows, mapping = wrapMappedRows(rows, mapping, effectiveWidth(m.width))
					visible := max(0, m.height-5-m.overlayRows())
					end := max(0, len(rows)-m.scroll)
					start := max(0, end-visible)
					row := start + v.Y - 3
					idx := -1
					if v.Y-3 >= 0 && v.Y-3 < visible && row >= 0 && row < len(mapping) {
						idx = mapping[row]
					}
					if idx >= 0 {
						m.selectedFile = idx
						m.openCheck()
					}
				}
				if m.active == reportTab && v.Y >= 4 {
					rows, mapping := m.reportRows()
					rows, mapping = wrapMappedRows(rows, mapping, effectiveWidth(m.width))
					visible := max(0, m.height-5-m.overlayRows())
					end := max(0, len(rows)-m.scroll)
					start := max(0, end-visible)
					row := start + v.Y - 3
					if v.Y-3 >= 0 && v.Y-3 < visible && row >= 0 && row < len(mapping) && mapping[row] >= 0 {
						m.reportIndex = mapping[row]
						m.openReport()
					}
				}
				if m.active == chatTab && m.questionFocus && len(m.state.Questions) > 0 {
					q := m.state.Questions[max(0, min(m.questionIndex, len(m.state.Questions)-1))]
					_, _, qrows, qmap, bodyHeight := m.chatGeometry(effectiveWidth(m.width), max(6, m.height))
					idx := v.Y - (3 + bodyHeight)
					if idx >= 0 && idx < len(qmap) && qmap[idx] >= 0 {
						choice := qmap[idx]
						m.questionChoice = choice
						m.emit("answer", q.Options[choice], q.ID)
						m.questionFocus = false
					}
					_ = qrows
				}
			}
		}
		return m, nil
	case tea.KeyMsg:
		if m.quitDialog {
			switch v.String() {
			case "y", "Y", "enter":
				m.emit("confirm-quit", "", "")
				m.quitDialog = false
			case "d", "D":
				m.emit("confirm-quit-remove", "", "")
				m.quitDialog = false
			case "n", "N", "esc", "q", "ctrl+c":
				m.emit("cancel-quit", "", "")
				m.quitDialog = false
				m.suppressQuit = true
			}
			return m, nil
		}
		if v.Type == tea.KeyEsc && m.active == chatTab && m.questionFocus {
			m.questionFocus = false
			return m, nil
		}
		if v.Type == tea.KeyEsc && m.active == activityTab && (m.filter != "" || m.sourceFilter != "") {
			m.filter, m.sourceFilter = "", ""
			m.resetTabScroll()
			return m, nil
		}
		if m.launcherMode {
			return m.updateLauncher(v)
		}
		if m.answerMode {
			return m.updateAnswer(v)
		}
		if m.searchMode {
			return m.updateSearch(v)
		}
		if m.commandMode {
			return m.updateCommand(v)
		}
		if v.Alt && strings.HasPrefix(v.String(), "alt+") && len(v.String()) == 5 && v.String()[4] >= '1' && v.String()[4] <= '6' {
			m.setTab(tab(v.String()[4] - '1'))
			return m, nil
		}
		if v.Type == tea.KeyCtrlP {
			m.commandMode = true
			m.command.SetValue("")
			m.command.Focus()
			return m, nil
		}
		if v.Type == tea.KeyEsc && m.eventDetail {
			m.eventDetail = false
			return m, nil
		}
		if m.active == chatTab && v.Type == tea.KeyEnter {
			if v.Alt {
				m.composer.InsertString("\n")
				return m, nil
			}
			if m.questionFocus && len(m.state.Questions) > 0 && strings.TrimSpace(m.composer.Value()) == "" {
				q := m.state.Questions[max(0, min(m.questionIndex, len(m.state.Questions)-1))]
				if len(q.Options) > 0 {
					m.emit("answer", q.Options[max(0, min(m.questionChoice, len(q.Options)-1))], q.ID)
					return m, nil
				}
			}
			text := strings.TrimSpace(m.composer.Value())
			if text != "" {
				m.emit("message", text, "")
				m.composer.Reset()
			}
			return m, nil
		}
		if v.Type == tea.KeyTab && m.active != chatTab {
			m.setTab((m.active + 1) % tab(len(tabNames)))
			return m, nil
		}
		if v.Type == tea.KeyShiftTab && m.active != chatTab {
			m.setTab((m.active + tab(len(tabNames)) - 1) % tab(len(tabNames)))
			return m, nil
		}
		switch v.String() {
		case "pgdown", "ctrl+f":
			return m.scrollBy(max(1, m.height-7)), nil
		case "pgup", "ctrl+b":
			return m.scrollBy(-max(1, m.height-7)), nil
		case "/":
			if m.active == activityTab {
				m.searchMode = true
				m.search.SetValue("")
				m.search.Focus()
				return m, nil
			}
		case "s":
			if m.active == activityTab {
				sources := []string{"", "You", "Codex", "Agents", "App", "GitHub", "CI"}
				i := 0
				for n, source := range sources {
					if source == m.sourceFilter {
						i = n
						break
					}
				}
				m.sourceFilter = sources[(i+1)%len(sources)]
				m.resetTabScroll()
				return m, nil
			}
		case "f":
			if m.active == activityTab {
				m.searchMode = true
				m.search.Focus()
				return m, nil
			}
		case "enter":
			if m.active == activityTab {
				m.eventDetail = true
				if len(m.filteredEvents()) > 0 {
					m.selectedEvent = max(0, min(m.selectedEvent, len(m.filteredEvents())-1))
				}
				return m, nil
			}
			if m.active == changesTab {
				m.selectedFile = (m.selectedFile + 1) % max(1, len(m.state.Snapshot.Files))
				return m, nil
			}
			if m.active == checksTab {
				m.openCheck()
				return m, nil
			}
			if m.active == reportTab {
				m.openReport()
				return m, nil
			}
		case "left", "h":
			if m.active == activityTab && m.selectedEvent > 0 {
				m.selectedEvent--
			} else if m.active == chatTab {
				break
			}
			return m, nil
		case "right", "l":
			if m.active == activityTab && m.selectedEvent < len(m.state.Events)-1 {
				m.selectedEvent++
			} else if m.active == chatTab {
				break
			}
			return m, nil
		case "up", "k":
			if m.active == chatTab && !m.questionFocus {
				break
			}
			if m.active == chatTab && m.questionFocus && len(m.state.Questions) > 0 && strings.TrimSpace(m.composer.Value()) == "" {
				m.questionChoice = max(0, m.questionChoice-1)
			} else if m.active == checksTab {
				m.selectedFile = max(0, m.selectedFile-1)
			} else if m.active == activityTab && m.selectedEvent > 0 {
				m.selectedEvent--
			} else {
				return m.scrollBy(-1), nil
			}
			return m, nil
		case "down", "j":
			if m.active == chatTab && !m.questionFocus {
				break
			}
			if m.active == chatTab && m.questionFocus && len(m.state.Questions) > 0 && strings.TrimSpace(m.composer.Value()) == "" && len(m.state.Questions[max(0, m.questionIndex)].Options) > 0 {
				m.questionChoice = min(len(m.state.Questions[max(0, m.questionIndex)].Options)-1, m.questionChoice+1)
			} else if m.active == checksTab {
				m.selectedFile = min(max(0, len(m.state.Snapshot.Checks)-1), m.selectedFile+1)
			} else if m.active == activityTab && m.selectedEvent < len(m.state.Events)-1 {
				m.selectedEvent++
			} else {
				return m.scrollBy(1), nil
			}
			return m, nil
		case "q":
			if m.active != chatTab {
				m.quitDialog = true
				return m, nil
			}
		case "ctrl+c":
			m.quitDialog = true
			return m, nil
		case "r":
			if m.active != chatTab {
				m.emit("refresh", "", "")
				return m, nil
			}
		case "p":
			if m.active != chatTab {
				if m.state.Paused {
					m.emit("resume", "", "")
				} else {
					m.emit("pause", "", "")
				}
				return m, nil
			}
		case "f2":
			if m.active == chatTab && len(m.state.Questions) > 0 {
				m.questionFocus = true
				m.questionChoice = 0
				return m, nil
			}
		case "a":
			if m.active == chatTab && m.questionFocus && len(m.state.Questions) > 0 {
				m.answerMode = true
				m.answer.Focus()
				return m, nil
			}
		}
	}
	if m.active == chatTab {
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateLauncher(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyEnter {
		text := strings.TrimSpace(m.launcher.Value())
		if text != "" {
			m.emit("start", text, "")
			m.launching = true
		}
		return m, nil
	}
	if k.Type == tea.KeyEsc {
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.launcher, cmd = m.launcher.Update(k)
	return m, cmd
}
func (m model) updateAnswer(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyEnter {
		if m.questionIndex >= 0 && m.questionIndex < len(m.state.Questions) {
			m.emit("answer", strings.TrimSpace(m.answer.Value()), m.state.Questions[m.questionIndex].ID)
		}
		m.answerMode = false
		m.answer.Reset()
		return m, nil
	}
	if k.Type == tea.KeyEsc {
		m.answerMode = false
		return m, nil
	}
	var cmd tea.Cmd
	m.answer, cmd = m.answer.Update(k)
	return m, cmd
}
func (m model) updateSearch(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyEnter {
		m.filter = strings.TrimSpace(m.search.Value())
		m.searchMode = false
		m.resetTabScroll()
		return m, nil
	}
	if k.Type == tea.KeyEsc {
		m.searchMode = false
		return m, nil
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(k)
	return m, cmd
}
func (m model) updateCommand(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyEnter {
		m.runCommand(m.command.Value())
		m.commandMode = false
		return m, nil
	}
	if k.Type == tea.KeyEsc {
		m.commandMode = false
		return m, nil
	}
	var cmd tea.Cmd
	m.command, cmd = m.command.Update(k)
	return m, cmd
}
func (m *model) runCommand(s string) {
	f := strings.Fields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "/")))
	if len(f) == 0 {
		return
	}
	switch strings.ToLower(f[0]) {
	case "refresh", "r":
		m.emit("refresh", "", "")
	case "pause":
		m.emit("pause", "", "")
	case "resume":
		m.emit("resume", "", "")
	case "report":
		m.emit("open", reportsPath(m.state), "")
	case "quit", "q":
		m.quitDialog = true
	}
}

func (m model) compactView() string {
	w := m.width
	if w < 20 {
		w = 20
	}
	if w > 180 {
		w = 180
	}
	hs := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#202C39", Dark: "#F4F0E8"})
	ts := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#435466", Dark: "#B8C5D2"})
	if m.noColor {
		hs = lipgloss.NewStyle().Bold(true)
		ts = lipgloss.NewStyle()
	}
	pr := m.state.Snapshot.PR
	prState := strings.ToUpper(strings.TrimSpace(m.state.Snapshot.State))
	if prState != "" {
		prState = " · " + prState
	}
	line1 := hs.Render(" QUICK REVIEW ") + ts.Render(fmt.Sprintf(" · %s/%s #%d%s  %s", pr.Owner, pr.Repo, pr.Number, prState, oneLine(m.state.Snapshot.Title)))
	if pr.Number == 0 {
		line1 = hs.Render(" QUICK REVIEW ") + ts.Render(" · pull request review")
	}
	if w < 82 && pr.Number > 0 {
		line1 = hs.Render(" QUICK REVIEW ") + ts.Render(fmt.Sprintf(" · %s/%s #%d%s", pr.Owner, pr.Repo, pr.Number, prState))
	}
	if w < 36 && pr.Number > 0 {
		line1 = hs.Render(fmt.Sprintf("%s/%s #%d %s", pr.Owner, pr.Repo, pr.Number, strings.TrimPrefix(prState, " · ")))
	}
	ci := ciSummary(m.state.Snapshot.Checks)
	conn := m.state.Connection
	if strings.Contains(strings.ToLower(conn), "stale") {
		ci = "stale"
	}
	if conn == "" {
		conn = "connected"
	}
	if m.state.Error != "" {
		conn = "error: " + m.state.Error
	}
	sha := short(m.state.Snapshot.HeadSHA)
	reviewed := short(m.state.ReviewedHead)
	if reviewed == "" {
		reviewed = "not reviewed"
	} else {
		reviewed = "reviewed " + reviewed
	}
	if m.state.ReviewedHead != "" && m.state.ReviewedHead != m.state.Snapshot.HeadSHA {
		reviewed += " · STALE"
	}
	ciDot := dot(ci)
	if m.noColor {
		ciDot = "●"
	}
	line2 := ciDot + " CI " + ci + " · " + first(m.state.Phase, "Ready") + " · head " + first(sha, "—") + "   ·   " + reviewed + "   ·   " + conn
	if w < 82 {
		line2 = ciDot + " CI " + ci + " · h" + shortN(sha, 6) + " r" + shortN(m.state.ReviewedHead, 6)
		if strings.Contains(strings.ToLower(conn), "stale") {
			line2 += " · GitHub stale"
		} else {
			line2 += " · " + first(conn, "connected")
		}
	}
	if w < 50 {
		line2 = ciDot + " CI " + ci + " · " + first(m.state.Phase, "Ready")
	}
	if m.state.Paused {
		line2 += "   ·   PAUSED"
	}
	if m.state.Error != "" && w < 82 {
		line2 = oneLine(m.state.Error)
	}
	if pr.Number == 0 {
		line2 = " Paste a GitHub pull request URL to begin"
		if m.state.Error != "" {
			line2 = " Error: " + m.state.Error
		}
	}
	if m.newActivity > 0 {
		line2 += fmt.Sprintf("   ·   %d new events ↓", m.newActivity)
	}
	tabs := renderTabs(w, m.active, m.noColor)
	content := m.tabView()
	if m.launcherMode {
		content = m.launcherView()
	}
	if m.commandMode {
		content = "Command  " + m.command.View() + "\n" + content
	}
	if m.searchMode {
		content = "Filter  " + m.search.View() + "  (Enter applies · Esc cancels)\n" + content
	}
	if m.quitDialog || ((m.state.QuitRequested || m.state.ClosedPrompt) && !m.suppressQuit) {
		content = quitLine(w) + "\n" + content
	}
	lines := []string{charmansi.TruncateWc(oneLine(line1), w, "…"), charmansi.TruncateWc(oneLine(line2), w, "…"), tabs}
	lines = append(lines, wrapRows(strings.Split(content, "\n"), w)...)
	h := max(6, m.height)
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}
func (m model) launcherView() string {
	status := "Press Enter to start · Esc to exit"
	if m.state.Error != "" {
		status = "Error: " + m.state.Error + " · edit the URL and try again"
	} else if m.launching {
		status = "Connecting to the pull request…"
	}
	return "\n  Review a pull request\n\n  Enter a GitHub PR URL and Quick Review will connect to the session.\n\n  " + m.launcher.View() + "\n\n  " + status + "\n"
}
func effectiveWidth(width int) int { return max(20, min(180, width)) }
func (m model) overlayRows() int {
	n := 0
	if m.commandMode {
		n++
	}
	if m.searchMode {
		n++
	}
	if m.quitDialog || ((m.state.QuitRequested || m.state.ClosedPrompt) && !m.suppressQuit) {
		n++
	}
	return n
}
func wrapRows(rows []string, width int) []string {
	if width < 1 {
		width = 1
	}
	out := []string{}
	for _, row := range rows {
		parts := strings.Split(row, "\n")
		for _, part := range parts {
			wrapped := charmansi.WrapWc(part, width, " /.:,·")
			for _, line := range strings.Split(wrapped, "\n") {
				// WrapWc preserves unbreakable runs even when they exceed width.
				// Clip those runs so paths, hashes, and user text cannot push the
				// terminal into a wider layout than the current viewport.
				out = append(out, charmansi.TruncateWc(line, width, "…"))
			}
		}
	}
	return out
}
func wrapMappedRows(rows []string, mapping []int, width int) ([]string, []int) {
	wrapped := []string{}
	mapped := []int{}
	for i, row := range rows {
		idx := -1
		if i < len(mapping) {
			idx = mapping[i]
		}
		parts := wrapRows([]string{row}, width)
		for _, part := range parts {
			wrapped = append(wrapped, part)
			mapped = append(mapped, idx)
		}
	}
	return wrapped, mapped
}
func oneLine(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\t", " ") }
func shortN(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	end := 0
	for i, r := range s {
		next := i + len(string(r))
		if next > n {
			break
		}
		end = next
	}
	return s[:end]
}

func (m model) questionRows(width int, height int) ([]string, []int) {
	rows := []string{}
	mapping := []int{}
	if len(m.state.Questions) == 0 {
		return rows, mapping
	}
	q := m.state.Questions[max(0, min(m.questionIndex, len(m.state.Questions)-1))]
	appendRows := func(text string, option int) {
		parts := wrapRows([]string{text}, width)
		for _, part := range parts {
			rows = append(rows, part)
			mapping = append(mapping, option)
		}
	}
	appendRows("Question · "+q.Title, -1)
	if height >= 14 {
		appendRows(q.Prompt, -1)
	}
	for i, option := range q.Options {
		mark := "  "
		if m.questionFocus && i == m.questionChoice {
			mark = "› "
		}
		appendRows(mark+fmt.Sprintf("%d. %s", i+1, option), i)
	}
	if height >= 14 {
		if m.questionFocus {
			appendRows("F2/Esc leave options · Enter answers · a for freeform", -1)
		} else {
			appendRows("F2 choose an answer", -1)
		}
	}
	return rows, mapping
}
func (m model) chatGeometry(width, height int) ([]string, []int, []string, []int, int) {
	timeline := make([]string, 0, len(m.state.Events)+4)
	for _, event := range m.state.Events {
		if event.Kind == "activity" || event.Kind == "tool" {
			continue // Detailed protocol and command output remains in Activity.
		}
		timeline = append(timeline, first(event.Source, "Review")+" · "+first(event.Kind, "event")+"\n"+event.Text+"\n")
	}
	if strings.TrimSpace(m.state.DraftReply) != "" {
		timeline = append(timeline, "Codex · streaming\n"+strings.TrimSpace(m.state.DraftReply))
	}
	if len(timeline) == 0 {
		timeline = append(timeline, "No conversation yet. Ask a question below or start a review.")
	}
	timeline, mapping := wrapMappedRows(timeline, nil, width)
	qrows, qmap := m.questionRows(width, height)
	composer := wrapRows(strings.Split(m.editorView(), "\n"), width)
	answer := []string{}
	if m.answerMode {
		answer = wrapRows([]string{"Freeform answer: " + m.answer.View()}, width)
	}
	// Keep the composer visible when a long prompt or a tiny terminal would
	// otherwise push the footer below the viewport.
	contentHeight := max(0, height-3-m.overlayRows())
	footerBase := len(answer) + 1 + len(composer)
	questionLimit := max(0, contentHeight-footerBase)
	if len(qrows) > questionLimit {
		qrows = qrows[:questionLimit]
		qmap = qmap[:questionLimit]
	}
	footerUsed := footerBase + len(qrows)
	help := chatHelp(height, contentHeight, footerUsed)
	bodyHeight := max(0, contentHeight-footerUsed-len(help))
	return timeline, mapping, qrows, qmap, bodyHeight
}

func chatHelp(height, contentHeight, used int) []string {
	help := []string{}
	if used < contentHeight {
		help = append(help, "Enter sends · Alt+Enter newline · Ctrl+P commands")
		used++
	}
	if height >= 14 && used < contentHeight {
		help = append(help, "↑/↓ scroll · PgUp/PgDown page · new activity appears in header")
	}
	return help
}
func (m model) chatView() string {
	width := effectiveWidth(m.width)
	height := max(6, m.height)
	timeline, _, qrows, _, bodyHeight := m.chatGeometry(width, height)
	end := max(0, len(timeline)-m.scroll)
	start := max(0, end-bodyHeight)
	body := append([]string(nil), timeline[start:end]...)
	for len(body) < bodyHeight {
		body = append(body, "")
	}
	footer := append([]string(nil), qrows...)
	if m.answerMode {
		footer = append(footer, wrapRows([]string{"Freeform answer: " + m.answer.View()}, width)...)
	}
	footer = append(footer, "")
	footer = append(footer, wrapRows(strings.Split(m.editorView(), "\n"), width)...)
	contentHeight := max(0, height-3-m.overlayRows())
	footer = append(footer, wrapRows(chatHelp(height, contentHeight, len(footer)), width)...)
	lines := append(body, footer...)
	return strings.Join(lines, "\n")
}

func (m model) tabView() string {
	if m.active == chatTab {
		return m.chatView()
	}
	rows := []string{}
	switch m.active {
	case changesTab:
		rows = append(rows, "Changed files · Enter advances selection · diff follows")
		for i, f := range m.state.Snapshot.Files {
			p := " "
			if i == m.selectedFile {
				p = "›"
			}
			rows = append(rows, fmt.Sprintf("%s %s  +%d −%d", p, f.Path, f.Additions, f.Deletions))
		}
		if len(m.state.Snapshot.Files) == 0 {
			rows = append(rows, "No changed files available.")
		}
		rows = append(rows, "", m.state.Diff)
	case checksTab:
		rows, _ = m.checkRows()
	case agentsTab:
		rows = append(rows, "Review agents")
		for _, a := range m.state.Agents {
			rows = append(rows, fmt.Sprintf("%s · %s · %s", a.Name, a.Status, a.Scope))
			if a.Detail != "" {
				rows = append(rows, "    "+a.Detail)
			}
		}
		if len(m.state.Agents) == 0 {
			rows = append(rows, "No agents have reported yet.")
		}
	case reportTab:
		rows, _ = m.reportRows()
	case activityTab:
		rows, _ = m.activityRows()
	}
	rows = wrapRows(rows, effectiveWidth(m.width))
	visible := max(0, m.height-5-m.overlayRows())
	end := max(0, len(rows)-m.scroll)
	start := max(0, end-visible)
	return strings.Join(rows[start:end], "\n") + "\n\n↑/↓ scroll · PgUp/PgDown page · Ctrl+P commands · q quit"
}
func (m model) reportRows() ([]string, []int) {
	rows := []string{"Live review report · Enter or click opens Markdown file"}
	mapping := []int{-1}
	if len(m.state.Reports) == 0 {
		rows = append(rows, "No report has been generated yet.")
		mapping = append(mapping, -1)
		return rows, mapping
	}
	// State.Reports may contain entries from older sessions; only its latest
	// report represents the live report shown by the UI.
	i := len(m.state.Reports) - 1
	r := m.state.Reports[i]
	stamp := ""
	if !r.CreatedAt.IsZero() {
		stamp = r.CreatedAt.Format("2006-01-02 15:04")
	}
	flag := "current"
	if r.Stale {
		flag = "stale"
	}
	phase := strings.ToLower(m.state.Phase)
	progress := ""
	if r.InProgress || (strings.Contains(phase, "review") && (strings.Contains(phase, "progress") || strings.Contains(phase, "running") || strings.Contains(phase, "reviewing"))) {
		progress = " · review in progress"
	}
	extra := []string{fmt.Sprintf("Report · %s · %s%s", stamp, flag, progress), "head " + short(r.HeadSHA) + " · base " + short(r.BaseSHA), "", renderReportMarkdown(r.Text, effectiveWidth(m.width))}
	if r.Path != "" {
		extra = append(extra, "", "File: "+r.Path)
	}
	for _, line := range extra {
		rows = append(rows, line)
		mapping = append(mapping, i)
	}
	return rows, mapping
}

func (m model) activityRows() ([]string, []int) {
	rows := []string{"Activity · / search · s cycle sources · Enter or click for details"}
	mapping := []int{-1}
	if m.filter != "" || m.sourceFilter != "" {
		rows = append(rows, "Filter: "+m.filter+"  source: "+first(m.sourceFilter, "all")+" (Esc clears search)")
		mapping = append(mapping, -1)
	}
	events := m.filteredEvents()
	for i, e := range events {
		mark := " "
		if i == m.selectedEvent {
			mark = "›"
		}
		rows = append(rows, mark+" "+formatEvent(e))
		mapping = append(mapping, i)
		if m.eventDetail && e.ID == m.selectedEventID() {
			if e.Detail != "" {
				detail := strings.Split(strings.ReplaceAll(e.Detail, "\n", "\n    "), "\n")
				for _, line := range detail {
					rows = append(rows, "    "+line)
					mapping = append(mapping, i)
				}
			}
			if e.SHA != "" {
				rows = append(rows, "    revision "+e.SHA)
				mapping = append(mapping, i)
			}
		}
	}
	if len(events) == 0 {
		rows = append(rows, "No matching activity.")
		mapping = append(mapping, -1)
	}
	return rows, mapping
}
func (m model) checkRows() ([]string, []int) {
	rows := []string{"Checks · Enter or click opens the check URL"}
	mapping := []int{-1}
	for i, c := range m.state.Snapshot.Checks {
		mark := " "
		if i == m.selectedFile {
			mark = "›"
		}
		rows = append(rows, fmt.Sprintf("%s %s  %s", mark, checkIcon(c.State), c.Name))
		mapping = append(mapping, i)
		if c.CompletedAt != "" {
			rows = append(rows, "    completed "+c.CompletedAt)
			mapping = append(mapping, i)
		} else if c.StartedAt != "" {
			rows = append(rows, "    running since "+c.StartedAt)
			mapping = append(mapping, i)
		}
	}
	if len(m.state.Snapshot.Checks) == 0 {
		rows = append(rows, "No checks reported yet.")
		mapping = append(mapping, -1)
	}
	return rows, mapping
}

func (m model) filteredEvents() []domain.Event {
	out := []domain.Event{}
	q := strings.ToLower(m.filter)
	for _, e := range m.state.Events {
		line := strings.ToLower(e.Source + " " + e.Kind + " " + e.Text + " " + e.Detail)
		if (q == "" || strings.Contains(line, q)) && (m.sourceFilter == "" || strings.EqualFold(e.Source, m.sourceFilter)) {
			out = append(out, e)
		}
	}
	return out
}
func (m model) visibleEventIndex(row int) int {
	e := m.filteredEvents()
	if row >= 0 && row < len(e) {
		return row
	}
	return -1
}
func (m model) selectedEventID() int {
	events := m.filteredEvents()
	if len(events) == 0 {
		return -1
	}
	return events[max(0, min(m.selectedEvent, len(events)-1))].ID
}
func formatEvent(e domain.Event) string {
	stamp := ""
	if !e.Time.IsZero() {
		stamp = e.Time.Local().Format("15:04:05")
	}
	src := e.Source
	if src == "" {
		src = "review"
	}
	kind := e.Kind
	if kind == "" {
		kind = "event"
	}
	return fmt.Sprintf("%s  %-10s  %-12s  %s", stamp, src, kind, e.Text)
}
func (m model) scrollBy(n int) model {
	m.scroll = max(0, m.scroll-n)
	if m.scrollByTab == nil {
		m.scrollByTab = map[tab]int{}
	}
	m.scrollByTab[m.active] = m.scroll
	m.follow = m.scroll == 0
	if m.follow {
		m.newActivity = 0
	}
	return m
}
func tabLabels(width int) []string {
	labels := make([]string, len(tabNames))
	narrow := width < 82
	abbreviations := []string{"C", "D", "K", "A", "R", "T"}
	for i, name := range tabNames {
		if width < 36 {
			labels[i] = fmt.Sprintf("%d%s", i+1, abbreviations[i])
		} else if narrow {
			labels[i] = fmt.Sprintf(" %d%s ", i+1, abbreviations[i])
		} else {
			labels[i] = fmt.Sprintf(" %d %s ", i+1, name)
		}
	}
	return labels
}
func quitLine(width int) string {
	if width < 36 {
		return "y keep d rm n stay"
	}
	if width < 82 {
		return "Quit? [y]keep [d]remove [n]watch"
	}
	return "Quit session?  [y] keep checkouts   [d] remove clean checkouts   [n] keep watching"
}
func (m *model) resetTabScroll() {
	m.scroll = 0
	if m.scrollByTab == nil {
		m.scrollByTab = map[tab]int{}
	}
	m.scrollByTab[m.active] = 0
	m.follow = true
}
func renderTabs(width int, active tab, noColor bool) string {
	labels := tabLabels(width)
	for i, label := range labels {
		if i == int(active) {
			if noColor {
				labels[i] = ">" + label[1:]
			} else {
				labels[i] = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#315D67")).Render(label)
			}
		} else if !noColor {
			labels[i] = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#435466", Dark: "#9BA9B5"}).Render(label)
		}
	}
	return strings.Join(labels, " ")
}
func tabAt(x, width int) int {
	if x < 0 || width <= 0 {
		return -1
	}
	pos := 0
	for i, label := range tabLabels(width) {
		if x >= pos && x < pos+len(label) {
			return i
		}
		pos += len(label) + 1
	}
	return -1
}
func (m *model) setTab(next tab) {
	if m.scrollByTab == nil {
		m.scrollByTab = map[tab]int{}
	}
	m.scrollByTab[m.active] = m.scroll
	_, remembered := m.scrollByTab[next]
	m.active = next
	m.scroll = m.scrollByTab[next]
	if next == reportTab && !remembered {
		m.setReportTop(0)
	}
	m.follow = m.scroll == 0
}

func (m model) reportTop() int {
	width, visible := m.reportViewport()
	layout := m
	layout.width = width
	rows, _ := layout.reportRows()
	wrapped := wrapRows(rows, width)
	return max(0, len(wrapped)-m.scroll-visible)
}

func (m *model) setReportTop(top int) {
	width, visible := m.reportViewport()
	layout := *m
	layout.width = width
	rows, _ := layout.reportRows()
	wrapped := wrapRows(rows, width)
	top = max(0, min(top, max(0, len(wrapped)-visible)))
	m.scroll = max(0, len(wrapped)-visible-top)
	if m.scrollByTab == nil {
		m.scrollByTab = map[tab]int{}
	}
	m.scrollByTab[reportTab] = m.scroll
}

func (m model) reportViewport() (width, visible int) {
	width, height := effectiveWidth(m.width), m.height
	if !m.framed && m.dashboard() && !m.launcherMode {
		_, main, _, _, content := m.dashboardSize()
		width, height = effectiveWidth(main-4), content+3
	}
	return width, max(0, height-5-m.overlayRows())
}

func (m *model) emit(kind, text, id string) {
	if m.dispatch != nil {
		m.dispatch(domain.Action{Kind: kind, Text: text, ID: id})
	}
}
func (m *model) openReport() {
	if len(m.state.Reports) > 0 {
		m.emit("open", m.state.Reports[len(m.state.Reports)-1].Path, "")
	}
}
func (m *model) openCheck() {
	if len(m.state.Snapshot.Checks) > 0 {
		c := m.state.Snapshot.Checks[max(0, min(m.selectedFile, len(m.state.Snapshot.Checks)-1))]
		if c.URL != "" {
			m.emit("open", c.URL, "")
		}
	}
}
func ciSummary(checks []domain.Check) string {
	if len(checks) == 0 {
		return "pending"
	}
	pending, failed := false, false
	for _, c := range checks {
		switch strings.ToLower(c.State) {
		case "success", "passed", "completed":
		case "failure", "failed", "error", "cancelled", "timed_out":
			failed = true
		default:
			pending = true
		}
	}
	if failed {
		return "failing"
	}
	if pending {
		return "running"
	}
	return "passing"
}
func checkIcon(s string) string {
	switch strings.ToLower(s) {
	case "success", "passed", "completed":
		return "✓"
	case "failure", "failed", "error", "cancelled", "timed_out":
		return "×"
	case "in_progress", "running", "queued", "pending":
		return "◷"
	default:
		return "·"
	}
}
func dot(s string) string {
	c := "#D6A84F"
	switch s {
	case "passing":
		c = "#68C69A"
	case "failing":
		c = "#E77979"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●")
}
func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func reportsPath(s domain.State) string {
	if len(s.Reports) > 0 {
		return s.Reports[len(s.Reports)-1].Path
	}
	return ""
}
