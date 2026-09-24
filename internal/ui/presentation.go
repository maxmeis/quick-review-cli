package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	charmansi "github.com/charmbracelet/x/ansi"
)

var severityMarker = regexp.MustCompile(`\bP[012]\b`)

// decorateContent adds restrained color to content lines while preserving every
// visible character, row, and cell width. Layout and wrapping remain the job of
// the caller, so decoration is safe to apply after content has been composed.
func decorateContent(text string, active tab, noColor bool) string {
	if noColor {
		return charmansi.Strip(text)
	}
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = decorateLine(line, active)
	}
	return strings.Join(lines, "\n")
}

func decorateLine(line string, active tab) string {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return line
	}

	var styled string
	switch {
	case strings.HasPrefix(trimmed, "+") && !strings.HasPrefix(trimmed, "+++"):
		styled = adaptive("#177245", "#8BE0AE").Render(line)
	case strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "---"):
		styled = adaptive("#B42332", "#F39AA5").Render(line)
	case strings.HasPrefix(trimmed, "@@"):
		styled = adaptive("#7551A6", "#C9A8F5").Render(line)
	case markdownHeading(trimmed):
		styled = adaptive("#315D67", "#89DCE2").Bold(true).Render(line)
	case actorHeading(trimmed):
		styled = adaptive("#315D67", "#89DCE2").Bold(true).Render(line)
	case sectionHeading(trimmed):
		styled = activeHeadingStyle(active).Render(line)
	case checkSucceeded(trimmed):
		styled = adaptive("#177245", "#8BE0AE").Render(line)
	case checkFailed(trimmed):
		styled = adaptive("#B42332", "#F39AA5").Render(line)
	default:
		styled = line
	}

	return severityMarker.ReplaceAllStringFunc(styled, func(marker string) string {
		switch marker {
		case "P0":
			return adaptive("#A31325", "#FF707D").Bold(true).Render(marker)
		case "P1":
			return adaptive("#A44C00", "#FFC078").Bold(true).Render(marker)
		default:
			return adaptive("#7551A6", "#C9A8F5").Bold(true).Render(marker)
		}
	})
}

func adaptive(light, dark string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: light, Dark: dark})
}

func activeHeadingStyle(active tab) lipgloss.Style {
	// The current tab supplies a subtle hue shift to section labels, while the
	// adaptive light/dark pair keeps contrast readable in both terminal themes.
	colors := [...]lipgloss.AdaptiveColor{
		{Light: "#6548AA", Dark: "#C6B5FF"},
		{Light: "#176B70", Dark: "#83DDE0"},
		{Light: "#177245", Dark: "#8BE0AE"},
		{Light: "#315D67", Dark: "#89DCE2"},
		{Light: "#7551A6", Dark: "#C9A8F5"},
		{Light: "#315D67", Dark: "#89DCE2"},
	}
	idx := int(active)
	if idx < 0 || idx >= len(colors) {
		idx = 0
	}
	return lipgloss.NewStyle().Foreground(colors[idx]).Bold(true)
}

func markdownHeading(line string) bool {
	return strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ")
}

func actorHeading(line string) bool {
	for _, actor := range []string{"You ·", "Codex ·", "Agent ·", "Agents ·", "GitHub ·", "App ·", "CI ·"} {
		if strings.HasPrefix(line, actor) {
			return true
		}
	}
	return false
}

func sectionHeading(line string) bool {
	for _, heading := range []string{
		"Changed files", "Checks", "Review agents", "Review reports", "Activity",
		"Review findings", "Live review report", "Style and maintainability", "Questions", "Suggested checks",
	} {
		if line == heading || strings.HasPrefix(line, heading+" ·") || strings.HasPrefix(line, heading+"  ") {
			return true
		}
	}
	return false
}

func checkSucceeded(line string) bool {
	return strings.Contains(line, "✓") || strings.Contains(line, "● success") || strings.Contains(line, "● passed")
}

func checkFailed(line string) bool {
	return strings.Contains(line, "✗") || strings.Contains(line, "● failed") || strings.Contains(line, "● failure") || strings.Contains(line, "● error")
}
