//go:build e2e && !windows

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// backend is a local GitHub/Codex installation for the real binary E2E tests.
// Root is its private control and observation directory; Bin contains command
// shims that re-enter this test binary as TestMockProcess.
type backend struct {
	Root, Bin, Repo, Base, Head, Next string
	exe                               string
}

func newBackend(t *testing.T) *backend {
	t.Helper()
	b := &backend{Root: t.TempDir()}
	b.Bin = filepath.Join(b.Root, "bin")
	b.Repo = filepath.Join(b.Root, "origin")
	if err := os.MkdirAll(filepath.Join(b.Root, "home"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b.Bin, 0755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = b.Repo
		c.Env = b.env()
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(b.Repo, 0755); err != nil {
		t.Fatal(err)
	}
	git("init", "-b", "main")
	git("config", "user.name", "E2E Fixture")
	git("config", "user.email", "e2e@example.invalid")
	git("config", "commit.gpgsign", "false")
	git("config", "core.hooksPath", "/dev/null")
	if err := os.WriteFile(filepath.Join(b.Repo, "feature.txt"), []byte("base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "base")
	b.Base = git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(b.Repo, "feature.txt"), []byte("base\nfirst review change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-am", "first review change")
	b.Head = git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(b.Repo, "feature.txt"), []byte("base\nfirst review change\nsecond review change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-am", "second review change")
	b.Next = git("rev-parse", "HEAD")
	git("update-ref", "refs/heads/main", b.Base)
	git("update-ref", "refs/pull/1/head", b.Head)
	if exe, err := os.Executable(); err != nil {
		t.Fatal(err)
	} else {
		b.exe = exe
	}
	for _, name := range []string{"gh", "codex", "terminal-notifier", "open", "xdg-open"} {
		p := filepath.Join(b.Bin, name)
		script := "#!/bin/sh\nE2E_MOCK_PROCESS=1 E2E_MOCK_KIND=" + name + " GORACE=atexit_sleep_ms=0 exec " + shellQuote(b.exe) + " -test.run=^TestMockProcess$ -- \"$@\"\n"
		if err := os.WriteFile(p, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	b.snapshot(t, b.Head, "OPEN", "IN_PROGRESS")
	return b
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// snapshot atomically replaces the data served by gh pr view.
func (b *backend) snapshot(t *testing.T, head, state, ci string) {
	t.Helper()
	data := map[string]any{
		"title": "Review fixture", "body": "E2E pull request fixture", "state": state,
		"headRefOid": head, "baseRefOid": b.Base, "baseRefName": "main",
		"statusCheckRollup": []any{mockCheck(ci)},
		"files":             []any{map[string]any{"path": "feature.txt", "additions": 2, "deletions": 0}},
		"commits":           []any{map[string]any{"oid": head, "messageHeadline": "review change"}},
		"url":               "https://github.com/acme/widget/pull/1",
	}
	buf, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(b.Root, "snapshot.tmp")
	if err = os.WriteFile(tmp, buf, 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(tmp, filepath.Join(b.Root, "snapshot.json")); err != nil {
		t.Fatal(err)
	}
}

func mockCheck(state string) map[string]any {
	if strings.EqualFold(state, "SUCCESS") {
		return map[string]any{"name": "Unit tests", "status": "COMPLETED", "conclusion": "SUCCESS"}
	}
	return map[string]any{"name": "Unit tests", "status": state, "conclusion": nil}
}

// advance makes the second fixture commit the new PR head and view.
func (b *backend) advance(t *testing.T) {
	t.Helper()
	c := exec.Command("git", "-C", b.Repo, "update-ref", "refs/pull/1/head", b.Next)
	c.Env = b.env()
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("advance PR ref: %v: %s", err, out)
	}
	b.snapshot(t, b.Next, "OPEN", "IN_PROGRESS")
}

// env returns a complete inherited environment with fake external tools first.
func (b *backend) env() []string {
	path := b.Bin + string(os.PathListSeparator) + os.Getenv("PATH")
	blocked := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_COMMON_DIR": true, "GIT_CONFIG_PARAMETERS": true,
		"GIT_CONFIG_COUNT": true,
	}
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && !blocked[key] {
			values[key] = entry
		}
	}
	values["HOME"] = "HOME=" + filepath.Join(b.Root, "home")
	values["E2E_ROOT"] = "E2E_ROOT=" + b.Root
	values["PATH"] = "PATH=" + path
	values["GIT_CONFIG_GLOBAL"] = "GIT_CONFIG_GLOBAL=/dev/null"
	values["GIT_CONFIG_NOSYSTEM"] = "GIT_CONFIG_NOSYSTEM=1"
	env := make([]string, 0, len(values))
	for _, entry := range values {
		env = append(env, entry)
	}
	return env
}

// TestMockProcess is re-executed by the fake command wrappers. os.Exit avoids
// the Go test runner writing PASS into the Codex JSONL stream.
func TestMockProcess(t *testing.T) {
	if os.Getenv("E2E_MOCK_PROCESS") != "1" {
		return
	}
	switch os.Getenv("E2E_MOCK_KIND") {
	case "gh":
		mockGH()
		os.Exit(0)
	case "codex":
		mockCodex()
		os.Exit(0)
	case "terminal-notifier":
		mockLog("notifications.log", mockArgs())
		os.Exit(0)
	case "open":
		mockLog("opened.log", mockArgs())
		os.Exit(0)
	case "xdg-open":
		mockLog("opened.log", mockArgs())
		os.Exit(0)
	}
	os.Exit(2)
}

func mockGH() {
	args := mockArgs()
	joined := strings.Join(args, " ")
	if _, err := os.Stat(filepath.Join(os.Getenv("E2E_ROOT"), "gh-error")); err == nil && strings.Contains(joined, "pr view") {
		fmt.Fprintln(os.Stderr, "mock gh view failure")
		os.Exit(1)
	}
	switch {
	case strings.Contains(joined, "auth status"):
	case strings.Contains(joined, "pr view"):
		buf, err := os.ReadFile(filepath.Join(os.Getenv("E2E_ROOT"), "snapshot.json"))
		if err != nil {
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(buf)
		_, _ = fmt.Fprintln(os.Stdout)
	case strings.Contains(joined, "repo clone"):
		if len(args) < 4 {
			os.Exit(2)
		}
		dst := args[len(args)-1]
		c := exec.Command("git", "clone", filepath.Join(os.Getenv("E2E_ROOT"), "origin"), dst)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if c.Run() != nil {
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "unsupported mock gh command:", joined)
		os.Exit(2)
	}
}

func mockArgs() []string {
	for i, arg := range os.Args {
		if arg == "--" {
			return os.Args[i+1:]
		}
	}
	return nil
}

func mockLog(name string, args []string) {
	line, _ := json.Marshal(args)
	f, err := os.OpenFile(filepath.Join(os.Getenv("E2E_ROOT"), name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.Write(append(line, '\n'))
		_ = f.Close()
	}
}

func mockCodex() {
	root := os.Getenv("E2E_ROOT")
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 4096), 4*1024*1024)
	var mu sync.Mutex
	write := func(v any) { mu.Lock(); defer mu.Unlock(); _ = json.NewEncoder(os.Stdout).Encode(v) }
	log := func(raw []byte) {
		f, e := os.OpenFile(filepath.Join(root, "codex.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if e == nil {
			_, _ = f.Write(append(append([]byte(nil), raw...), '\n'))
			_ = f.Close()
		}
	}
	for in.Scan() {
		raw := append([]byte(nil), in.Bytes()...)
		log(raw)
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(msg.Params, &p)
		result := any(map[string]any{})
		switch msg.Method {
		case "initialize":
			result = map[string]any{}
		case "account/read":
			result = map[string]any{"account": map[string]any{"type": "chatgpt"}}
		case "thread/start":
			result = map[string]any{"thread": map[string]any{"id": "e2e-thread"}}
		case "thread/resume":
			result = map[string]any{"thread": map[string]any{"id": p["threadId"]}}
		case "turn/start":
			result = map[string]any{"turn": map[string]any{"id": "e2e-turn"}}
		}
		write(map[string]any{"id": msg.ID, "result": result})
		if msg.Method == "turn/start" {
			cwd, _ := p["cwd"].(string)
			head := "review head"
			if cwd != "" {
				c := exec.Command("git", "-C", cwd, "rev-parse", "HEAD")
				if out, e := c.Output(); e == nil {
					head = strings.TrimSpace(string(out))
				}
			}
			go func() {
				time.Sleep(40 * time.Millisecond)
				write(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": p["threadId"], "turn": map[string]any{"id": "e2e-turn"}}})
				write(map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": "e2e-reviewer", "parentThreadId": p["threadId"], "agentNickname": "reviewer", "agentRole": "review"}}})
				write(map[string]any{"method": "item/started", "params": map[string]any{"threadId": p["threadId"], "item": map[string]any{"id": "graph", "type": "agentMessage"}}})
				write(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": p["threadId"], "item": map[string]any{"id": "graph", "type": "agentMessage", "phase": "final_answer", "text": "```mermaid\ngraph LR\n  Push --> Review\n```\n\nReview complete for " + head}}})
				questionSent := false
				for {
					if _, e := os.Stat(filepath.Join(root, "complete")); e == nil {
						_ = os.Remove(filepath.Join(root, "complete"))
						break
					}
					if _, e := os.Stat(filepath.Join(root, "disconnect")); e == nil {
						os.Exit(0)
					}
					if !questionSent {
						if _, e := os.Stat(filepath.Join(root, "question")); e == nil {
							_ = os.Remove(filepath.Join(root, "question"))
							questionSent = true
							write(map[string]any{"id": 100, "method": "item/tool/requestUserInput", "params": map[string]any{"threadId": p["threadId"], "questions": []any{map[string]any{"id": "scope", "header": "Scope", "question": "Focus on correctness?", "isSecret": false, "options": []any{map[string]any{"label": "Yes"}, map[string]any{"label": "No"}}}}}})
						}
					}
					time.Sleep(20 * time.Millisecond)
				}
				write(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": p["threadId"], "turn": map[string]any{"id": "e2e-turn", "status": "completed"}}})
			}()
		}
	}
}
