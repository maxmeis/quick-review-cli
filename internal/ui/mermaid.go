package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/coreequip/mermaidascii"
)

// The original Markdown remains untouched on disk. Render only fenced Mermaid,
// retaining source so limitations of terminal graph layout never hide evidence.
func renderReportMarkdown(text string, width int) string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		fence := strings.TrimSpace(lines[i])
		marker := ""
		if strings.HasPrefix(fence, "```") {
			marker = "`"
		}
		if strings.HasPrefix(fence, "~~~") {
			marker = "~"
		}
		count := len(fence) - len(strings.TrimLeft(fence, marker))
		if marker == "" {
			out = append(out, lines[i])
			continue
		}
		end := i + 1
		for end < len(lines) {
			closing := strings.TrimSpace(lines[end])
			if len(closing) >= count && strings.Trim(closing, marker) == "" {
				break
			}
			end++
		}
		if end == len(lines) {
			out = append(out, lines[i:]...)
			break
		}
		if strings.TrimSpace(fence[count:]) != "mermaid" {
			out = append(out, lines[i:end+1]...)
			i = end
			continue
		}
		src := strings.Join(lines[i+1:end], "\n")
		preview, reason := mermaidPreview(src, width)
		if reason == "" {
			out = append(out, "Mermaid · terminal preview (layout may simplify shapes/labels)", preview, "Mermaid source:")
		} else {
			out = append(out, "Mermaid · "+reason+" · source:")
		}
		out = append(out, lines[i:end+1]...)
		i = end
	}
	return strings.Join(out, "\n")
}

var drawMermaid = func(src string) (string, error) {
	return mermaidascii.Render(src, mermaidascii.WithSpacing(3, 2), mermaidascii.WithPadding(0))
}

func mermaidPreview(src string, width int) (preview, reason string) {
	defer func() {
		if recover() != nil {
			preview, reason = "", "diagram could not be rendered"
		}
	}()
	if len(src) > 4096 || strings.Count(src, "\n") > 80 || strings.Count(src, ">") > 80 {
		return "", "diagram too large for inline rendering"
	}
	fields := strings.Fields(src)
	if len(fields) == 0 {
		return "", "empty diagram"
	}
	if fields[0] != "graph" && fields[0] != "flowchart" && fields[0] != "sequenceDiagram" {
		return "", "unsupported diagram type"
	}
	// The library skips these directives; show source rather than silently omit them.
	for _, line := range strings.Split(src, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && (f[0] == "subgraph" || f[0] == "classDef" || f[0] == "style" || f[0] == "click") {
			return "", "unsupported diagram directive"
		}
	}
	rendered, err := drawMermaid(src)
	if err != nil {
		return "", "diagram could not be rendered"
	}
	for _, line := range strings.Split(rendered, "\n") {
		if ansi.StringWidth(line) > width {
			return "", "diagram wider than this pane; enlarge terminal"
		}
	}
	return strings.TrimRight(rendered, "\n"), ""
}
