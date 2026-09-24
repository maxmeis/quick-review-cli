package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type cleanupOps struct {
	lstat     func(string) (os.FileInfo, error)
	readDir   func(string) ([]os.DirEntry, error)
	removeAll func(string) error
	gitStatus func(context.Context, string) (string, error)
}

func removeCheckouts(ctx context.Context, sessionDir string) error {
	return removeCheckoutsWith(ctx, sessionDir, cleanupOps{
		lstat:     os.Lstat,
		readDir:   os.ReadDir,
		removeAll: os.RemoveAll,
		gitStatus: checkoutGitStatus,
	})
}

func removeCheckoutsWith(ctx context.Context, sessionDir string, ops cleanupOps) error {
	if sessionDir == "" {
		return errors.New("session directory is empty")
	}
	root := filepath.Join(sessionDir, "checkouts")
	info, err := ops.lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect checkouts directory %q: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to remove symlinked checkouts directory %q", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("checkouts path %q is not a directory", root)
	}
	entries, err := ops.readDir(root)
	if err != nil {
		return fmt.Errorf("list checkouts in %q: %w", root, err)
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		entryInfo, err := ops.lstat(path)
		if err != nil {
			return fmt.Errorf("inspect checkout path %q: %w", path, err)
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to remove symlinked checkout path %q", path)
		}
		if !entryInfo.IsDir() {
			return fmt.Errorf("unknown path in checkouts directory: %q", path)
		}
		status, err := ops.gitStatus(ctx, path)
		if err != nil {
			return fmt.Errorf("check checkout status for %q: %w", path, err)
		}
		if strings.TrimSpace(status) != "" {
			return fmt.Errorf("refusing to remove checkout with local changes: %q", path)
		}
	}
	if err := ops.removeAll(root); err != nil {
		return fmt.Errorf("remove checkouts directory %q: %w", root, err)
	}
	return nil
}

func checkoutGitStatus(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "status", "--porcelain", "--untracked-files=all")
	output, err := cmd.Output()
	return string(output), err
}
