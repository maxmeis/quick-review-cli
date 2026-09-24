//go:build e2e && !windows

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"quick-review-cli/internal/domain"
)

type terminal struct {
	t       *testing.T
	cmd     *exec.Cmd
	file    *os.File
	mu      sync.Mutex
	output  bytes.Buffer
	done    chan error
	backend *backend
}

func buildCLI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "review")
	cmd := exec.Command("go", "build", "-race", "-o", binary, "./cmd/review")
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return binary
}

func launch(t *testing.T, b *backend, binary string, args ...string) *terminal {
	t.Helper()
	args = append([]string{"--data-dir", filepath.Join(b.Root, "sessions"), "--poll", "5s"}, args...)
	cmd := exec.Command(binary, args...)
	cmd.Env = append(b.env(), "TERM=xterm-256color", "NO_COLOR=1", "GORACE=atexit_sleep_ms=0")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 120, Rows: 40})
	if err != nil {
		t.Fatal(err)
	}
	p := &terminal{t: t, cmd: cmd, file: f, done: make(chan error, 1), backend: b}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 8192)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.output.Write(buf[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = f.Close()
		<-readDone
		if dir := os.Getenv("E2E_ARTIFACT_DIR"); dir != "" {
			_ = os.MkdirAll(dir, 0755)
			name := strings.ReplaceAll(t.Name(), "/", "-")
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.ansi", name, cmd.Process.Pid)), []byte(p.raw()), 0600)
		}
		if t.Failed() {
			t.Logf("terminal transcript:\n%s", p.text())
		}
	})
	// Bubble Tea must have put the terminal into raw mode before keys are sent.
	p.waitText("QUICK REVIEW")
	return p
}
func (p *terminal) raw() string  { p.mu.Lock(); defer p.mu.Unlock(); return p.output.String() }
func (p *terminal) text() string { return ansi.Strip(p.raw()) }
func (p *terminal) send(keys string) {
	p.t.Helper()
	if _, err := io.WriteString(p.file, keys); err != nil {
		p.t.Fatal(err)
	}
}
func (p *terminal) command(s string) {
	p.send("\x10")
	time.Sleep(50 * time.Millisecond)
	p.send(s + "\r")
}
func (p *terminal) tab(n int) { p.send(fmt.Sprintf("\x1b%d", n)); time.Sleep(80 * time.Millisecond) }
func (p *terminal) waitText(s string) {
	p.t.Helper()
	eventually(p.t, func() bool { return strings.Contains(p.text(), s) }, "terminal text: "+s)
}
func eventually(t *testing.T, fn func() bool, label string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}
func (p *terminal) state() domain.State {
	paths, _ := filepath.Glob(filepath.Join(p.backend.Root, "sessions", "*", "state.json"))
	var s domain.State
	if len(paths) > 0 {
		data, _ := os.ReadFile(paths[0])
		_ = json.Unmarshal(data, &s)
	}
	return s
}
func (p *terminal) waitState(label string, fn func(domain.State) bool) domain.State {
	p.t.Helper()
	var s domain.State
	eventually(p.t, func() bool { s = p.state(); return fn(s) }, label)
	return s
}
func control(t *testing.T, b *backend, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(b.Root, name), []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
}
func removeControl(t *testing.T, b *backend, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(b.Root, name)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
func read(path string) string { b, _ := os.ReadFile(path); return string(b) }
func (p *terminal) exit(key string) {
	p.t.Helper()
	p.command("quit")
	p.waitText("Keep checkouts")
	p.send(key)
	select {
	case err := <-p.done:
		if err != nil {
			p.t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		p.t.Fatal("app failed to exit")
	}
}

func TestE2EFullReviewJourney(t *testing.T) {
	b := newBackend(t)
	binary := buildCLI(t)
	p := launch(t, b, binary)
	p.send("https://github.com/acme/widget/pull/1\r")
	s := p.waitState("review checkout and reviewers", func(s domain.State) bool { return s.ReviewedHead == b.Head && len(s.Agents) > 0 })
	if s.Snapshot.PR.Number != 1 || !strings.Contains(s.Diff, "feature.txt") {
		t.Fatalf("bad initial snapshot: %+v", s)
	}
	// Every tab is exercised through the terminal's actual key parser.
	for n, marker := range map[int]string{2: "feature.txt", 3: "Unit tests", 4: "reviewer", 6: "Activity"} {
		p.tab(n)
		p.waitText(marker)
	}
	// Mouse click Chat, then user input flows through the real JSONL protocol.
	p.send("\x1b[<0;4;5M\x1b[<0;4;5m")
	p.send("Please inspect error handling\r")
	eventually(t, func() bool {
		return strings.Contains(read(filepath.Join(b.Root, "codex.jsonl")), "Please inspect error handling")
	}, "chat delivered to Codex")
	control(t, b, "question")
	p.waitState("question request", func(s domain.State) bool { return len(s.Questions) > 0 })
	p.waitText("Focus on correctness?")
	p.send("\x1bOQ")
	time.Sleep(80 * time.Millisecond)
	p.send("\r")
	p.waitState("question answered", func(s domain.State) bool { return len(s.Questions) == 0 })
	// Automatic watcher tick, without requesting a refresh.
	b.snapshot(t, b.Head, "OPEN", "SUCCESS")
	p.waitState("automatic CI update", func(s domain.State) bool {
		return len(s.Snapshot.Checks) > 0 && s.Snapshot.Checks[0].State == "SUCCESS"
	})
	eventually(t, func() bool { return strings.Contains(read(filepath.Join(b.Root, "codex.jsonl")), "WATCHER UPDATE") }, "watcher steering")
	control(t, b, "complete")
	s = p.waitState("first finalized report", func(s domain.State) bool { return len(s.Reports) == 1 && !s.Reports[0].InProgress })
	reportPath := s.Reports[0].Path
	originalCheckout := filepath.Join(s.SessionDir, "checkouts", b.Head+"-"+b.Base)
	if !strings.HasSuffix(reportPath, "reports/report.md") || !strings.Contains(read(reportPath), "```mermaid") {
		t.Fatalf("wrong report %s", reportPath)
	}
	p.tab(5)
	p.waitText("terminal preview")
	p.waitText("Push")
	p.send("\r")
	eventually(t, func() bool { return strings.Contains(read(filepath.Join(b.Root, "opened.log")), reportPath) }, "open canonical report")
	eventually(t, func() bool { return strings.Contains(read(filepath.Join(b.Root, "notifications.log")), "acme/widget") }, "repository notification")
	// Hold the second review; keep the old report visible and explicitly stale.
	removeControl(t, b, "complete")
	p.command("pause")
	p.waitState("paused", func(s domain.State) bool { return s.Paused })
	b.advance(t)
	p.command("refresh")
	p.waitState("new push queued", func(s domain.State) bool { return s.Snapshot.HeadSHA == b.Next && s.Reports[0].Stale })
	if got := p.state().Reports[0].Path; got != reportPath {
		t.Fatal("canonical path changed")
	}
	p.command("resume")
	p.waitState("new exact checkout", func(s domain.State) bool { return s.ReviewedHead == b.Next })
	control(t, b, "complete")
	s = p.waitState("report replaced", func(s domain.State) bool {
		return len(s.Reports) == 1 && s.Reports[0].HeadSHA == b.Next && !s.Reports[0].InProgress && !s.Reports[0].Stale
	})
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(reportPath), "*.md"))
	if len(files) != 1 {
		t.Fatalf("report history created: %v", files)
	}
	if !strings.Contains(read(reportPath), b.Next) {
		t.Fatal("file not updated to new revision")
	}
	for dir, want := range map[string]string{originalCheckout: b.Head, filepath.Join(s.SessionDir, "checkouts", b.Next+"-"+b.Base): b.Next} {
		got, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil || strings.TrimSpace(string(got)) != want {
			t.Fatalf("checkout %s moved or missing: %s %v", dir, got, err)
		}
	}
	// Network failure and recovery retain the current report.
	control(t, b, "gh-error")
	p.command("refresh")
	p.waitState("GitHub failure", func(s domain.State) bool { return strings.Contains(s.Connection, "stale") })
	removeControl(t, b, "gh-error")
	p.command("refresh")
	p.waitState("GitHub recovery", func(s domain.State) bool { return s.Connection == "Connected" })
	sessionDir := s.SessionDir
	p.exit("y")
	// Resume the same persisted session and Codex thread.
	resumed := launch(t, b, binary, "--resume", sessionDir)
	resumed.waitState("resumed session", func(s domain.State) bool { return s.ReviewedHead == b.Next && s.Connection == "Connected" })
	eventually(t, func() bool { return strings.Contains(read(filepath.Join(b.Root, "codex.jsonl")), "thread/resume") }, "thread resumed")
	b.snapshot(t, b.Next, "CLOSED", "SUCCESS")
	resumed.command("refresh")
	resumed.waitState("closed prompt", func(s domain.State) bool { return s.ClosedPrompt })
	resumed.send("n")
	resumed.waitState("keep watching", func(s domain.State) bool { return !s.ClosedPrompt })
	b.snapshot(t, b.Next, "MERGED", "SUCCESS")
	resumed.command("refresh")
	resumed.waitState("merged prompt", func(s domain.State) bool { return s.ClosedPrompt && s.Snapshot.State == "MERGED" })
	resumed.send("d")
	select {
	case err := <-resumed.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("merge cleanup failed")
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "checkouts")); !os.IsNotExist(err) {
		t.Fatalf("checkouts retained: %v", err)
	}
	if read(reportPath) == "" {
		t.Fatal("cleanup removed report")
	}
}

func TestE2EDisconnectRecoveryAndDirtyCleanup(t *testing.T) {
	b := newBackend(t)
	p := launch(t, b, buildCLI(t), "https://github.com/acme/widget/pull/1")
	s := p.waitState("partial report", func(s domain.State) bool { return len(s.Reports) == 1 && s.Reports[0].InProgress })
	reportPath := s.Reports[0].Path
	control(t, b, "disconnect")
	p.waitState("Codex disconnected", func(s domain.State) bool { return strings.Contains(s.Connection, "Codex disconnected") })
	if read(reportPath) == "" {
		t.Fatal("disconnect lost partial report")
	}
	removeControl(t, b, "disconnect")
	p.command("resume")
	p.waitState("Codex reconnected", func(s domain.State) bool { return s.Connection == "Connected" })
	control(t, b, "complete")
	s = p.waitState("recovered report", func(s domain.State) bool { return len(s.Reports) == 1 && !s.Reports[0].InProgress })
	if s.Reports[0].Path != reportPath {
		t.Fatal("recovery created another report")
	}
	p.tab(5)
	if err := pty.Setsize(p.file, &pty.Winsize{Cols: 40, Rows: 20}); err != nil {
		t.Fatal(err)
	}
	p.waitText("Live review report")
	if err := pty.Setsize(p.file, &pty.Winsize{Cols: 120, Rows: 40}); err != nil {
		t.Fatal(err)
	}
	dirs, err := os.ReadDir(filepath.Join(s.SessionDir, "checkouts"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no checkouts: %v", err)
	}
	dirty := filepath.Join(s.SessionDir, "checkouts", dirs[0].Name(), "user-work.txt")
	if err := os.WriteFile(dirty, []byte("keep my edits"), 0600); err != nil {
		t.Fatal(err)
	}
	p.command("quit")
	p.waitText("Keep checkouts")
	p.send("d")
	p.waitState("dirty checkout protected", func(s domain.State) bool {
		return strings.Contains(s.Error, "local changes") || strings.Contains(s.Error, "uncommitted")
	})
	if read(dirty) != "keep my edits" {
		t.Fatal("cleanup discarded edits")
	}
	if err := os.Remove(dirty); err != nil {
		t.Fatal(err)
	}
	p.exit("d")
	if read(reportPath) == "" {
		t.Fatal("cleanup discarded report")
	}
}
