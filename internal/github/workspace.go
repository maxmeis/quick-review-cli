package github

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"quick-review-cli/internal/domain"
)

// Workspace manages a dedicated clone used for reviewing a pull request.
// root is the checkout directory and is never removed automatically.
type Workspace struct {
	root string
	run  Runner
	fs   workspaceFS
}

type workspaceFS struct {
	stat     func(string) (os.FileInfo, error)
	mkdirAll func(string, os.FileMode) error
	readDir  func(string) ([]os.DirEntry, error)
}

func realWorkspaceFS() workspaceFS {
	return workspaceFS{stat: os.Stat, mkdirAll: os.MkdirAll, readDir: os.ReadDir}
}

func NewWorkspace(root string, r Runner) *Workspace {
	if r == nil {
		r = commandRunner
	}
	return &Workspace{root: filepath.Clean(root), run: r, fs: realWorkspaceFS()}
}

func (w *Workspace) Path() string { return w.root }

// Prepare checks out the exact head and base revisions from the snapshot, then
// returns the checked out SHA, merge base, and three-dot-equivalent diff.
func (w *Workspace) Prepare(ctx context.Context, pr domain.PR, snapshot domain.Snapshot) (head, mergeBase, diff string, err error) {
	if err := validatePR(pr); err != nil {
		return "", "", "", err
	}
	if snapshot.HeadSHA == "" || snapshot.BaseSHA == "" {
		return "", "", "", fmt.Errorf("pull request snapshot is missing a head or base SHA")
	}
	if !validRefComponent(snapshot.BaseBranch) {
		return "", "", "", fmt.Errorf("invalid base branch %q", snapshot.BaseBranch)
	}
	if w.root == "." || w.root == string(filepath.Separator) {
		return "", "", "", fmt.Errorf("workspace path must be a dedicated directory")
	}
	if err := w.ensureClone(ctx, pr); err != nil {
		return "", "", "", err
	}
	if err := w.requireClean(ctx); err != nil {
		return "", "", "", err
	}

	baseRef := fmt.Sprintf("refs/quick-review/%d/base", pr.Number)
	headRef := fmt.Sprintf("refs/quick-review/%d/head", pr.Number)
	headRemote := fmt.Sprintf("refs/pull/%d/head", pr.Number)
	baseRemote := "refs/heads/" + snapshot.BaseBranch
	_, err = w.git(ctx, "fetch", "--force", "origin", "+"+headRemote+":"+headRef, "+"+baseRemote+":"+baseRef)
	if err != nil {
		return "", "", "", fmt.Errorf("fetch pull request revisions: %w", err)
	}

	fetchedHead, err := w.revParse(ctx, headRef)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve fetched pull request head: %w", err)
	}
	fetchedBase, err := w.revParse(ctx, baseRef)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve fetched pull request base: %w", err)
	}
	if !strings.EqualFold(fetchedHead, snapshot.HeadSHA) {
		return "", "", "", fmt.Errorf("pull request head changed during review setup (snapshot %s, fetched %s); retry", snapshot.HeadSHA, fetchedHead)
	}
	if !strings.EqualFold(fetchedBase, snapshot.BaseSHA) {
		return "", "", "", fmt.Errorf("pull request base changed during review setup (snapshot %s, fetched %s); retry", snapshot.BaseSHA, fetchedBase)
	}

	// Check cleanliness again immediately before changing HEAD. This also
	// protects edits that appeared while the network fetch was in progress.
	if err := w.requireClean(ctx); err != nil {
		return "", "", "", err
	}
	if _, err := w.git(ctx, "checkout", "--detach", fetchedHead); err != nil {
		return "", "", "", fmt.Errorf("checkout pull request head: %w", err)
	}
	head, err = w.revParse(ctx, "HEAD")
	if err != nil {
		return "", "", "", fmt.Errorf("resolve checked out head: %w", err)
	}
	mergeBase, err = w.gitText(ctx, "merge-base", "HEAD", baseRef)
	if err != nil {
		return "", "", "", fmt.Errorf("find pull request merge base: %w", err)
	}
	diff, err = w.gitText(ctx, "diff", "--no-ext-diff", mergeBase, "HEAD")
	if err != nil {
		return "", "", "", fmt.Errorf("diff pull request changes: %w", err)
	}
	return head, mergeBase, diff, nil
}

func (w *Workspace) ensureClone(ctx context.Context, pr domain.PR) error {
	info, err := w.fs.stat(w.root)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect workspace path %q: %w", w.root, err)
	}
	if os.IsNotExist(err) {
		if err := w.fs.mkdirAll(filepath.Dir(w.root), 0o755); err != nil {
			return fmt.Errorf("create workspace parent: %w", err)
		}
		if _, err := w.run(ctx, "gh", "repo", "clone", pr.Owner+"/"+pr.Repo, w.root); err != nil {
			return fmt.Errorf("clone %s/%s into workspace: %w", pr.Owner, pr.Repo, err)
		}
	} else {
		if !info.IsDir() {
			return fmt.Errorf("workspace path %q is not a directory", w.root)
		}
		if _, err := w.fs.stat(filepath.Join(w.root, ".git")); os.IsNotExist(err) {
			entries, readErr := w.fs.readDir(w.root)
			if readErr != nil {
				return fmt.Errorf("inspect workspace directory: %w", readErr)
			}
			if len(entries) == 0 {
				return fmt.Errorf("workspace path %q exists but is not a Git checkout; choose an absent path to clone", w.root)
			}
			return fmt.Errorf("workspace path %q exists and is not a Git checkout", w.root)
		} else if err != nil {
			return fmt.Errorf("inspect Git checkout at %q: %w", w.root, err)
		}
	}
	if _, err := w.git(ctx, "rev-parse", "--show-toplevel"); err != nil {
		return fmt.Errorf("workspace path %q is not a usable Git checkout: %w", w.root, err)
	}
	return nil
}

func (w *Workspace) requireClean(ctx context.Context) error {
	status, err := w.git(ctx, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("check workspace status: %w", err)
	}
	if len(strings.TrimSpace(string(status))) != 0 {
		return fmt.Errorf("workspace has local changes; preserving them and refusing to switch revisions")
	}
	return nil
}

func (w *Workspace) revParse(ctx context.Context, rev string) (string, error) {
	return w.gitText(ctx, "rev-parse", "--verify", rev+"^{commit}")
}

func (w *Workspace) git(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"-C", w.root}, args...)
	out, err := w.run(ctx, "git", full...)
	if err != nil {
		return out, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func (w *Workspace) gitText(ctx context.Context, args ...string) (string, error) {
	out, err := w.git(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func validRefComponent(branch string) bool {
	if branch == "" || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.ContainsAny(branch, " ~^:?*[\\") {
		return false
	}
	for _, part := range strings.Split(branch, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
