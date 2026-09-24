package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	charmansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestDecorateContentAddsStylesWithoutChangingLayout(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	oldDark := lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	defer lipgloss.SetColorProfile(oldProfile)
	defer lipgloss.SetHasDarkBackground(oldDark)

	plain := strings.Join([]string{
		"Checks · Enter opens URL",
		"  ✓ unit tests",
		"  ✗ deploy failed",
		"# Review findings",
		"P0 data loss; P1 retry; P2 polish",
		"You · message",
		"+added",
		"-removed",
		"@@ -1 +1 @@",
		"ordinary body text",
		"",
	}, "\n")
	got := decorateContent(plain, checksTab, false)
	if strings.Count(got, "\n") != strings.Count(plain, "\n") {
		t.Fatalf("decoration changed row count: %q", got)
	}
	if stripped := charmansi.Strip(got); stripped != plain {
		t.Fatalf("decoration changed displayed text\nwant: %q\n got: %q", plain, stripped)
	}
	plainRows, styledRows := strings.Split(plain, "\n"), strings.Split(got, "\n")
	for i := range plainRows {
		if want, actual := charmansi.StringWidth(plainRows[i]), charmansi.StringWidth(styledRows[i]); want != actual {
			t.Errorf("row %d width changed from %d to %d: %q", i, want, actual, styledRows[i])
		}
	}
	if !strings.Contains(got, "\x1b[") {
		t.Fatal("styled render contains no terminal styling")
	}
}

func TestDecorateContentNoColorOnlyStripsANSI(t *testing.T) {
	input := "\x1b[31mP1 issue\x1b[0m\n\x1b]8;;https://example.test\aopen\x1b]8;;\a"
	want := charmansi.Strip(input)
	if got := decorateContent(input, reportTab, true); got != want {
		t.Fatalf("no-color output = %q, want stripped input %q", got, want)
	}
	if got := decorateContent("", chatTab, true); got != "" {
		t.Fatalf("empty no-color input = %q", got)
	}
	if got := decorateContent("", chatTab, false); got != "" {
		t.Fatalf("empty styled input = %q", got)
	}
}

func TestDecorationCategoriesAndAdaptiveActiveHeading(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	oldDark := lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(false)
	defer lipgloss.SetColorProfile(oldProfile)
	defer lipgloss.SetHasDarkBackground(oldDark)

	for _, row := range []string{
		"Changed files · Enter advances",
		"Review agents",
		"Live review report",
		"Activity · search",
		"Style and maintainability",
		"Questions · pending",
		"Suggested checks",
		"## Markdown title",
		"Agents · concurrency reviewer",
		"GitHub · push detected",
		"App · checkout ready",
		"CI · passing",
		"● success build",
		"● passed tests",
		"● failed build",
		"● failure tests",
		"● error tool",
		"› P2: maintainability",
	} {
		styled := decorateContent(row, activityTab, false)
		if charmansi.Strip(styled) != row {
			t.Errorf("row changed after decoration: %q -> %q", row, charmansi.Strip(styled))
		}
		if !strings.Contains(styled, "\x1b[") {
			t.Errorf("expected style for %q, got %q", row, styled)
		}
	}
	if got := decorateContent("Activity", tab(999), false); charmansi.Strip(got) != "Activity" {
		t.Fatalf("invalid active tab changed text: %q", got)
	}
}

func TestDecorationDiffPrefixesAndUnicodeWidth(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(oldProfile)
	for _, row := range []string{"  + 新しい行", "  - deleted", "+++ header", "--- header", "@@ hunk @@", "plain P0x is not severity"} {
		got := decorateContent(row, changesTab, false)
		if charmansi.StringWidth(got) != charmansi.StringWidth(row) {
			t.Errorf("width changed for %q: got %d want %d", row, charmansi.StringWidth(got), charmansi.StringWidth(row))
		}
		if charmansi.Strip(got) != row {
			t.Errorf("text changed for %q: %q", row, charmansi.Strip(got))
		}
	}
}
