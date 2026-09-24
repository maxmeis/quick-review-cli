package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quick-review-cli/internal/domain"
)

type fakeWritableFile struct {
	name     string
	data     bytes.Buffer
	chmodErr error
	writeErr error
	syncErr  error
	closeErr error
}

func (f *fakeWritableFile) Name() string            { return f.name }
func (f *fakeWritableFile) Chmod(os.FileMode) error { return f.chmodErr }
func (f *fakeWritableFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.data.Write(p)
}
func (f *fakeWritableFile) Sync() error  { return f.syncErr }
func (f *fakeWritableFile) Close() error { return f.closeErr }

type fakeReadCloser struct {
	io.Reader
	closeErr error
}

func (f fakeReadCloser) Close() error { return f.closeErr }

func replaceSessionFS(t *testing.T, change func(*filesystemOps)) func() {
	t.Helper()
	previous := sessionFS
	change(&sessionFS)
	return func() { sessionFS = previous }
}

func TestParsePR(t *testing.T) {
	valid := []string{
		"https://github.com/octo-org/repo_1/pull/12",
		"https://github.com/octo/re.po/pull/7/files",
		"https://github.com/octo/repo/pull/2/files/changed?tab=files",
		"https://github.com/octo/repo/pull/3?expand=1",
	}
	for _, raw := range valid {
		pr, err := ParsePR(raw)
		if err != nil || pr.Owner != "octo" && pr.Owner != "octo-org" || pr.Number < 2 && raw != valid[1] {
			t.Errorf("ParsePR(%q) = %#v, %v", raw, pr, err)
		}
	}
	for _, raw := range []string{
		"", " https://github.com/octo/repo/pull/1", "https://github.com.evil.test/octo/repo/pull/1",
		"http://github.com/octo/repo/pull/1", "https://user:pass@github.com/octo/repo/pull/1",
		"https://github.com/octo/repo/pull/0", "https://github.com/octo/repo/pull/-1",
		"https://github.com/octo/repo/pull/1/", "https://github.com/a!/repo/pull/1",
		"https://github.com/octo/a%2Fb/pull/1", "https://github.com/octo/repo/pulls/1",
		"https://github.com/octo/../pull/1",
		"https://github.com/%zz/repo/pull/1",
		"https://github.com/octo/repo/pull/999999999999999999999999",
	} {
		if _, err := ParsePR(raw); err == nil {
			t.Errorf("ParsePR(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestIdentityURL(t *testing.T) {
	pr := domain.PR{Owner: "octo", Repo: "repo", Number: 42}
	if got := Identity(pr); got != "octo/repo" {
		t.Fatalf("Identity = %q", got)
	}
	if got := URL(pr); got != "https://github.com/octo/repo/pull/42" {
		t.Fatalf("URL = %q", got)
	}
}

func TestCI(t *testing.T) {
	tests := []struct {
		states []string
		want   string
	}{
		{nil, "none"},
		{[]string{"unknown"}, "none"},
		{[]string{"success", "passed", "skipped"}, "passed"},
		{[]string{"success", "IN_PROGRESS"}, "running"},
		{[]string{"queued", "failure"}, "failed"},
		{[]string{"error"}, "failed"},
		{[]string{"success", "vendor_state"}, "running"},
		{[]string{"vendor_state"}, "none"},
		{[]string{""}, "none"},
	}
	for _, test := range tests {
		checks := make([]domain.Check, len(test.states))
		for i, state := range test.states {
			checks[i].State = state
		}
		if got := CI(checks); got != test.want {
			t.Errorf("CI(%v) = %q; want %q", test.states, got, test.want)
		}
	}
}

func TestChanges(t *testing.T) {
	before := domain.Snapshot{HeadSHA: "old", BaseSHA: "base1", BaseBranch: "main", State: "OPEN", Title: "old title", Body: "old body", Checks: []domain.Check{{State: "pending"}}}
	after := domain.Snapshot{HeadSHA: "new", BaseSHA: "base2", BaseBranch: "trunk", State: "MERGED", Title: "new title", Body: "new body", Checks: []domain.Check{{State: "success"}}}
	got := Changes(before, after)
	if len(got) != 4 {
		t.Fatalf("Changes returned %d events, want push/base/state/metadata only: %#v", len(got), got)
	}
	wants := []string{"push", "base", "state", "metadata"}
	for i, event := range got {
		if event.Kind != wants[i] || !event.Time.IsZero() || event.ID != 0 {
			t.Errorf("event %d = %#v", i, event)
		}
	}
	after.HeadSHA = "old"
	after.BaseSHA, after.BaseBranch, after.State, after.Title, after.Body = before.BaseSHA, before.BaseBranch, before.State, before.Title, before.Body
	after.Checks = []domain.Check{{State: "success"}}
	ci := Changes(before, after)
	if len(ci) != 1 || ci[0].Kind != "ci" || ci[0].Source != "CI" || ci[0].Detail != ": pending → success" {
		t.Fatalf("CI transition = %#v", ci)
	}
	if events := Changes(after, after); len(events) != 0 {
		t.Fatalf("unchanged snapshot produced events: %#v", events)
	}
	blank := Changes(domain.Snapshot{}, domain.Snapshot{HeadSHA: "", BaseSHA: "", BaseBranch: "", State: "", Title: "", Body: ""})
	if len(blank) != 0 {
		t.Fatalf("empty snapshot produced events: %#v", blank)
	}
}

func TestStorePersistence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Dir()); err != nil {
		t.Fatal(err)
	}
	state := domain.State{Snapshot: domain.Snapshot{Title: "Review"}, ReviewedHead: "abc", Events: []domain.Event{{Kind: "push"}}}
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadState()
	if err != nil || loaded.ReviewedHead != state.ReviewedHead || loaded.Snapshot.Title != "Review" {
		t.Fatalf("LoadState = %#v, %v", loaded, err)
	}
	if err := store.Append(domain.Event{ID: 1, Source: "GitHub", Kind: "push", Text: "updated"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events()
	if err != nil || len(events) != 1 || events[0].Text != "updated" {
		t.Fatalf("Events = %#v, %v", events, err)
	}
	reopened, err := OpenStore(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Dir() != store.Dir() {
		t.Fatalf("OpenStore dir = %q", reopened.Dir())
	}
	if err := reopened.SaveState(domain.State{Phase: "done"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = reopened.LoadState()
	if err != nil || loaded.Phase != "done" {
		t.Fatalf("replaced state = %#v, %v", loaded, err)
	}
}

func TestStoreErrorsAndEmptyEvents(t *testing.T) {
	if _, err := NewStore(""); err == nil {
		t.Fatal("NewStore accepted empty root")
	}
	if _, err := OpenStore(""); err == nil {
		t.Fatal("OpenStore accepted empty directory")
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(file); err == nil {
		t.Fatal("NewStore accepted file as root")
	}
	if _, err := OpenStore(file); err == nil {
		t.Fatal("OpenStore accepted file as directory")
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "session"))
	if err != nil {
		t.Fatal(err)
	}
	if events, err := store.Events(); err != nil || len(events) != 0 {
		t.Fatalf("empty Events = %#v, %v", events, err)
	}
	if _, err := store.LoadState(); err == nil {
		t.Fatal("LoadState accepted missing state")
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "state.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadState(); err == nil {
		t.Fatal("LoadState accepted malformed JSON")
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "events.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if events, err := store.Events(); err != nil || len(events) != 0 {
		t.Fatalf("empty event log = %#v, %v", events, err)
	}
	restoreFS := replaceSessionFS(t, func(ops *filesystemOps) {
		ops.open = func(string) (io.ReadCloser, error) { return nil, os.ErrPermission }
	})
	if _, err := store.Events(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Events open error = %v", err)
	}
	restoreFS()
	if err := os.WriteFile(filepath.Join(store.Dir(), "events.jsonl"), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("Events malformed log error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "events.jsonl"), []byte(strings.Repeat("x", 4*1024*1024+10)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("Events oversized line error = %v", err)
	}
}

func TestNewStoreTemporaryDirectoryError(t *testing.T) {
	root := t.TempDir()
	restore := replaceSessionFS(t, func(ops *filesystemOps) {
		ops.mkdirTemp = func(string, string) (string, error) { return "", os.ErrPermission }
	})
	defer restore()
	if _, err := NewStore(root); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("NewStore temp error = %v", err)
	}
}

func TestAppendIOErrors(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	badTime := domain.Event{Time: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)}
	if err := store.Append(badTime); err == nil || !strings.Contains(err.Error(), "encode event") {
		t.Fatalf("Append marshal error = %v", err)
	}
	restore := replaceSessionFS(t, func(ops *filesystemOps) {
		ops.openFile = func(string, int, os.FileMode) (writableFile, error) { return nil, os.ErrPermission }
	})
	if err := store.Append(domain.Event{}); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Append open error = %v", err)
	}
	restore()

	writeErr := errors.New("disk full")
	restore = replaceSessionFS(t, func(ops *filesystemOps) {
		ops.openFile = func(string, int, os.FileMode) (writableFile, error) {
			return &fakeWritableFile{writeErr: writeErr}, nil
		}
	})
	if err := store.Append(domain.Event{}); !errors.Is(err, writeErr) {
		t.Fatalf("Append write error = %v", err)
	}
	restore()

	closeErr := errors.New("close failed")
	restore = replaceSessionFS(t, func(ops *filesystemOps) {
		ops.openFile = func(string, int, os.FileMode) (writableFile, error) {
			return &fakeWritableFile{closeErr: closeErr}, nil
		}
	})
	defer restore()
	if err := store.Append(domain.Event{}); !errors.Is(err, closeErr) {
		t.Fatalf("Append close error = %v", err)
	}
}

func TestSaveReport(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"", "abcdef0"}, {"../abcdef0", "abcdef0"}, {"abc", "abcdef0"}, {strings.Repeat("g", 40), "abcdef0"}} {
		if _, err := store.SaveReport(pair[0], pair[1], "report"); err == nil {
			t.Errorf("SaveReport accepted unsafe hashes %q and %q", pair[0], pair[1])
		}
	}
	one, err := store.SaveReport(strings.Repeat("A", 40), strings.Repeat("b", 40), "# report")
	if err != nil {
		t.Fatal(err)
	}
	two, err := store.SaveReport(strings.Repeat("A", 40), strings.Repeat("b", 40), "# report")
	if err != nil {
		t.Fatal(err)
	}
	if one.Path == two.Path || !strings.Contains(one.Path, "review-aaaaaaaaaaaa-bbbbbbbbbbbb-") || one.Text != "# report" || one.Stale || one.CreatedAt.IsZero() {
		t.Fatalf("unexpected reports: %#v %#v", one, two)
	}
	content, err := os.ReadFile(one.Path)
	if err != nil || string(content) != "# report" {
		t.Fatalf("report contents %q, %v", content, err)
	}
}

func TestSaveReportDirectoryErrorAndAtomicEncodingError(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "reports"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveReport(strings.Repeat("a", 40), strings.Repeat("b", 40), "x"); err == nil {
		t.Fatal("SaveReport accepted a file as the reports directory")
	}
	if err := writeJSONAtomic(t.TempDir(), "bad.json", make(chan int)); err == nil {
		t.Fatal("writeJSONAtomic accepted an unencodable value")
	}
}

func TestSaveReportFailurePaths(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	head, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	randomErr := errors.New("random unavailable")
	restore := replaceSessionFS(t, func(ops *filesystemOps) {
		ops.randomRead = func([]byte) (int, error) { return 0, randomErr }
	})
	if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, randomErr) {
		t.Fatalf("SaveReport random error = %v", err)
	}
	restore()

	restore = replaceSessionFS(t, func(ops *filesystemOps) {
		ops.openFile = func(string, int, os.FileMode) (writableFile, error) { return nil, os.ErrPermission }
	})
	if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("SaveReport create error = %v", err)
	}
	restore()
	if entries, err := os.ReadDir(filepath.Join(store.Dir(), "reports")); err != nil || len(entries) != 0 {
		t.Fatalf("temporary report remained after reservation error: %v %v", entries, err)
	}

	restore = replaceSessionFS(t, func(ops *filesystemOps) {
		ops.openFile = func(string, int, os.FileMode) (writableFile, error) { return nil, os.ErrExist }
	})
	if _, err := store.SaveReport(head, base, "body"); err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatalf("SaveReport collision error = %v", err)
	}
	restore()

	writeErr := errors.New("disk full")
	restore = replaceSessionFS(t, func(ops *filesystemOps) {
		ops.createTemp = func(string, string) (writableFile, error) {
			return &fakeWritableFile{name: filepath.Join(store.Dir(), "staging"), writeErr: writeErr}, nil
		}
	})
	if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, writeErr) {
		t.Fatalf("SaveReport write error = %v", err)
	}
	restore()

	closeErr := errors.New("close failed")
	restore = replaceSessionFS(t, func(ops *filesystemOps) {
		ops.createTemp = func(string, string) (writableFile, error) {
			return &fakeWritableFile{name: filepath.Join(store.Dir(), "staging"), closeErr: closeErr}, nil
		}
	})
	defer restore()
	if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, closeErr) {
		t.Fatalf("SaveReport close error = %v", err)
	}
}

func TestSaveReportAtomicStagingFailures(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	head, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	t.Run("create temp", func(t *testing.T) {
		createErr := errors.New("temp create failed")
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) { return nil, createErr }
		})
		defer restore()
		if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, createErr) {
			t.Fatalf("create temp error = %v", err)
		}
	})
	t.Run("chmod", func(t *testing.T) {
		chmodErr := errors.New("chmod failed")
		removed := false
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) {
				return &fakeWritableFile{name: "staging", chmodErr: chmodErr}, nil
			}
			ops.remove = func(path string) error { removed = path == "staging"; return nil }
		})
		defer restore()
		if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, chmodErr) || !removed {
			t.Fatalf("chmod failure/cleanup = %v, removed=%v", err, removed)
		}
	})
	t.Run("sync", func(t *testing.T) {
		syncErr := errors.New("sync failed")
		removed := false
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) {
				return &fakeWritableFile{name: "staging", syncErr: syncErr}, nil
			}
			ops.remove = func(path string) error { removed = path == "staging"; return nil }
		})
		defer restore()
		if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, syncErr) || !removed {
			t.Fatalf("sync failure/cleanup = %v, removed=%v", err, removed)
		}
	})
	t.Run("short write", func(t *testing.T) {
		removed := false
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) {
				return &shortWritableFile{name: "staging"}, nil
			}
			ops.remove = func(path string) error { removed = path == "staging"; return nil }
		})
		defer restore()
		if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, io.ErrShortWrite) || !removed {
			t.Fatalf("short write/cleanup = %v, removed=%v", err, removed)
		}
	})
	t.Run("reservation close", func(t *testing.T) {
		closeErr := errors.New("reservation close failed")
		removed := []string{}
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.openFile = func(string, int, os.FileMode) (writableFile, error) {
				return &fakeWritableFile{closeErr: closeErr}, nil
			}
			ops.remove = func(path string) error { removed = append(removed, path); return nil }
		})
		defer restore()
		if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, closeErr) || len(removed) != 2 {
			t.Fatalf("reservation close/cleanup = %v, removed=%v", err, removed)
		}
	})
	t.Run("rename", func(t *testing.T) {
		renameErr := errors.New("rename failed")
		removed := []string{}
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.rename = func(string, string) error { return renameErr }
			ops.remove = func(path string) error { removed = append(removed, path); return nil }
		})
		defer restore()
		if _, err := store.SaveReport(head, base, "body"); !errors.Is(err, renameErr) || len(removed) != 2 {
			t.Fatalf("rename failure/cleanup = %v, removed=%v", err, removed)
		}
	})
}

type shortWritableFile struct{ name string }

func (f *shortWritableFile) Name() string              { return f.name }
func (f *shortWritableFile) Chmod(os.FileMode) error   { return nil }
func (f *shortWritableFile) Write([]byte) (int, error) { return 0, nil }
func (f *shortWritableFile) Sync() error               { return nil }
func (f *shortWritableFile) Close() error              { return nil }

func TestWriteJSONAtomicFailurePaths(t *testing.T) {
	dir := t.TempDir()
	t.Run("create", func(t *testing.T) {
		createErr := errors.New("create failed")
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) { return nil, createErr }
		})
		defer restore()
		if err := writeJSONAtomic(dir, "state.json", domain.State{}); !errors.Is(err, createErr) {
			t.Fatalf("create error = %v", err)
		}
	})
	t.Run("chmod", func(t *testing.T) {
		chmodErr := errors.New("chmod failed")
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) {
				return &fakeWritableFile{name: "temp", chmodErr: chmodErr}, nil
			}
		})
		defer restore()
		if err := writeJSONAtomic(dir, "state.json", domain.State{}); !errors.Is(err, chmodErr) {
			t.Fatalf("chmod error = %v", err)
		}
	})
	t.Run("write", func(t *testing.T) {
		writeErr := errors.New("write failed")
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) {
				return &fakeWritableFile{name: "temp", writeErr: writeErr}, nil
			}
		})
		defer restore()
		if err := writeJSONAtomic(dir, "state.json", domain.State{}); !errors.Is(err, writeErr) {
			t.Fatalf("write error = %v", err)
		}
	})
	t.Run("sync", func(t *testing.T) {
		syncErr := errors.New("sync failed")
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) {
				return &fakeWritableFile{name: "temp", syncErr: syncErr}, nil
			}
		})
		defer restore()
		if err := writeJSONAtomic(dir, "state.json", domain.State{}); !errors.Is(err, syncErr) {
			t.Fatalf("sync error = %v", err)
		}
	})
	t.Run("close", func(t *testing.T) {
		closeErr := errors.New("close failed")
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) {
				return &fakeWritableFile{name: "temp", closeErr: closeErr}, nil
			}
		})
		defer restore()
		if err := writeJSONAtomic(dir, "state.json", domain.State{}); !errors.Is(err, closeErr) {
			t.Fatalf("close error = %v", err)
		}
	})
	t.Run("rename", func(t *testing.T) {
		renameErr := errors.New("rename failed")
		restore := replaceSessionFS(t, func(ops *filesystemOps) {
			ops.createTemp = func(string, string) (writableFile, error) { return &fakeWritableFile{name: "temp"}, nil }
			ops.rename = func(string, string) error { return renameErr }
		})
		defer restore()
		if err := writeJSONAtomic(dir, "state.json", domain.State{}); !errors.Is(err, renameErr) {
			t.Fatalf("rename error = %v", err)
		}
	})
}

func TestNotify(t *testing.T) {
	called := false
	look := func(name string) (string, error) {
		if name != "terminal-notifier" {
			t.Errorf("looked up %q", name)
		}
		return "/bin/terminal-notifier", nil
	}
	run := func(ctx context.Context, path string, args ...string) error {
		called = true
		deadline, hasDeadline := ctx.Deadline()
		if !hasDeadline || time.Until(deadline) > notifierTimeout || path != "/bin/terminal-notifier" || strings.Join(args, " ") != "-title 🔔 done -subtitle octo/repo · PR #9 -message finished" {
			t.Errorf("notification args %q %v", path, args)
		}
		return errors.New("notifier failed")
	}
	notify(domain.PR{Owner: "octo", Repo: "repo", Number: 9}, "done", "finished", look, run)
	if !called {
		t.Fatal("notifier was not called")
	}
	notify(domain.PR{}, "done", "finished", func(string) (string, error) { return "", errors.New("missing") }, run)
}

func TestNotifyUsesInjectableRunners(t *testing.T) {
	oldLookPath, oldRun := lookPathRunner, commandRunner
	t.Cleanup(func() { lookPathRunner, commandRunner = oldLookPath, oldRun })
	called := false
	lookPathRunner = func(string) (string, error) { return "mock-notifier", nil }
	commandRunner = func(_ context.Context, path string, _ ...string) error {
		called = path == "mock-notifier"
		return errors.New("ignored notification failure")
	}
	Notify(domain.PR{Owner: "o", Repo: "r", Number: 1}, "title", "message")
	if !called {
		t.Fatal("Notify did not invoke injected command")
	}
}

func TestNotifyTimeoutBoundsCommand(t *testing.T) {
	called := false
	notifyWithTimeout(domain.PR{Owner: "owner", Repo: "repo", Number: 21}, "updated", "new revision", 5*time.Millisecond,
		func(string) (string, error) { return "notifier", nil },
		func(ctx context.Context, _ string, _ ...string) error {
			called = true
			<-ctx.Done()
			return ctx.Err()
		})
	if !called {
		t.Fatal("notification command was not started")
	}
}

func TestDefaultCommandRunner(t *testing.T) {
	if err := commandRunner(context.Background(), os.Args[0], "-test.run=^$"); err != nil {
		t.Fatalf("default command runner failed to invoke test binary: %v", err)
	}
}

func TestChangesMetadataAndHeadBoundChecks(t *testing.T) {
	old := domain.Snapshot{HeadSHA: "head", Title: "Title", Checks: []domain.Check{{Name: "unit", State: "failure"}}}
	newSnapshot := domain.Snapshot{HeadSHA: "new-head", Title: "", Checks: []domain.Check{{Name: "unit", State: "success"}}}
	events := Changes(old, newSnapshot)
	if len(events) != 2 || events[0].Kind != "push" || events[1].Kind != "metadata" {
		t.Fatalf("head/title changes = %#v", events)
	}
	if events[0].SHA != "new-head" || events[1].Detail != "" {
		t.Fatalf("event details = %#v", events)
	}
	if got := CI([]domain.Check{{State: " canceled "}}); got != "failed" {
		t.Fatalf("CI canceled = %q", got)
	}
}

func TestChangesCheckActivity(t *testing.T) {
	before := domain.Snapshot{HeadSHA: "same", Checks: []domain.Check{
		{Name: "zeta", State: "queued"},
		{Name: "alpha", State: "success"},
		{Name: "alpha", State: "waiting"},
		{Name: "removed", State: "failure"},
	}}
	after := domain.Snapshot{HeadSHA: "same", Checks: []domain.Check{
		{Name: "zeta", State: "in_progress"},
		{Name: "alpha", State: "WAITING"}, // Duplicate names are compared as a sorted group.
		{Name: "alpha", State: "SUCCESS"}, // Case normalization prevents duplicate activity.
		{Name: "added", State: "pending"},
	}}
	events := Changes(before, after)
	if len(events) != 3 {
		t.Fatalf("check changes = %#v", events)
	}
	want := []string{"added: added (pending)", "removed: removed (failure)", "zeta: queued → in_progress"}
	for i, event := range events {
		if event.Kind != "ci" || event.Source != "CI" || event.Detail != want[i] || event.SHA != "same" {
			t.Errorf("event %d = %#v; want detail %q", i, event, want[i])
		}
	}
	reordered := before
	reordered.Checks = []domain.Check{before.Checks[3], before.Checks[1], before.Checks[0], before.Checks[2]}
	if got := Changes(before, reordered); len(got) != 0 {
		t.Fatalf("check reorder generated events: %#v", got)
	}
	withRun := domain.Snapshot{HeadSHA: "same", Checks: []domain.Check{{Name: "matrix", State: "queued"}, {Name: "matrix", State: "success"}}}
	withoutRun := domain.Snapshot{HeadSHA: "same", Checks: []domain.Check{{Name: "matrix", State: "queued"}}}
	if got := Changes(withoutRun, withRun); len(got) != 1 || got[0].Detail != "matrix: queued → queued, success" {
		t.Fatalf("duplicate check addition = %#v", got)
	}
}
