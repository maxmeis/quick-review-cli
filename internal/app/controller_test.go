package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"quick-review-cli/internal/codex"
	"quick-review-cli/internal/domain"
	"quick-review-cli/internal/session"
)

const (
	oldHead  = "1111111111111111111111111111111111111111"
	newHead  = "2222222222222222222222222222222222222222"
	lastHead = "3333333333333333333333333333333333333333"
	baseSHA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

type fakeGitHub struct {
	checkErr error
	snapshot domain.Snapshot
	err      error
	calls    int
	called   chan struct{}
}

func (f *fakeGitHub) CheckAuth(context.Context) error { return f.checkErr }
func (f *fakeGitHub) Snapshot(context.Context, domain.PR) (domain.Snapshot, error) {
	f.calls++
	if f.called != nil {
		select {
		case f.called <- struct{}{}:
		default:
		}
	}
	return f.snapshot, f.err
}

type fakeWorkspace struct {
	path       string
	prepareErr error
	seen       []domain.Snapshot
	prepared   []string
	called     chan struct{}
}

func (f *fakeWorkspace) Prepare(_ context.Context, _ domain.PR, s domain.Snapshot) (string, string, string, error) {
	f.seen = append(f.seen, s)
	f.prepared = append(f.prepared, s.HeadSHA)
	if f.called != nil {
		select {
		case f.called <- struct{}{}:
		default:
		}
	}
	return s.HeadSHA, s.BaseSHA, "diff for " + s.HeadSHA, f.prepareErr
}
func (f *fakeWorkspace) Path() string { return f.path }

type fakeAgent struct {
	events            chan codex.Message
	turnIDs           []string
	turnTexts         []string
	turnErr           error
	steerErr          error
	steered           []string
	interrupted       []string
	replies           []any
	rejections        []string
	closed            bool
	closedCh          chan struct{}
	initializeErr     error
	initializeStarted chan struct{}
	initializeRelease <-chan struct{}
	threadErr         error
	threadID          string
}

func newFakeAgent() *fakeAgent {
	return &fakeAgent{events: make(chan codex.Message, 16), threadID: "thread", closedCh: make(chan struct{})}
}
func (f *fakeAgent) Initialize(context.Context) error {
	if f.initializeStarted != nil {
		close(f.initializeStarted)
	}
	if f.initializeRelease != nil {
		<-f.initializeRelease
	}
	return f.initializeErr
}
func (f *fakeAgent) Thread(context.Context, string, string, string) (string, error) {
	if f.threadErr != nil {
		return "", f.threadErr
	}
	if f.threadID == "" {
		return "thread", nil
	}
	return f.threadID, nil
}
func (f *fakeAgent) Turn(_ context.Context, _, _, text string) (string, error) {
	f.turnTexts = append(f.turnTexts, text)
	if f.turnErr != nil {
		return "", f.turnErr
	}
	if len(f.turnIDs) == 0 {
		return fmt.Sprintf("turn-%d", len(f.turnTexts)), nil
	}
	id := f.turnIDs[0]
	f.turnIDs = f.turnIDs[1:]
	return id, nil
}
func (f *fakeAgent) Steer(_ context.Context, _, _, text string) error {
	f.steered = append(f.steered, text)
	return f.steerErr
}
func (f *fakeAgent) Interrupt(_ context.Context, _, turn string) error {
	f.interrupted = append(f.interrupted, turn)
	return nil
}
func (f *fakeAgent) Events() <-chan codex.Message { return f.events }
func (f *fakeAgent) Reply(_ json.RawMessage, response any) error {
	f.replies = append(f.replies, response)
	return nil
}
func (f *fakeAgent) Reject(_ json.RawMessage, reason string) error {
	f.rejections = append(f.rejections, reason)
	return nil
}
func (f *fakeAgent) Close() {
	if !f.closed {
		f.closed = true
		close(f.closedCh)
	}
}

func testSnapshot(head string) domain.Snapshot {
	return domain.Snapshot{
		PR:    domain.PR{Owner: "acme", Repo: "app", Number: 4},
		Title: "Test PR", State: "OPEN", HeadSHA: head, BaseSHA: baseSHA, BaseBranch: "main",
	}
}

func testController(t *testing.T) (*Controller, *fakeAgent, *[]*fakeWorkspace) {
	t.Helper()
	c := New(Config{Root: filepath.Join(t.TempDir(), "sessions"), PollInterval: time.Hour})
	agent := newFakeAgent()
	var workspaces []*fakeWorkspace
	c.client = agent
	c.state = domain.State{Snapshot: testSnapshot(oldHead), ThreadID: "thread", ReviewedHead: oldHead, Phase: "Reviewing", Connection: "Connected"}
	c.workspace = &fakeWorkspace{path: "/retained/old"}
	c.store, _ = session.NewStore(c.cfg.Root)
	c.makeWorkspace = func(path string) Workspace {
		w := &fakeWorkspace{path: path}
		workspaces = append(workspaces, w)
		return w
	}
	c.notify = func(domain.PR, string, string) {}
	c.publish()
	return c, agent, &workspaces
}

func TestControllerDefersAndCoalescesPushUntilReviewFinishes(t *testing.T) {
	c, agent, created := testController(t)
	c.activeTurn = "review-1"
	c.reviewTurn = true
	c.onPoll(context.Background(), polled{snapshot: testSnapshot(newHead)})
	if !c.refreshPending || c.workspace.Path() != "/retained/old" || len(*created) != 0 {
		t.Fatalf("push mutated or failed to queue current checkout: pending=%v path=%q workspaces=%d", c.refreshPending, c.workspace.Path(), len(*created))
	}
	c.onPoll(context.Background(), polled{snapshot: testSnapshot(lastHead)})
	if len(c.state.Reports) != 0 || c.state.Snapshot.HeadSHA != lastHead || len(*created) != 0 {
		t.Fatalf("coalesced update state: head=%q reports=%d newWorkspaces=%d", c.state.Snapshot.HeadSHA, len(c.state.Reports), len(*created))
	}
	message, _ := json.Marshal(map[string]any{"turn": map[string]any{"id": "review-1", "status": "failed"}})
	c.onProtocol(context.Background(), codex.Message{Method: "turn/completed", Params: message})
	waitFor(t, func() bool { return len(c.jobs) > 0 })
	result := <-c.jobs
	p, ok := result.(prepared)
	if !ok {
		t.Fatalf("refresh job has type %T", result)
	}
	if p.snapshot.HeadSHA != lastHead || len(*created) != 1 || (*created)[0].seen[0].HeadSHA != lastHead {
		t.Fatalf("refreshed stale intermediate revision: snapshot=%s workspaces=%+v", p.snapshot.HeadSHA, *created)
	}
	c.onPrepared(context.Background(), p)
	if c.state.ReviewedHead != lastHead || c.workspace.Path() != (*created)[0].path || len(agent.turnTexts) != 1 {
		t.Fatalf("latest review was not started: reviewed=%s path=%s turns=%d", c.state.ReviewedHead, c.workspace.Path(), len(agent.turnTexts))
	}
}

func TestControllerCIChangesRetryAndClosure(t *testing.T) {
	c, agent, _ := testController(t)
	c.activeTurn = "review-1"
	before := testSnapshot(oldHead)
	before.Checks = []domain.Check{{Name: "build", State: "IN_PROGRESS"}}
	c.state.Snapshot = before
	after := before
	after.Checks = []domain.Check{{Name: "build", State: "FAILURE"}}
	c.gh = &fakeGitHub{snapshot: after}
	c.poll(context.Background())
	waitFor(t, func() bool { return len(c.jobs) > 0 })
	c.onPoll(context.Background(), (<-c.jobs).(polled))
	ciEvent := false
	for _, event := range c.state.Events {
		ciEvent = ciEvent || event.Kind == "ci"
	}
	if !ciEvent || c.state.Connection != "Connected" || len(agent.steered) != 1 {
		t.Fatalf("CI update not published: %+v", c.state)
	}
	c.onPoll(context.Background(), polled{err: errors.New("offline")})
	if c.pollFailures != 1 || c.state.Connection != "GitHub stale · retrying" || c.state.Error != "offline" || !c.nextPoll.After(time.Now()) {
		t.Fatalf("poll failure was not retried: failures=%d connection=%q error=%q next=%v", c.pollFailures, c.state.Connection, c.state.Error, c.nextPoll)
	}
	closed := after
	closed.State = "CLOSED"
	c.onPoll(context.Background(), polled{snapshot: closed})
	if !c.state.ClosedPrompt || c.pollFailures != 0 || c.state.Connection != "Connected" {
		t.Fatalf("closure was not reflected: %+v", c.state)
	}
	c.client = nil
	c.state.Connection = "Codex disconnected"
	c.state.Error = "Codex disconnected. Use /resume to reconnect."
	c.onPoll(context.Background(), polled{snapshot: closed})
	if c.state.Connection != "Codex disconnected · GitHub connected" || c.state.Error == "" {
		t.Fatalf("GitHub poll masked Codex disconnect: connection=%q error=%q", c.state.Connection, c.state.Error)
	}
	c.state.Connection = "Not connected"
	c.state.Error = "Codex startup failed"
	c.onPoll(context.Background(), polled{snapshot: closed})
	if c.state.Connection != "GitHub connected · Codex unavailable" || c.state.Error != "Codex startup failed" {
		t.Fatalf("GitHub poll masked Codex startup failure: connection=%q error=%q", c.state.Connection, c.state.Error)
	}
}

func TestControllerStartRunsAsyncSetup(t *testing.T) {
	c := New(Config{Root: filepath.Join(t.TempDir(), "sessions")})
	snapshot := testSnapshot(oldHead)
	c.gh = &fakeGitHub{snapshot: snapshot}
	agent := newFakeAgent()
	c.startClient = func(context.Context) (AgentClient, error) { return agent, nil }
	var workspace *fakeWorkspace
	c.makeWorkspace = func(path string) Workspace {
		workspace = &fakeWorkspace{path: path}
		return workspace
	}
	c.notify = func(domain.PR, string, string) {}
	c.start(context.Background(), "https://github.com/acme/app/pull/4")
	if !c.preparing || c.state.Phase != "Connecting" {
		t.Fatalf("start did not begin setup: preparing=%v phase=%s", c.preparing, c.state.Phase)
	}
	c.start(context.Background(), "https://github.com/acme/app/pull/4")
	if !strings.Contains(c.state.Error, "already active") {
		t.Fatalf("duplicate start was not rejected: %q", c.state.Error)
	}
	waitFor(t, func() bool { return len(c.jobs) > 0 })
	p := (<-c.jobs).(prepared)
	if p.err != nil || p.snapshot.HeadSHA != oldHead || p.head != oldHead || p.client != agent || p.thread != "thread" {
		t.Fatalf("unexpected prepared result: %+v", p)
	}
	c.onPrepared(context.Background(), p)
	if c.preparing || c.state.Phase != "Reviewing" || c.state.ReviewedHead != oldHead || c.workspace != workspace || len(agent.turnTexts) != 1 {
		t.Fatalf("setup did not transition to review: preparing=%v phase=%s head=%s turns=%d", c.preparing, c.state.Phase, c.state.ReviewedHead, len(agent.turnTexts))
	}
}

func TestControllerStartRejectsWhilePolling(t *testing.T) {
	c := New(Config{})
	c.polling = true
	gh := &fakeGitHub{snapshot: testSnapshot(oldHead)}
	c.gh = gh
	c.start(context.Background(), "https://github.com/acme/app/pull/4")
	if gh.calls != 0 || c.preparing || !strings.Contains(c.state.Error, "already active") {
		t.Fatalf("start raced an active poll: calls=%d preparing=%v error=%q", gh.calls, c.preparing, c.state.Error)
	}
}

func TestControllerStartFailuresAreReported(t *testing.T) {
	for _, stage := range []string{"auth", "snapshot", "store", "workspace", "client", "initialize", "thread"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New(stage + " failed")
			root := filepath.Join(t.TempDir(), "sessions")
			if stage == "store" {
				root = filepath.Join(t.TempDir(), "root-file")
				if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			c := New(Config{Root: root})
			c.gh = &fakeGitHub{snapshot: testSnapshot(oldHead)}
			if stage == "auth" {
				c.gh = &fakeGitHub{checkErr: failure}
			}
			if stage == "snapshot" {
				c.gh = &fakeGitHub{err: failure}
			}
			c.makeWorkspace = func(path string) Workspace {
				w := &fakeWorkspace{path: path}
				if stage == "workspace" {
					w.prepareErr = failure
				}
				return w
			}
			agent := newFakeAgent()
			if stage == "initialize" {
				agent.initializeErr = failure
			}
			if stage == "thread" {
				agent.threadErr = failure
			}
			c.startClient = func(context.Context) (AgentClient, error) {
				if stage == "client" {
					return nil, failure
				}
				return agent, nil
			}
			c.start(context.Background(), "https://github.com/acme/app/pull/4")
			waitFor(t, func() bool { return len(c.jobs) > 0 })
			p := (<-c.jobs).(prepared)
			if p.err == nil || (stage != "store" && !errors.Is(p.err, failure)) {
				t.Fatalf("%s error lost: %v", stage, p.err)
			}
			c.onPrepared(context.Background(), p)
			if c.state.Phase != "Error" || c.state.Error == "" {
				t.Fatalf("%s error not shown: phase=%s error=%s", stage, c.state.Phase, c.state.Error)
			}
			if (stage == "initialize" || stage == "thread") && !agent.closed {
				t.Fatalf("agent was not closed after %s failure", stage)
			}
		})
	}
}

func TestControllerStartClosesPreparedClientWhenCanceled(t *testing.T) {
	c := New(Config{Root: filepath.Join(t.TempDir(), "sessions")})
	c.gh = &fakeGitHub{snapshot: testSnapshot(oldHead)}
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.store = store
	c.makeWorkspace = func(path string) Workspace { return &fakeWorkspace{path: path} }
	agent := newFakeAgent()
	c.startClient = func(context.Context) (AgentClient, error) { return agent, nil }
	for i := 0; i < cap(c.jobs); i++ {
		c.jobs <- struct{}{} // Force the completed setup to select the canceled context.
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.start(ctx, "https://github.com/acme/app/pull/4")
	cancel()
	select {
	case <-agent.closedCh:
	case <-time.After(time.Second):
		t.Fatal("prepared agent was not closed after cancellation")
	}
}

func TestControllerRunClosesPreparedClientDuringTeardown(t *testing.T) {
	c := New(Config{Root: filepath.Join(t.TempDir(), "sessions"), URL: "https://github.com/acme/app/pull/4"})
	c.gh = &fakeGitHub{snapshot: testSnapshot(oldHead)}
	c.makeWorkspace = func(path string) Workspace { return &fakeWorkspace{path: path} }
	agent := newFakeAgent()
	agent.initializeStarted = make(chan struct{})
	releaseInitialize := make(chan struct{})
	agent.initializeRelease = releaseInitialize
	c.startClient = func(context.Context) (AgentClient, error) { return agent, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, nil) }()
	<-c.Updates()
	select {
	case <-agent.initializeStarted:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("setup did not reach agent initialization")
	}
	cancel() // Run exits its event loop, then waits for this in-flight setup worker.
	time.Sleep(20 * time.Millisecond)
	close(releaseInitialize)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not finish after its setup worker completed")
	}
	if !agent.closed {
		t.Fatal("prepared Codex client was leaked during Run teardown")
	}
}

func TestControllerCloseQueuedClientsClosesUnconsumedPreparedClient(t *testing.T) {
	c := New(Config{})
	agent := newFakeAgent()
	c.jobs <- polled{snapshot: testSnapshot(oldHead)}
	c.jobs <- prepared{client: agent}
	c.closeQueuedClients()
	if !agent.closed || len(c.jobs) != 0 {
		t.Fatalf("queued setup client was not closed and drained: closed=%v queued=%d", agent.closed, len(c.jobs))
	}
}

func TestControllerPreparedRevisionRaceAndPause(t *testing.T) {
	c, agent, created := testController(t)
	latest := testSnapshot(lastHead)
	c.state.Snapshot = latest
	older := testSnapshot(newHead)
	w := &fakeWorkspace{path: "/retained/old"}
	c.onPrepared(context.Background(), prepared{snapshot: older, head: newHead, base: baseSHA, diff: "old", ws: w, store: c.store})
	if !c.refreshPending || !c.preparing {
		t.Fatalf("stale prepared result was installed instead of refreshed: pending=%v preparing=%v workspaces=%+v", c.refreshPending, c.preparing, *created)
	}
	waitFor(t, func() bool { return len(c.jobs) > 0 })
	p := (<-c.jobs).(prepared)
	if p.snapshot.HeadSHA != lastHead || len(*created) != 1 || (*created)[0].seen[0].HeadSHA != lastHead {
		t.Fatalf("prepared wrong revision after race: %s", p.snapshot.HeadSHA)
	}
	c.state.Paused = true
	c.preparing = false
	c.onPrepared(context.Background(), prepared{snapshot: latest, head: lastHead, base: baseSHA, diff: "latest", ws: &fakeWorkspace{path: "/latest"}, store: c.store})
	if c.state.Phase != "Paused" || len(agent.turnTexts) != 0 {
		t.Fatalf("prepared checkout started while paused: phase=%s turns=%d", c.state.Phase, len(agent.turnTexts))
	}
}

func TestControllerPreparedNewSnapshotStalesSavedReports(t *testing.T) {
	c, _, _ := testController(t)
	c.state.Reports = []domain.Report{
		{HeadSHA: oldHead, BaseSHA: "merge-base-sha", Text: "saved report"},
		{HeadSHA: "older", Stale: true},
	}
	newSnapshot := testSnapshot(newHead)
	newSnapshot.BaseSHA = "new-base-tip"
	agent := newFakeAgent()
	c.onPrepared(context.Background(), prepared{
		snapshot: newSnapshot, head: newHead, base: "new-merge-base", diff: "new diff",
		ws: &fakeWorkspace{path: "/new"}, store: c.store, client: agent, thread: "thread",
	})
	if !c.state.Reports[0].Stale || !c.state.Reports[1].Stale {
		t.Fatalf("saved reports were not retained as stale after resumed revision changed: %+v", c.state.Reports)
	}
	if c.state.Snapshot.HeadSHA != newHead || c.state.Snapshot.BaseSHA != "new-base-tip" {
		t.Fatalf("new snapshot not installed: %+v", c.state.Snapshot)
	}
}

func TestControllerPauseResumeAndPrepareFailure(t *testing.T) {
	c, agent, created := testController(t)
	c.activeTurn = "review-1"
	c.childTurns["reviewer"] = "child-turn"
	c.action(context.Background(), domain.Action{Kind: "pause"})
	if !c.state.Paused || c.state.Phase != "Paused" || len(agent.interrupted) != 2 {
		t.Fatalf("pause did not stop turns: paused=%v phase=%s interrupted=%v", c.state.Paused, c.state.Phase, agent.interrupted)
	}
	c.refreshPending = true
	// Model the interrupted turn completing before the user resumes.
	c.activeTurn = ""
	c.action(context.Background(), domain.Action{Kind: "resume"})
	if c.state.Paused || !c.preparing {
		t.Fatalf("resume did not schedule refresh: paused=%v preparing=%v", c.state.Paused, c.preparing)
	}
	waitFor(t, func() bool { return len(c.jobs) > 0 })
	p := (<-c.jobs).(prepared)
	c.onPrepared(context.Background(), p)
	if len(*created) != 1 || c.state.Phase != "Reviewing" {
		t.Fatalf("resume did not prepare and restart: workspaces=%d phase=%s", len(*created), c.state.Phase)
	}

	c.preparing = true
	failure := errors.New("checkout failed")
	c.onPrepared(context.Background(), prepared{err: failure})
	if c.state.Phase != "Error" || c.state.Error != failure.Error() {
		t.Fatalf("prepare failure not shown: phase=%q error=%q", c.state.Phase, c.state.Error)
	}
}

func TestControllerSteerFailureQueuesMessage(t *testing.T) {
	c, agent, _ := testController(t)
	c.activeTurn = "turn-1"
	agent.steerErr = errors.New("turn changed")
	c.action(context.Background(), domain.Action{Kind: "message", Text: "please inspect edge case"})
	if len(agent.steered) != 1 || len(c.pending) != 1 || c.pending[0] != "please inspect edge case" {
		t.Fatalf("failed steer was not queued: steered=%v pending=%v", agent.steered, c.pending)
	}
	if len(c.state.Events) < 2 || c.state.Events[len(c.state.Events)-1].Kind != "queued" {
		t.Fatalf("queue event missing: %+v", c.state.Events)
	}
	c.activeTurn = ""
	c.drain(context.Background())
	if len(c.pending) != 0 || len(agent.turnTexts) != 1 || !strings.Contains(agent.turnTexts[0], "please inspect edge case") {
		t.Fatalf("queued message not submitted after turn: pending=%v turns=%v", c.pending, agent.turnTexts)
	}
}

func TestCancelQuitDoesNotStartConcurrentTurns(t *testing.T) {
	c, agent, _ := testController(t)
	c.activeTurn = "review-1"
	c.pending = []string{"queued message"}
	c.state.QuitRequested = true
	c.action(context.Background(), domain.Action{Kind: "cancel-quit"})
	if len(agent.turnTexts) != 0 || len(c.pending) != 1 {
		t.Fatalf("cancel quit started an overlapping turn or discarded queued input: turns=%v pending=%v", agent.turnTexts, c.pending)
	}
	c.activeTurn = ""
	c.preparing = true
	c.state.QuitRequested = true
	c.action(context.Background(), domain.Action{Kind: "cancel-quit"})
	if len(agent.turnTexts) != 0 || len(c.pending) != 1 {
		t.Fatalf("cancel quit started a turn during setup: turns=%v pending=%v", agent.turnTexts, c.pending)
	}
}

func TestControllerRefreshAndPrepareLatestGuards(t *testing.T) {
	c, _, created := testController(t)
	c.gh = &fakeGitHub{snapshot: c.state.Snapshot}
	c.preparing = true
	c.action(context.Background(), domain.Action{Kind: "refresh"})
	if c.polling {
		t.Fatal("manual refresh started while a checkout was preparing")
	}
	c.preparing = false
	c.action(context.Background(), domain.Action{Kind: "refresh"})
	if !c.polling {
		t.Fatal("manual refresh did not start polling")
	}
	c.action(context.Background(), domain.Action{Kind: "refresh"})
	if !c.polling {
		t.Fatal("second manual refresh cleared the active poll")
	}
	waitFor(t, func() bool { return len(c.jobs) > 0 })
	c.onPoll(context.Background(), (<-c.jobs).(polled))

	c.refreshPending = true
	c.preparing = true
	c.prepareLatest(context.Background())
	if len(*created) != 0 {
		t.Fatal("prepareLatest started while already preparing")
	}
	c.preparing = false
	c.activeTurn = "active"
	c.prepareLatest(context.Background())
	if len(*created) != 0 {
		t.Fatal("prepareLatest changed checkout during a turn")
	}
	c.activeTurn = ""
	c.state.Paused = true
	c.prepareLatest(context.Background())
	if len(*created) != 0 {
		t.Fatal("prepareLatest changed checkout while paused")
	}
	c.state.Paused = false
	c.client = nil
	c.gh = &fakeGitHub{snapshot: c.state.Snapshot}
	agent := newFakeAgent()
	c.startClient = func(context.Context) (AgentClient, error) { return agent, nil }
	c.action(context.Background(), domain.Action{Kind: "resume"})
	if !c.preparing {
		t.Fatal("prepareLatest did not reconnect a disconnected agent")
	}
	waitFor(t, func() bool { return len(c.jobs) > 0 })
	p := (<-c.jobs).(prepared)
	c.onPrepared(context.Background(), p)
	if len(agent.turnTexts) != 1 || c.state.Connection != "Connected" {
		t.Fatalf("disconnected agent was not reconnected: turns=%d connection=%s", len(agent.turnTexts), c.state.Connection)
	}
	c.activeTurn = ""
	c.client = nil
	c.prepareLatest(context.Background())
	waitFor(t, func() bool { return len(c.jobs) > 0 })
	if prepared := (<-c.jobs).(prepared); prepared.err != nil || prepared.client != agent {
		t.Fatalf("prepareLatest did not route disconnected client through session start: %+v", prepared)
	}
}

func TestControllerRetainsReportsAndQueuedWatcher(t *testing.T) {
	c, agent, _ := testController(t)
	c.state.Reports = []domain.Report{{Path: "report.md", HeadSHA: oldHead}}
	c.activeTurn = "review-1"
	c.onPoll(context.Background(), polled{snapshot: testSnapshot(newHead)})
	if !c.state.Reports[0].Stale || !c.refreshPending {
		t.Fatalf("new head did not stale prior report: %+v", c.state.Reports)
	}
	// Simulate an active turn that has completed before the next state update.
	c.activeTurn = ""
	c.refreshPending = false
	c.watchContext = "CI changed"
	c.drain(context.Background())
	if len(agent.turnTexts) != 1 || !strings.Contains(agent.turnTexts[0], "CI changed") || c.watchContext != "" {
		t.Fatalf("watcher context did not start a follow-up turn: turns=%v watcher=%q", agent.turnTexts, c.watchContext)
	}
}

func TestControllerTurnFailureAndIdleGuards(t *testing.T) {
	c, agent, _ := testController(t)
	agent.turnErr = errors.New("turn failed")
	c.startTurn(context.Background(), "message", false)
	if c.state.Phase != "Error" || c.state.Error != "turn failed" {
		t.Fatalf("turn failure not reported: phase=%s error=%s", c.state.Phase, c.state.Error)
	}
	c.pending = []string{"keep this message"}
	c.watchContext = "watch facts"
	c.drain(context.Background())
	if len(c.pending) != 1 || c.pending[0] != "keep this message" || c.watchContext != "watch facts" {
		t.Fatalf("failed turn discarded queued context: pending=%v watcher=%q", c.pending, c.watchContext)
	}
	c.client = nil
	c.startTurn(context.Background(), "ignored", true)
	if len(agent.turnTexts) != 2 {
		t.Fatalf("startTurn without a client submitted work: %v", agent.turnTexts)
	}
	c.client = agent
	c.workspace = nil
	c.startTurn(context.Background(), "ignored", true)
	if len(agent.turnTexts) != 2 {
		t.Fatalf("startTurn without a workspace submitted work: %v", agent.turnTexts)
	}
	c.state.ClosedPrompt = true
	c.drain(context.Background())
	if len(agent.turnTexts) != 2 {
		t.Fatalf("drain ignored the closed PR prompt: %v", agent.turnTexts)
	}
}

func TestControllerPersistenceAndUtilityPaths(t *testing.T) {
	c, _, _ := testController(t)
	c.pending = []string{"follow up"}
	c.watchContext = "new revision observed"
	c.refreshPending = true
	c.publish()
	saved, err := c.store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(strings.Join(saved.PendingMessages, ","), "follow up") || saved.WatcherContext != c.watchContext || !saved.RefreshPending {
		t.Fatalf("queued work was not persisted: %+v", saved)
	}
	storeDir := c.store.Dir()
	if err := os.RemoveAll(storeDir); err != nil {
		t.Fatal(err)
	}
	c.publish()
	if !strings.Contains(c.state.Error, "Could not save session") {
		t.Fatalf("state persistence failure missing: %q", c.state.Error)
	}
	c.event("App", "test", "event", "", "")
	if !strings.Contains(c.state.Error, "Could not save activity") {
		t.Fatalf("event persistence failure missing: %q", c.state.Error)
	}
	originalLookup := userConfigDir
	t.Cleanup(func() { userConfigDir = originalLookup })
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	userConfigDir = func() (string, error) { return os.Getenv("XDG_CONFIG_HOME"), nil }
	root, err := DefaultRoot()
	if err != nil || root != filepath.Join(configHome, "quick-review", "sessions") {
		t.Fatalf("DefaultRoot = %q, %v", root, err)
	}
	userConfigDir = func() (string, error) { return "", errors.New("config unavailable") }
	if _, err := DefaultRoot(); err == nil || err.Error() != "config unavailable" {
		t.Fatalf("DefaultRoot did not propagate config lookup error: %v", err)
	}
	if err := openTarget("http://example.com"); err == nil {
		t.Fatal("openTarget accepted a non-HTTPS URL")
	}
	for _, tc := range []struct {
		goos string
		name string
		args []string
	}{
		{goos: "darwin", name: "open", args: []string{"https://example.com/review"}},
		{goos: "linux", name: "xdg-open", args: []string{"https://example.com/review"}},
		{goos: "windows", name: "rundll32", args: []string{"url.dll,FileProtocolHandler", "https://example.com/review"}},
	} {
		name, args := openCommand("https://example.com/review", tc.goos)
		if name != tc.name || strings.Join(args, "\x00") != strings.Join(tc.args, "\x00") {
			t.Errorf("openCommand(%q) = %q %q; want %q %q", tc.goos, name, args, tc.name, tc.args)
		}
	}

	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "opened-args")
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	} else if runtime.GOOS == "windows" {
		command = "rundll32"
	}
	opener := filepath.Join(bin, command)
	if err := os.WriteFile(opener, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+shellQuote(logPath)+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := openTarget("https://example.com/review"); err != nil {
		t.Fatalf("fake HTTPS opener failed: %v", err)
	}
	opened, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), "https://example.com/review") {
		t.Fatalf("opener did not receive the requested URL: %q", opened)
	}
	if err := os.WriteFile(opener, []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := openTarget("/tmp/report.md"); err == nil {
		t.Fatal("openTarget did not propagate opener failure")
	}

	defaults := New(Config{})
	if defaults.cfg.PollInterval <= 0 {
		t.Fatal("New did not set a default polling interval")
	}
	if w := defaults.makeWorkspace(filepath.Join(t.TempDir(), "checkout")); w.Path() == "" {
		t.Fatal("default workspace factory returned an empty path")
	}
	defaults.open = func(string) error { return errors.New("open failed") }
	defaults.action(context.Background(), domain.Action{Kind: "open", Text: "/tmp/report.md"})
	if defaults.state.Error != "open failed" {
		t.Fatalf("open action failure missing: %q", defaults.state.Error)
	}
}

func TestControllerDefaultCodexStartUsesExecutable(t *testing.T) {
	bin := t.TempDir()
	program := filepath.Join(bin, "codex")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := New(Config{})
	client, err := c.startClient(context.Background())
	if err != nil {
		t.Fatalf("default Codex starter did not launch fake executable: %v", err)
	}
	client.Close()
}

func TestControllerPublishWithNoListenerReplacesUpdate(t *testing.T) {
	c := New(Config{})
	c.updates = make(chan domain.State)
	done := make(chan struct{})
	go func() {
		c.publish()
		close(done)
	}()
	time.Sleep(5 * time.Millisecond)
	select {
	case state := <-c.updates:
		if state.Phase != "Ready" {
			t.Fatalf("published unexpected state: %+v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("publish did not finish after listener arrived")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish remained blocked")
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestControllerQuitActionsAndInvalidStart(t *testing.T) {
	c, _, _ := testController(t)
	c.action(context.Background(), domain.Action{Kind: "quit"})
	if !c.state.QuitRequested {
		t.Fatal("quit did not request confirmation")
	}
	c.action(context.Background(), domain.Action{Kind: "cancel-quit"})
	if c.state.QuitRequested {
		t.Fatal("cancel quit left prompt active")
	}
	if !c.action(context.Background(), domain.Action{Kind: "confirm-quit"}) {
		t.Fatal("confirm quit did not request exit")
	}
	c.start(context.Background(), "not a URL")
	if c.state.Error == "" || !strings.Contains(c.state.Error, "https://github.com") {
		t.Fatalf("invalid start did not show parse error: %q", c.state.Error)
	}
	c.start(context.Background(), "https://github.com/other/repo/pull/2")
	if !strings.Contains(c.state.Error, "new review session") {
		t.Fatalf("second PR was not rejected: %q", c.state.Error)
	}
}

func TestControllerActionBranches(t *testing.T) {
	c, agent, created := testController(t)
	c.action(context.Background(), domain.Action{Kind: "start", Text: "invalid"})
	if !strings.Contains(c.state.Error, "https://github.com") {
		t.Fatalf("start action did not call start: %q", c.state.Error)
	}
	beforeEvents := len(c.state.Events)
	c.action(context.Background(), domain.Action{Kind: "message", Text: "  \n"})
	if len(c.state.Events) != beforeEvents {
		t.Fatal("blank message generated an event")
	}
	c.activeTurn = "turn-1"
	agent.steerErr = nil
	c.action(context.Background(), domain.Action{Kind: "message", Text: "add one more check"})
	if len(agent.steered) != 1 || len(c.pending) != 0 || c.state.Events[len(c.state.Events)-1].Kind != "submitted" {
		t.Fatalf("successful steer was not recorded: steered=%v pending=%v events=%+v", agent.steered, c.pending, c.state.Events)
	}
	c.activeTurn = ""
	c.client = nil
	c.action(context.Background(), domain.Action{Kind: "message", Text: "queue while disconnected"})
	if len(c.pending) != 1 || c.pending[0] != "queue while disconnected" {
		t.Fatalf("offline message was not retained: %v", c.pending)
	}
	c.client = agent
	c.requests["7"] = &request{id: json.RawMessage(`7`), method: "approval", remaining: 1}
	c.state.Questions = []domain.Question{{ID: "7"}}
	c.action(context.Background(), domain.Action{Kind: "answer", ID: "7", Text: "Approve once"})
	if len(agent.replies) != 1 || len(c.state.Questions) != 0 {
		t.Fatalf("answer action was not handled: replies=%v questions=%v", agent.replies, c.state.Questions)
	}
	c.state.ClosedPrompt = true
	c.action(context.Background(), domain.Action{Kind: "cancel-quit"})
	if c.state.ClosedPrompt {
		t.Fatal("cancel quit did not clear closed PR prompt")
	}
	if len(*created) != 0 {
		t.Fatalf("action branch unexpectedly created workspaces: %d", len(*created))
	}
	c.open = func(string) error { return nil }
	c.action(context.Background(), domain.Action{Kind: "open", Text: "/tmp/report.md"})
}

func TestControllerPrepareAndPollCancelBlockedJobs(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, _, _ := testController(t)
	for i := 0; i < cap(c.jobs); i++ {
		c.jobs <- struct{}{}
	}
	pctx, pcancel := context.WithCancel(context.Background())
	cancelWorkspace := &fakeWorkspace{path: "/cancelled", called: make(chan struct{}, 1)}
	c.makeWorkspace = func(string) Workspace { return cancelWorkspace }
	c.store = store
	c.state.Snapshot = testSnapshot(newHead)
	c.prepareLatest(pctx)
	select {
	case <-cancelWorkspace.called:
	case <-time.After(time.Second):
		t.Fatal("prepareLatest worker did not run")
	}
	pcancel()

	gh := &fakeGitHub{snapshot: c.state.Snapshot, called: make(chan struct{}, 1)}
	c.gh = gh
	qctx, qcancel := context.WithCancel(context.Background())
	c.poll(qctx)
	select {
	case <-gh.called:
	case <-time.After(time.Second):
		t.Fatal("poll worker did not run")
	}
	qcancel()
	// The full queue plus canceled context makes each worker take its cancellation
	// branch instead of blocking while trying to enqueue its result.
	time.Sleep(5 * time.Millisecond)
	if len(c.jobs) != cap(c.jobs) {
		t.Fatalf("canceled jobs replaced queued results: len=%d cap=%d", len(c.jobs), cap(c.jobs))
	}
}

func TestControllerRunConfirmQuitAndContextStop(t *testing.T) {
	t.Run("confirm quit retains session", func(t *testing.T) {
		c, agent, _ := testController(t)
		actions := make(chan domain.Action, 3)
		actions <- domain.Action{Kind: "quit"}
		actions <- domain.Action{Kind: "cancel-quit"}
		actions <- domain.Action{Kind: "confirm-quit"}
		close(actions)
		if err := c.Run(context.Background(), actions); err != nil {
			t.Fatal(err)
		}
		if !agent.closed || c.state.Phase != "Stopped" {
			t.Fatalf("Run did not stop cleanly: closed=%v phase=%s", agent.closed, c.state.Phase)
		}
		last := <-c.Updates()
		if last.Phase != "Stopped" || len(c.state.Events) == 0 || c.state.Events[len(c.state.Events)-1].Kind != "exit" {
			t.Fatalf("final state not published: %+v", last)
		}
	})
	t.Run("context cancellation", func(t *testing.T) {
		c := New(Config{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Run(ctx, make(chan domain.Action)); err != nil {
			t.Fatal(err)
		}
		if c.state.Phase != "Stopped" {
			t.Fatalf("context cancellation phase = %q", c.state.Phase)
		}
		if last := <-c.Updates(); last.Phase != "Stopped" {
			t.Fatalf("stopped state not published: %+v", last)
		}
	})
}

func TestControllerRunResumeAndInputErrors(t *testing.T) {
	t.Run("resume path must be a directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		c := New(Config{ResumeDir: file})
		if err := c.Run(context.Background(), nil); err == nil {
			t.Fatal("expected resume open failure")
		}
	})
	t.Run("missing saved state is reported", func(t *testing.T) {
		c := New(Config{ResumeDir: filepath.Join(t.TempDir(), "empty-session")})
		if err := c.Run(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "read session state") {
			t.Fatalf("expected saved-state read failure, got %v", err)
		}
	})
	t.Run("resume resets transient prompts", func(t *testing.T) {
		store, err := session.NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		saved := domain.State{
			Phase: "Reviewing", DraftReply: "partial", QuitRequested: true, ClosedPrompt: true,
			Questions: []domain.Question{{ID: "q"}}, ReviewedHead: oldHead,
			PendingMessages: []string{"resume message"}, WatcherContext: "resume watch", RefreshPending: true,
		}
		if err := store.SaveState(saved); err != nil {
			t.Fatal(err)
		}
		c := New(Config{ResumeDir: store.Dir()})
		actions := make(chan domain.Action, 1)
		actions <- domain.Action{Kind: "confirm-quit"}
		close(actions)
		if err := c.Run(context.Background(), actions); err != nil {
			t.Fatal(err)
		}
		if c.state.Phase != "Stopped" || c.state.DraftReply != "" || len(c.state.Questions) != 0 || c.state.QuitRequested || c.state.ClosedPrompt {
			t.Fatalf("transient state survived resume: %+v", c.state)
		}
		foundResume := false
		for _, event := range c.state.Events {
			foundResume = foundResume || event.Kind == "resume"
		}
		if !foundResume {
			t.Fatalf("resume event missing: %+v", c.state.Events)
		}
		if len(c.pending) != 1 || c.pending[0] != "resume message" || c.watchContext != "resume watch" || !c.refreshPending {
			t.Fatalf("pending work not restored: pending=%v watcher=%q refresh=%v", c.pending, c.watchContext, c.refreshPending)
		}
	})
}

func TestControllerRunHandlesJobsAndDisconnect(t *testing.T) {
	t.Run("poll job updates published state", func(t *testing.T) {
		c, _, _ := testController(t)
		actions := make(chan domain.Action)
		done := make(chan error, 1)
		go func() { done <- c.Run(context.Background(), actions) }()
		<-c.Updates() // initial state
		c.jobs <- polled{err: errors.New("offline")}
		var update domain.State
		for update.Connection != "GitHub stale · retrying" {
			update = <-c.Updates()
		}
		actions <- domain.Action{Kind: "confirm-quit"}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("closed agent event stream disconnects", func(t *testing.T) {
		c, agent, _ := testController(t)
		close(agent.events)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if err := c.Run(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if c.client != nil || c.state.Connection != "Codex disconnected" || c.state.Phase != "Stopped" || !agent.closed {
			t.Fatalf("disconnect handling failed: client=%v connection=%q phase=%q closed=%v", c.client, c.state.Connection, c.state.Phase, agent.closed)
		}
	})
}

func TestControllerRunPreparedEventAndTickerCases(t *testing.T) {
	t.Run("prepared job is consumed", func(t *testing.T) {
		c := New(Config{})
		c.jobs <- prepared{err: errors.New("prepare failed")}
		actions := make(chan domain.Action)
		done := make(chan error, 1)
		go func() { done <- c.Run(context.Background(), actions) }()
		<-c.Updates()
		update := <-c.Updates()
		if update.Phase != "Error" || update.Error != "prepare failed" {
			t.Fatalf("prepared job state was not published: %+v", update)
		}
		actions <- domain.Action{Kind: "confirm-quit"}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("protocol event is dispatched", func(t *testing.T) {
		c, agent, _ := testController(t)
		agent.events <- codex.Message{Method: "thread/renamed", Params: json.RawMessage(`{}`)}
		actions := make(chan domain.Action)
		done := make(chan error, 1)
		go func() { done <- c.Run(context.Background(), actions) }()
		<-c.Updates()
		var update domain.State
		for len(update.Events) == 0 {
			update = <-c.Updates()
		}
		if update.Events[len(update.Events)-1].Text != "thread/renamed" {
			t.Fatalf("protocol event not dispatched: %+v", update.Events)
		}
		actions <- domain.Action{Kind: "confirm-quit"}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ticker initiates polling", func(t *testing.T) {
		c, _, _ := testController(t)
		gh := &fakeGitHub{snapshot: c.state.Snapshot, called: make(chan struct{}, 1)}
		c.gh = gh
		c.nextPoll = time.Now().Add(-time.Second)
		actions := make(chan domain.Action)
		done := make(chan error, 1)
		go func() { done <- c.Run(context.Background(), actions) }()
		<-c.Updates()
		select {
		case <-gh.called:
		case <-time.After(2 * time.Second):
			t.Fatal("ticker did not poll GitHub")
		}
		actions <- domain.Action{Kind: "confirm-quit"}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("closed actions stop the loop", func(t *testing.T) {
		c := New(Config{})
		actions := make(chan domain.Action)
		close(actions)
		if err := c.Run(context.Background(), actions); err != nil {
			t.Fatal(err)
		}
		if c.state.Phase != "Ready" {
			t.Fatalf("closed action stream changed phase: %s", c.state.Phase)
		}
	})
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for controller job")
}
