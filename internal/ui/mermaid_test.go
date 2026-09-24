package ui

import (
	"errors"
	"strings"
	"testing"
)

func TestMermaidReportRendering(t *testing.T) {
	for _, fence := range []string{"```", "~~~~"} {
		source := "Before\n" + fence + "mermaid\nflowchart TD\n A[Push] --> B[Review]\n" + fence + "\nAfter"
		got := renderReportMarkdown(source, 100)
		for _, want := range []string{"terminal preview", "Push", "Review", "┌", "Mermaid source:", source[strings.Index(source, fence):]} {
			if !strings.Contains(got, want) {
				t.Fatalf("missing %q in %s", want, got)
			}
		}
	}
	for _, source := range []string{"````text\n```mermaid\ngraph TD\nA --> B\n```\n````", "plain", "```go\nhello\n```", "```mermaid\ngraph TD\nA --> B"} {
		if got := renderReportMarkdown(source, 80); got != source {
			t.Fatalf("changed ordinary/incomplete source: %q", got)
		}
	}
	got := renderReportMarkdown("```mermaid\npie\n title Pie\n```", 80)
	if !strings.Contains(got, "unsupported diagram type") || !strings.Contains(got, "title Pie") {
		t.Fatal(got)
	}
}

func TestMermaidPreviewLimits(t *testing.T) {
	for _, tt := range []struct {
		source, reason string
		width          int
	}{
		{strings.Repeat("a", 4097), "too large", 80},
		{strings.Repeat("\n", 82), "too large", 80},
		{" ", "empty", 80},
		{"pie\n title Example", "unsupported diagram type", 80},
		{"flowchart TD\nsubgraph A\nend", "unsupported diagram directive", 80},
		{"graph LR\nclassDef a fill:red", "unsupported diagram directive", 80},
		{"graph LR\nstyle A fill:red", "unsupported diagram directive", 80},
		{"graph LR\nclick A url", "unsupported diagram directive", 80},
		{"graph LR\nA --> B", "wider", 1},
	} {
		_, reason := mermaidPreview(tt.source, tt.width)
		if !strings.Contains(reason, tt.reason) {
			t.Fatalf("%q: %s", tt.source, reason)
		}
	}
	for _, src := range []string{"graph TD\n\nA --> B", "sequenceDiagram\nparticipant A\nparticipant B\nA->>B: Hello"} {
		got, reason := mermaidPreview(src, 100)
		if reason != "" || got == "" {
			t.Fatalf("%s: %s", got, reason)
		}
	}
	old := drawMermaid
	t.Cleanup(func() { drawMermaid = old })
	drawMermaid = func(string) (string, error) { return "", errors.New("invalid graph") }
	if _, reason := mermaidPreview("graph TD", 80); reason != "diagram could not be rendered" {
		t.Fatal(reason)
	}
}

func TestMermaidRendererPanic(t *testing.T) {
	old := drawMermaid
	t.Cleanup(func() { drawMermaid = old })
	drawMermaid = func(string) (string, error) { panic("bad diagram") }
	if out, reason := mermaidPreview("graph TD", 80); out != "" || reason != "diagram could not be rendered" {
		t.Fatalf("%q %q", out, reason)
	}
}
