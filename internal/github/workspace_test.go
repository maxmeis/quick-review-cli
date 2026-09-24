package github

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"quick-review-cli/internal/domain"
)

func TestWorkspacePrepareUsesFetchedSnapshot(t *testing.T) {
	fixture := newGitFixture(t)
	root := filepath.Join(t.TempDir(), "checkout")
	if out, err := exec.Command("git", "clone", fixture.origin, root).CombinedOutput(); err != nil {
		t.Fatalf("clone fixture: %v: %s", err, out)
	}
	var calls []call
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, call{name: name, args: append([]string(nil), args...)})
		return commandRunner(ctx, name, args...)
	}
	w := NewWorkspace(root, run)
	if w.Path() != root {
		t.Fatalf("Path() = %q, want %q", w.Path(), root)
	}
	head, base, diff, err := w.Prepare(context.Background(), fixture.pr, fixture.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if head != fixture.head || base != fixture.base {
		t.Fatalf("head/base = %s/%s, want %s/%s", head, base, fixture.head, fixture.base)
	}
	if !strings.Contains(diff, "+feature change") || !strings.Contains(diff, "-base") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
	if len(calls) < 8 {
		t.Fatalf("expected git preparation commands, saw %d", len(calls))
	}
	for _, c := range calls {
		if c.name != "git" || len(c.args) < 2 || c.args[0] != "-C" || c.args[1] != root {
			t.Fatalf("git command was not rooted in the workspace: %+v", c)
		}
	}
	status := gitOutput(t, root, "status", "--porcelain")
	if status != "" {
		t.Fatalf("checkout left worktree dirty: %q", status)
	}
}

func TestWorkspaceDoesNotDiscardLocalChanges(t *testing.T) {
	fixture := newGitFixture(t)
	root := filepath.Join(t.TempDir(), "checkout")
	if out, err := exec.Command("git", "clone", fixture.origin, root).CombinedOutput(); err != nil {
		t.Fatalf("clone fixture: %v: %s", err, out)
	}
	local := filepath.Join(root, "file.txt")
	if err := os.WriteFile(local, []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []call
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, call{name: name, args: append([]string(nil), args...)})
		return commandRunner(ctx, name, args...)
	}
	_, _, _, err := NewWorkspace(root, run).Prepare(context.Background(), fixture.pr, fixture.snapshot)
	if err == nil || !strings.Contains(err.Error(), "preserving them") {
		t.Fatalf("expected dirty-worktree error, got %v", err)
	}
	if got, readErr := os.ReadFile(local); readErr != nil || string(got) != "user edit\n" {
		t.Fatalf("local edit changed: %q, %v", got, readErr)
	}
	for _, c := range calls {
		if len(c.args) > 1 && c.args[0] == "-C" && c.args[2] == "fetch" {
			t.Fatal("fetched despite detecting preexisting local changes")
		}
	}
}

func TestWorkspaceDetectsFetchRace(t *testing.T) {
	fixture := newGitFixture(t)
	root := filepath.Join(t.TempDir(), "checkout")
	if out, err := exec.Command("git", "clone", fixture.origin, root).CombinedOutput(); err != nil {
		t.Fatalf("clone fixture: %v: %s", err, out)
	}
	snapshot := fixture.snapshot
	snapshot.HeadSHA = strings.Repeat("0", 40)
	_, _, _, err := NewWorkspace(root, nil).Prepare(context.Background(), fixture.pr, snapshot)
	if err == nil || !strings.Contains(err.Error(), "changed during review setup") || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("expected retryable head race, got %v", err)
	}
	if got := gitOutput(t, root, "rev-parse", "HEAD"); got == fixture.head {
		t.Fatal("workspace switched revisions before validating fetched SHA")
	}
}

func TestWorkspaceClonesAbsentPath(t *testing.T) {
	fixture := newGitFixture(t)
	root := filepath.Join(t.TempDir(), "new", "checkout")
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "gh" {
			if len(args) != 4 || args[0] != "repo" || args[1] != "clone" || args[2] != "acme/project" || args[3] != root {
				t.Fatalf("unexpected gh clone args: %v", args)
			}
			return commandRunner(ctx, "git", "clone", fixture.origin, root)
		}
		return commandRunner(ctx, name, args...)
	}
	if _, _, _, err := NewWorkspace(root, run).Prepare(context.Background(), fixture.pr, fixture.snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceValidationAndFailures(t *testing.T) {
	ctx := context.Background()
	pr := domain.PR{Owner: "acme", Repo: "project", Number: 4}
	snapshot := domain.Snapshot{HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), BaseBranch: "main"}
	for _, tc := range []struct {
		name string
		root string
		pr   domain.PR
		snap domain.Snapshot
	}{
		{name: "invalid PR", root: filepath.Join(t.TempDir(), "repo"), pr: domain.PR{Number: 1}, snap: snapshot},
		{name: "missing SHAs", root: filepath.Join(t.TempDir(), "repo"), pr: pr, snap: domain.Snapshot{BaseBranch: "main"}},
		{name: "invalid branch", root: filepath.Join(t.TempDir(), "repo"), pr: pr, snap: domain.Snapshot{HeadSHA: "a", BaseSHA: "b", BaseBranch: "bad branch"}},
		{name: "invalid branch component", root: filepath.Join(t.TempDir(), "repo"), pr: pr, snap: domain.Snapshot{HeadSHA: "a", BaseSHA: "b", BaseBranch: "feature//branch"}},
		{name: "root current directory", root: ".", pr: pr, snap: snapshot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			r := func(context.Context, string, ...string) ([]byte, error) { called = true; return nil, nil }
			if _, _, _, err := NewWorkspace(tc.root, r).Prepare(ctx, tc.pr, tc.snap); err == nil {
				t.Fatal("expected validation failure")
			}
			if called {
				t.Fatal("runner called before validation completed")
			}
		})
	}

	root := filepath.Join(t.TempDir(), "checkout")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NewWorkspace(root, nil).Prepare(ctx, pr, snapshot); err == nil || !strings.Contains(err.Error(), "not a Git checkout") {
		t.Fatalf("expected nonempty path error, got %v", err)
	}

	root = filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NewWorkspace(root, nil).Prepare(ctx, pr, snapshot); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected file path error, got %v", err)
	}

	root = filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cloneCalled := false
	if _, _, _, err := NewWorkspace(root, func(context.Context, string, ...string) ([]byte, error) {
		cloneCalled = true
		return nil, nil
	}).Prepare(ctx, pr, snapshot); err == nil || !strings.Contains(err.Error(), "choose an absent path") {
		t.Fatalf("expected existing empty path error, got %v", err)
	}
	if cloneCalled {
		t.Fatal("attempted to clone into an existing empty path")
	}

	parent := filepath.Join(t.TempDir(), "parent-file")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NewWorkspace(filepath.Join(parent, "checkout"), nil).Prepare(ctx, pr, snapshot); err == nil || !strings.Contains(err.Error(), "inspect workspace path") {
		t.Fatalf("expected workspace path inspection error, got %v", err)
	}
	mkdirRoot := filepath.Join(t.TempDir(), "missing", "checkout")
	mkdirWorkspace := NewWorkspace(mkdirRoot, nil)
	mkdirWorkspace.fs.mkdirAll = func(string, os.FileMode) error { return errors.New("mkdir blocked") }
	if _, _, _, err := mkdirWorkspace.Prepare(ctx, pr, snapshot); err == nil || !strings.Contains(err.Error(), "create workspace parent") {
		t.Fatalf("expected injected parent creation error, got %v", err)
	}

	readDirRoot := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(readDirRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	readDirWorkspace := NewWorkspace(readDirRoot, nil)
	readDirWorkspace.fs.readDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("read denied") }
	if _, _, _, err := readDirWorkspace.Prepare(ctx, pr, snapshot); err == nil || !strings.Contains(err.Error(), "inspect workspace directory") {
		t.Fatalf("expected injected directory read error, got %v", err)
	}

	cloneRoot := filepath.Join(t.TempDir(), "clone-failure")
	cloneErr := errors.New("gh unavailable")
	if _, _, _, err := NewWorkspace(cloneRoot, func(context.Context, string, ...string) ([]byte, error) { return nil, cloneErr }).Prepare(ctx, pr, snapshot); !errors.Is(err, cloneErr) {
		t.Fatalf("clone error was not propagated: %v", err)
	}

	brokenGit := filepath.Join(t.TempDir(), "broken-git")
	if err := os.Mkdir(brokenGit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(brokenGit, ".git"), filepath.Join(brokenGit, ".git")); err == nil {
		if _, _, _, err := NewWorkspace(brokenGit, nil).Prepare(ctx, pr, snapshot); err == nil || !strings.Contains(err.Error(), "inspect Git checkout") {
			t.Fatalf("expected .git inspection error, got %v", err)
		}
	}

	runErr := errors.New("fetch blocked")
	fixture := newGitFixture(t)
	root = filepath.Join(t.TempDir(), "checkout")
	if out, err := exec.Command("git", "clone", fixture.origin, root).CombinedOutput(); err != nil {
		t.Fatalf("clone fixture: %v: %s", err, out)
	}
	r := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "git" && len(args) > 2 && args[2] == "fetch" {
			return nil, runErr
		}
		return commandRunner(ctx, name, args...)
	}
	if _, _, _, err := NewWorkspace(root, r).Prepare(ctx, fixture.pr, fixture.snapshot); !errors.Is(err, runErr) {
		t.Fatalf("fetch error was not propagated: %v", err)
	}

	for _, stage := range []string{"fetch", "checkout", "head-parse", "base-parse", "head-mismatch", "base-mismatch", "status-second", "merge-base", "diff", "root-parse", "status-error", "checked-out-head-parse"} {
		t.Run(stage, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "checkout")
			if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			calls := map[string]int{}
			runnerErr := errors.New("stage failed")
			fake := func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "gh" {
					return nil, runnerErr
				}
				if name != "git" || len(args) < 3 {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				cmd := args[2]
				calls[cmd]++
				if stage == "root-parse" && cmd == "rev-parse" && args[3] == "--show-toplevel" {
					return nil, runnerErr
				}
				if cmd == "status" {
					if stage == "status-error" {
						return nil, runnerErr
					}
					if stage == "status-second" && calls[cmd] == 2 {
						return []byte(" M changed"), nil
					}
					return nil, nil
				}
				if stage == "fetch" && cmd == "fetch" {
					return nil, runnerErr
				}
				if cmd == "checkout" && stage == "checkout" {
					return nil, runnerErr
				}
				if cmd == "merge-base" && stage == "merge-base" {
					return nil, runnerErr
				}
				if cmd == "diff" && stage == "diff" {
					return nil, runnerErr
				}
				if cmd == "rev-parse" && len(args) > 4 && args[3] == "--verify" {
					switch {
					case args[4] == "HEAD^{commit}":
						if stage == "checked-out-head-parse" {
							return nil, runnerErr
						}
						return []byte(snapshot.HeadSHA), nil
					case strings.Contains(args[4], "/head^"):
						if stage == "head-parse" {
							return nil, runnerErr
						}
						if stage == "head-mismatch" {
							return []byte(strings.Repeat("c", 40)), nil
						}
						return []byte(snapshot.HeadSHA), nil
					case strings.Contains(args[4], "/base^"):
						if stage == "base-parse" {
							return nil, runnerErr
						}
						if stage == "base-mismatch" {
							return []byte(strings.Repeat("c", 40)), nil
						}
						return []byte(snapshot.BaseSHA), nil
					default:
						return []byte(snapshot.HeadSHA), nil
					}
				}
				if cmd == "rev-parse" {
					return []byte("/workspace"), nil
				}
				if cmd == "merge-base" {
					return []byte(snapshot.BaseSHA), nil
				}
				return nil, nil
			}
			_, _, _, err := NewWorkspace(root, fake).Prepare(ctx, pr, snapshot)
			if err == nil {
				t.Fatal("expected stage failure")
			}
		})
	}
}

type gitFixture struct {
	origin   string
	pr       domain.PR
	snapshot domain.Snapshot
	head     string
	base     string
}

func newGitFixture(t *testing.T) gitFixture {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	source := filepath.Join(root, "source")
	runGit(t, root, "init", "--bare", "--initial-branch=main", origin)
	if out, err := exec.Command("git", "init", "-b", "main", source).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	gitOutput(t, source, "config", "user.email", "test@example.com")
	gitOutput(t, source, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, source, "add", "file.txt")
	gitOutput(t, source, "commit", "-m", "base")
	base := gitOutput(t, source, "rev-parse", "HEAD")
	gitOutput(t, source, "remote", "add", "origin", origin)
	gitOutput(t, source, "push", "origin", "main")
	gitOutput(t, source, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("feature change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, source, "commit", "-am", "feature")
	head := gitOutput(t, source, "rev-parse", "HEAD")
	gitOutput(t, source, "push", "origin", "HEAD:refs/pull/5/head")
	return gitFixture{
		origin: origin,
		pr:     domain.PR{Owner: "acme", Repo: "project", Number: 5},
		snapshot: domain.Snapshot{
			HeadSHA: head, BaseSHA: base, BaseBranch: "main",
		},
		head: head, base: base,
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
