package github

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"quick-review-cli/internal/domain"
)

type call struct {
	name string
	args []string
}

type stubRunner struct {
	calls []call
	fn    func(context.Context, string, ...string) ([]byte, error)
}

func (s *stubRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	s.calls = append(s.calls, call{name: name, args: append([]string(nil), args...)})
	return s.fn(ctx, name, args...)
}

func TestCheckAuth(t *testing.T) {
	if NewClient(nil).run == nil {
		t.Fatal("nil runner did not select the command runner")
	}
	t.Run("success", func(t *testing.T) {
		r := &stubRunner{fn: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "gh" || !reflect.DeepEqual(args, []string{"auth", "status"}) {
				t.Fatalf("unexpected call: %s %v", name, args)
			}
			return []byte("logged in"), nil
		}}
		if err := NewClient(r.run).CheckAuth(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("failure is wrapped", func(t *testing.T) {
		want := errors.New("no auth")
		c := NewClient(func(context.Context, string, ...string) ([]byte, error) { return nil, want })
		if err := c.CheckAuth(context.Background()); !errors.Is(err, want) || !strings.Contains(err.Error(), "authentication") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestSnapshotDecodesFieldsAndChecks(t *testing.T) {
	data := `{"title":"Fix it","url":"https://github.com/acme/app/pull/7","body":"details","state":"OPEN","headRefOid":"abc","baseRefOid":"def","baseRefName":"main","statusCheckRollup":[{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://ci/build","startedAt":"s","completedAt":"c"},{"__typename":"CheckRun","name":"pending","status":"IN_PROGRESS","conclusion":null,"detailsUrl":"https://ci/pending"},{"__typename":"CheckRun","name":"neutral","status":"COMPLETED","conclusion":"NEUTRAL"},{"__typename":"CheckRun","name":"skipped","status":"COMPLETED","conclusion":"SKIPPED"},{"__typename":"CheckRun","name":"queued","status":"QUEUED","state":"","conclusion":null},{"__typename":"StatusContext","context":"legacy","state":"ERROR","targetUrl":"https://ci/legacy","createdAt":"created","updatedAt":"updated"},{"__typename":"StatusContext","context":"queued","state":"EXPECTED","targetUrl":"https://ci/queued"}],"files":[{"path":"a.go","additions":2,"deletions":1}],"commits":[{"oid":"sha1","messageHeadline":"Headline"},{"commit":{"oid":"sha2","messageHeadline":"nested"}}]}`
	r := &stubRunner{fn: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "gh" || !reflect.DeepEqual(args, []string{"pr", "view", "7", "--repo", "acme/app", "--json", "title,body,state,headRefOid,baseRefOid,baseRefName,statusCheckRollup,files,commits,url"}) {
			t.Fatalf("unexpected call: %s %v", name, args)
		}
		return []byte(data), nil
	}}
	pr := domain.PR{Owner: "acme", Repo: "app", Number: 7}
	snapshot, err := NewClient(r.run).Snapshot(context.Background(), pr)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PR != pr || snapshot.Title != "Fix it" || snapshot.URL != "https://github.com/acme/app/pull/7" || snapshot.Body != "details" || snapshot.State != "OPEN" || snapshot.HeadSHA != "abc" || snapshot.BaseSHA != "def" || snapshot.BaseBranch != "main" {
		t.Fatalf("unexpected snapshot fields: %+v", snapshot)
	}
	if !reflect.DeepEqual(snapshot.Files, []domain.File{{Path: "a.go", Additions: 2, Deletions: 1}}) {
		t.Fatalf("files: %+v", snapshot.Files)
	}
	wantChecks := []domain.Check{
		{Name: "build", State: "SUCCESS", URL: "https://ci/build", StartedAt: "s", CompletedAt: "c"},
		{Name: "pending", State: "IN_PROGRESS", URL: "https://ci/pending"},
		{Name: "neutral", State: "NEUTRAL"},
		{Name: "skipped", State: "SKIPPED"},
		{Name: "queued", State: "QUEUED"},
		{Name: "legacy", State: "FAILURE", URL: "https://ci/legacy", StartedAt: "created", CompletedAt: "updated"},
		{Name: "queued", State: "PENDING", URL: "https://ci/queued"},
	}
	if !reflect.DeepEqual(snapshot.Checks, wantChecks) {
		t.Fatalf("checks:\n got %#v\nwant %#v", snapshot.Checks, wantChecks)
	}
	if !reflect.DeepEqual(snapshot.Commits, []domain.Commit{{SHA: "sha1", Title: "Headline"}, {SHA: "sha2", Title: "nested"}}) {
		t.Fatalf("commits: %+v", snapshot.Commits)
	}
}

func TestSnapshotErrorsAndEmptyRollup(t *testing.T) {
	valid := `{"statusCheckRollup":null,"files":[],"commits":[]}`
	for _, tc := range []struct {
		name string
		pr   domain.PR
		data string
		err  error
	}{
		{name: "zero number", pr: domain.PR{Owner: "o", Repo: "r"}, err: errors.New("invalid")},
		{name: "unsafe owner", pr: domain.PR{Owner: "../o", Repo: "r", Number: 1}, err: errors.New("invalid")},
		{name: "gh failure", pr: domain.PR{Owner: "o", Repo: "r", Number: 1}, err: errors.New("gh failed")},
		{name: "invalid json", pr: domain.PR{Owner: "o", Repo: "r", Number: 1}, data: `{`},
		{name: "malformed rollup", pr: domain.PR{Owner: "o", Repo: "r", Number: 1}, data: `{"statusCheckRollup":{}}`},
		{name: "malformed check", pr: domain.PR{Owner: "o", Repo: "r", Number: 1}, data: `{"statusCheckRollup":[2]}`},
		{name: "malformed commit", pr: domain.PR{Owner: "o", Repo: "r", Number: 1}, data: `{"commits":[{"oid":`},
		{name: "invalid commit value", pr: domain.PR{Owner: "o", Repo: "r", Number: 1}, data: `{"commits":[2]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			r := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
				calls++
				if tc.name == "gh failure" {
					return nil, tc.err
				}
				return []byte(tc.data), nil
			}
			_, err := NewClient(r).Snapshot(context.Background(), tc.pr)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.name == "zero number" || tc.name == "unsafe owner" {
				if calls != 0 {
					t.Fatalf("runner called %d times for invalid PR", calls)
				}
			}
		})
	}
	pr := domain.PR{Owner: "o", Repo: "r", Number: 1}
	snapshot, err := NewClient(func(context.Context, string, ...string) ([]byte, error) { return []byte(valid), nil }).Snapshot(context.Background(), pr)
	if err != nil || len(snapshot.Checks) != 0 {
		t.Fatalf("empty rollup = %+v, %v", snapshot, err)
	}
}

func TestNormalizeCheckStates(t *testing.T) {
	cases := []struct{ status, conclusion, want string }{
		{"QUEUED", "", "QUEUED"}, {"WAITING", "", "QUEUED"}, {"REQUESTED", "", "QUEUED"},
		{"PENDING", "", "QUEUED"}, {"IN_PROGRESS", "", "IN_PROGRESS"},
		{"COMPLETED", "SUCCESS", "SUCCESS"}, {"COMPLETED", "NEUTRAL", "NEUTRAL"},
		{"COMPLETED", "SKIPPED", "SKIPPED"}, {"COMPLETED", "CANCELLED", "CANCELLED"},
		{"COMPLETED", "CANCELED", "CANCELLED"}, {"COMPLETED", "FAILURE", "FAILURE"},
		{"COMPLETED", "TIMED_OUT", "FAILURE"}, {"COMPLETED", "ACTION_REQUIRED", "FAILURE"},
		{"COMPLETED", "STARTUP_FAILURE", "FAILURE"}, {"COMPLETED", "OTHER", "OTHER"},
		{"OTHER", "", "OTHER"}, {"OTHER", "SUCCESS", "SUCCESS"},
	}
	for _, tc := range cases {
		if got := normalizeCheckRun(tc.status, tc.conclusion); got != tc.want {
			t.Errorf("normalizeCheckRun(%q,%q) = %q, want %q", tc.status, tc.conclusion, got, tc.want)
		}
	}
	for _, tc := range []struct{ state, want string }{{"EXPECTED", "PENDING"}, {"ERROR", "FAILURE"}, {"FAILURE", "FAILURE"}, {"PENDING", "PENDING"}, {"SUCCESS", "SUCCESS"}, {"other", "OTHER"}} {
		if got := normalizeStatusContext(tc.state); got != tc.want {
			t.Errorf("normalizeStatusContext(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestCommandRunner(t *testing.T) {
	if _, err := commandRunner(context.Background(), "true"); err != nil {
		t.Fatal(err)
	}
	if _, err := commandRunner(context.Background(), "sh", "-c", "echo failed >&2; exit 9"); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected stderr in failure, got %v", err)
	}
}
