package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testCleanupOps(status func(context.Context, string) (string, error)) cleanupOps {
	return cleanupOps{lstat: os.Lstat, readDir: os.ReadDir, removeAll: os.RemoveAll, gitStatus: status}
}

func TestRemoveCheckoutsMissingAndInvalidPaths(t *testing.T) {
	if err := removeCheckouts(context.Background(), ""); err == nil {
		t.Fatal("accepted empty session directory")
	}
	sessionDir := t.TempDir()
	if err := removeCheckoutsWith(context.Background(), sessionDir, testCleanupOps(nil)); err != nil {
		t.Fatalf("missing checkouts directory: %v", err)
	}
	root := filepath.Join(sessionDir, "checkouts")
	if err := os.WriteFile(root, []byte("unknown"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeCheckoutsWith(context.Background(), sessionDir, testCleanupOps(nil)); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("non-directory root error = %v", err)
	}
	if got, err := os.ReadFile(root); err != nil || string(got) != "unknown" {
		t.Fatalf("non-directory path changed: %q %v", got, err)
	}
}

func TestRemoveCheckoutsRootInspectionErrorsAndSymlink(t *testing.T) {
	sessionDir := t.TempDir()
	root := filepath.Join(sessionDir, "checkouts")
	inspectErr := errors.New("permission denied")
	ops := testCleanupOps(nil)
	ops.lstat = func(string) (os.FileInfo, error) { return nil, inspectErr }
	if err := removeCheckoutsWith(context.Background(), sessionDir, ops); !errors.Is(err, inspectErr) {
		t.Fatalf("root inspection error = %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	ops = testCleanupOps(nil)
	ops.readDir = func(string) ([]os.DirEntry, error) { return nil, inspectErr }
	if err := removeCheckoutsWith(context.Background(), sessionDir, ops); !errors.Is(err, inspectErr) {
		t.Fatalf("directory listing error = %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root removed on listing error: %v", err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, root); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	if err := removeCheckoutsWith(context.Background(), sessionDir, testCleanupOps(nil)); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink root error = %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target changed: %v", err)
	}
}

func TestRemoveCheckoutsPreflightPreservesAllPathsOnError(t *testing.T) {
	sessionDir := t.TempDir()
	root := filepath.Join(sessionDir, "checkouts")
	clean := filepath.Join(root, "a-clean")
	dirty := filepath.Join(root, "b-dirty")
	if err := os.MkdirAll(clean, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirty, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{filepath.Join(clean, "tracked"), filepath.Join(dirty, "untracked")} {
		if err := os.WriteFile(entry, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	statusCalls := []string{}
	removeCalls := 0
	ops := testCleanupOps(func(_ context.Context, path string) (string, error) {
		statusCalls = append(statusCalls, filepath.Base(path))
		if filepath.Base(path) == "b-dirty" {
			return "?? untracked\n", nil
		}
		return "", nil
	})
	ops.removeAll = func(string) error { removeCalls++; return nil }
	err := removeCheckoutsWith(context.Background(), sessionDir, ops)
	if err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("dirty checkout error = %v", err)
	}
	if len(statusCalls) != 2 || removeCalls != 0 {
		t.Fatalf("preflight was incomplete: status=%v remove=%d", statusCalls, removeCalls)
	}
	for _, path := range []string{clean, dirty} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("checkout %q removed despite dirty preflight: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dirty, "untracked")); err != nil {
		t.Fatalf("untracked content was removed: %v", err)
	}
}

func TestRemoveCheckoutsRejectsUnknownAndSymlinkEntries(t *testing.T) {
	for _, mode := range []string{"file", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			sessionDir := t.TempDir()
			root := filepath.Join(sessionDir, "checkouts")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			entry := filepath.Join(root, "unexpected")
			if mode == "file" {
				if err := os.WriteFile(entry, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				target := t.TempDir()
				if err := os.Symlink(target, entry); err != nil {
					t.Skipf("symlink creation is unavailable: %v", err)
				}
			}
			if err := removeCheckoutsWith(context.Background(), sessionDir, testCleanupOps(nil)); err == nil {
				t.Fatalf("accepted unexpected %s path", mode)
			}
			if _, err := os.Lstat(entry); err != nil {
				t.Fatalf("unexpected path removed: %v", err)
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("checkout root removed: %v", err)
			}
		})
	}
}

func TestRemoveCheckoutsInspectionAndGitErrorsPreserveData(t *testing.T) {
	sessionDir := t.TempDir()
	root := filepath.Join(sessionDir, "checkouts")
	checkout := filepath.Join(root, "one")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(checkout, "keep")
	if err := os.WriteFile(marker, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspectErr := errors.New("checkout disappeared")
	ops := testCleanupOps(func(context.Context, string) (string, error) { return "", nil })
	ops.lstat = func(path string) (os.FileInfo, error) {
		if path == checkout {
			return nil, inspectErr
		}
		return os.Lstat(path)
	}
	if err := removeCheckoutsWith(context.Background(), sessionDir, ops); !errors.Is(err, inspectErr) {
		t.Fatalf("entry inspection error = %v", err)
	}
	gitErr := errors.New("not a git checkout")
	ops = testCleanupOps(func(context.Context, string) (string, error) { return "", gitErr })
	if err := removeCheckoutsWith(context.Background(), sessionDir, ops); !errors.Is(err, gitErr) {
		t.Fatalf("git status error = %v", err)
	}
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatalf("checkout data removed on error: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("checkout root removed on error: %v", err)
	}
}

func TestRemoveCheckoutsRemoveError(t *testing.T) {
	sessionDir := t.TempDir()
	root := filepath.Join(sessionDir, "checkouts")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	removeErr := errors.New("remove failed")
	ops := testCleanupOps(func(context.Context, string) (string, error) { return "", nil })
	ops.removeAll = func(string) error { return removeErr }
	if err := removeCheckoutsWith(context.Background(), sessionDir, ops); !errors.Is(err, removeErr) {
		t.Fatalf("remove error = %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root unexpectedly removed: %v", err)
	}
}

func TestRemoveCleanGitCheckoutsKeepsSessionData(t *testing.T) {
	sessionDir := t.TempDir()
	root := filepath.Join(sessionDir, "checkouts")
	checkout := filepath.Join(root, "revision-a")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, checkout, "init", "-q")
	runGitTestCommand(t, checkout, "config", "user.email", "review@example.test")
	runGitTestCommand(t, checkout, "config", "user.name", "Review Test")
	if err := os.WriteFile(filepath.Join(checkout, "tracked.txt"), []byte("committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, checkout, "add", "tracked.txt")
	runGitTestCommand(t, checkout, "commit", "-q", "-m", "initial")
	statePath := filepath.Join(sessionDir, "state.json")
	reportPath := filepath.Join(sessionDir, "report.md")
	for _, path := range []string{statePath, reportPath} {
		if err := os.WriteFile(path, []byte("retain"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeCheckouts(context.Background(), sessionDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkouts still exist: %v", err)
	}
	for _, path := range []string{statePath, reportPath} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "retain" {
			t.Fatalf("session data %q changed: %q %v", path, data, err)
		}
	}
}

func TestCheckoutGitStatusError(t *testing.T) {
	if _, err := checkoutGitStatus(context.Background(), t.TempDir()); err == nil {
		t.Fatal("git status accepted a non-checkout")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := checkoutGitStatus(canceled, t.TempDir()); err == nil {
		t.Fatal("git status ignored a canceled context")
	}
}

func runGitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, output)
	}
}

func TestRemoveCheckoutsRejectsEveryEntryBeforeDeletion(t *testing.T) {
	sessionDir := t.TempDir()
	root := filepath.Join(sessionDir, "checkouts")
	if err := os.MkdirAll(filepath.Join(root, "one"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "two"), 0o700); err != nil {
		t.Fatal(err)
	}
	checked := []string{}
	ops := testCleanupOps(func(_ context.Context, path string) (string, error) {
		checked = append(checked, filepath.Base(path))
		return "", nil
	})
	if err := removeCheckoutsWith(context.Background(), sessionDir, ops); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(checked) != "[one two]" {
		t.Fatalf("preflight paths = %v", checked)
	}
}
