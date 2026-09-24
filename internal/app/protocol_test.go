package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"quick-review-cli/internal/codex"
	"quick-review-cli/internal/domain"
	"quick-review-cli/internal/session"
)

type protocolClient struct {
	replies  []any
	rejects  []string
	replyErr error
	turns    int
	events   chan codex.Message
}

func (p *protocolClient) Initialize(context.Context) error { return nil }
func (p *protocolClient) Thread(context.Context, string, string, string) (string, error) {
	return "thread", nil
}
func (p *protocolClient) Turn(context.Context, string, string, string) (string, error) {
	p.turns++
	return "next-turn", nil
}
func (p *protocolClient) Steer(context.Context, string, string, string) error { return nil }
func (p *protocolClient) Interrupt(context.Context, string, string) error     { return nil }
func (p *protocolClient) Events() <-chan codex.Message                        { return p.events }
func (p *protocolClient) Reply(_ json.RawMessage, a any) error {
	p.replies = append(p.replies, a)
	return p.replyErr
}
func (p *protocolClient) Reject(_ json.RawMessage, s string) error {
	p.rejects = append(p.rejects, s)
	return nil
}
func (p *protocolClient) Close() {}

type protocolWorkspace struct{}

func (protocolWorkspace) Prepare(context.Context, domain.PR, domain.Snapshot) (string, string, string, error) {
	return "", "", "", nil
}
func (protocolWorkspace) Path() string { return "/tmp/test-checkout" }
func protocolController(t *testing.T) (*Controller, *protocolClient) {
	t.Helper()
	c := New(Config{Root: t.TempDir()})
	p := &protocolClient{}
	c.client = p
	c.workspace = protocolWorkspace{}
	c.state.ThreadID = "thread"
	c.activeTurn = "turn"
	c.state.Snapshot = domain.Snapshot{PR: domain.PR{Owner: "o", Repo: "r", Number: 1}, HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), State: "OPEN"}
	c.state.ReviewedHead = c.state.Snapshot.HeadSHA
	c.reviewBase = c.state.Snapshot.BaseSHA
	c.notify = func(domain.PR, string, string) {}
	st, e := session.NewStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c.store = st
	return c, p
}
func protocolMessage(method string, params any) codex.Message {
	b, _ := json.Marshal(params)
	return codex.Message{Method: method, Params: b}
}
func TestProtocolMessagesAndSubagents(t *testing.T) {
	c, _ := protocolController(t)
	ctx := context.Background()
	c.onProtocol(ctx, codex.Message{Method: "bad", Params: []byte("no")})
	c.onProtocol(ctx, protocolMessage("thread/started", map[string]any{"thread": map[string]string{"id": "child", "parentThreadId": "thread", "agentNickname": "Luna", "agentRole": "security"}}))
	c.onProtocol(ctx, protocolMessage("turn/started", map[string]any{"threadId": "child", "turn": map[string]string{"id": "child-turn"}}))
	if c.childTurns["child"] != "child-turn" {
		t.Fatal(c.childTurns)
	}
	c.onProtocol(ctx, protocolMessage("turn/started", map[string]any{"threadId": "thread", "turn": map[string]string{"id": "turn"}}))
	c.onProtocol(ctx, protocolMessage("item/agentMessage/delta", map[string]string{"threadId": "thread", "delta": "hello"}))
	c.onProtocol(ctx, protocolMessage("item/agentMessage/delta", map[string]string{"threadId": "child", "delta": "private"}))
	if c.state.DraftReply != "hello" {
		t.Fatal(c.state.DraftReply)
	}
	for _, x := range []struct {
		kind, phase, thread string
		complete            bool
	}{
		{"agentMessage", "commentary", "thread", false}, {"agentMessage", "commentary", "thread", true}, {"agentMessage", "final_answer", "thread", true}, {"agentMessage", "", "child", true},
		{"commandExecution", "", "thread", false}, {"commandExecution", "", "child", true}, {"plan", "", "thread", true}, {"plan", "", "thread", false},
	} {
		method := "item/started"
		if x.complete {
			method = "item/completed"
		}
		c.onProtocol(ctx, protocolMessage(method, map[string]any{"threadId": x.thread, "item": map[string]string{"type": x.kind, "phase": x.phase, "text": "finding", "command": "git diff", "aggregatedOutput": "diff", "status": "completed"}}))
	}
	c.onProtocol(ctx, protocolMessage("item/completed", map[string]any{"threadId": "thread", "item": map[string]any{"type": "collabAgentToolCall", "tool": "spawnAgent", "status": "completed", "prompt": "security", "receiverThreadIds": []string{"child", "child2"}, "agentsStates": map[string]any{"child": map[string]string{"status": "running", "message": "checking"}}}}))
	c.onProtocol(ctx, protocolMessage("item/started", map[string]any{"item": map[string]any{"type": "collabAgentToolCall", "tool": "wait", "status": "running", "receiverThreadIds": []string{}}}))
	c.onProtocol(ctx, protocolMessage("item/completed", map[string]any{"item": map[string]any{"type": "subAgentActivity", "agentThreadId": "child", "agentPath": "security", "kind": "completed"}}))
	c.onProtocol(ctx, protocolMessage("turn/completed", map[string]any{"threadId": "child", "turn": map[string]string{"id": "child-turn", "status": "completed"}}))
	if len(c.childTurns) != 0 {
		t.Fatal(c.childTurns)
	}
	c.onProtocol(ctx, protocolMessage("error", map[string]string{"message": "retry"}))
	c.onProtocol(ctx, protocolMessage("thread/tokenUsage/updated", map[string]int{"tokens": 4}))
	n := len(c.state.Events)
	c.onProtocol(ctx, protocolMessage("item/commandExecution/outputDelta", map[string]string{"delta": "raw"}))
	// Unknown events remain visible rather than crashing a newer server.
	if len(c.state.Events) < n {
		t.Fatal("lost event")
	}
	c.onProtocol(ctx, protocolMessage("codex/event/old", map[string]string{}))
	c.onProtocol(ctx, protocolMessage("text/delta", map[string]string{}))
	c.upsertAgent(domain.Agent{ID: "anonymous", Status: "running"})
	if len(c.state.Agents) < 3 {
		t.Fatal(c.state.Agents)
	}
}
func TestCompletedReviewSavesOnlyItsRevision(t *testing.T) {
	c, _ := protocolController(t)
	c.reviewTurn = true
	c.finalText = "No actionable findings"
	c.refreshPending = true
	c.state.Paused = true
	c.onProtocol(context.Background(), protocolMessage("turn/completed", map[string]any{"threadId": "thread", "turn": map[string]string{"id": "wrong", "status": "completed"}}))
	if c.activeTurn != "turn" {
		t.Fatal("wrong turn consumed")
	}
	c.onProtocol(context.Background(), protocolMessage("turn/completed", map[string]any{"threadId": "thread", "turn": map[string]string{"id": "turn", "status": "completed"}}))
	if len(c.state.Reports) != 1 || !c.state.Reports[0].Stale || !strings.Contains(c.state.Reports[0].Text, c.state.ReviewedHead) {
		t.Fatal(c.state.Reports)
	}
	if c.activeTurn != "" {
		t.Fatal(c.activeTurn)
	}
}
func TestCompletionFailures(t *testing.T) {
	for _, status := range []string{"failed", "interrupted", "completed"} {
		c, _ := protocolController(t)
		c.reviewTurn = true
		c.finalText = ""
		c.onProtocol(context.Background(), protocolMessage("turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": status, "error": map[string]string{"message": "failure"}}}))
		if len(c.state.Reports) != 0 {
			t.Fatal("invented report")
		}
	}
	c, _ := protocolController(t)
	c.reviewTurn = true
	c.finalText = "report"
	c.reviewBase = "invalid"
	c.onProtocol(context.Background(), protocolMessage("turn/completed", map[string]any{"turn": map[string]string{"id": "turn", "status": "completed"}}))
	if c.state.Error == "" {
		t.Fatal("save failure hidden")
	}
}
func TestApprovalAndPermissions(t *testing.T) {
	for _, method := range []string{"item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval"} {
		for _, choice := range []string{"Decline", "Approve once"} {
			t.Run(method+choice, func(t *testing.T) {
				c, p := protocolController(t)
				m := protocolMessage(method, map[string]any{"command": "git diff", "reason": "inspect", "permissions": map[string]any{"network": map[string]bool{"enabled": true}}})
				m.ID = json.RawMessage(`12`)
				c.onProtocol(context.Background(), m)
				if len(c.state.Questions) != 1 {
					t.Fatal(c.state.Questions)
				}
				c.answer(domain.Action{ID: "12", Text: choice})
				if len(p.replies) != 1 || len(c.state.Questions) != 0 {
					t.Fatal(p.replies, c.state.Questions)
				}
			})
		}
	}
	c, p := protocolController(t)
	p.replyErr = errors.New("offline")
	m := protocolMessage("item/fileChange/requestApproval", map[string]string{})
	m.ID = json.RawMessage(`"abc"`)
	c.onProtocol(context.Background(), m)
	c.answer(domain.Action{ID: `"abc"`, Text: "Decline"})
	if c.state.Error == "" || len(c.state.Questions) == 0 {
		t.Fatal("lost retryable approval")
	}
	c.answer(domain.Action{ID: "missing"})
	c.client = nil
	c.answer(domain.Action{ID: `"abc"`})
}
func TestUserQuestionsAndUnsupportedRequests(t *testing.T) {
	c, p := protocolController(t)
	m := protocolMessage("item/tool/requestUserInput", map[string]any{"questions": []any{map[string]any{"id": "first", "header": "First", "question": "Which?", "options": []any{map[string]string{"label": "A"}}}, map[string]string{"id": "second", "header": "Second", "question": "Why?"}}})
	m.ID = json.RawMessage(`7`)
	c.onProtocol(context.Background(), m)
	if len(c.state.Questions) != 2 {
		t.Fatal(c.state.Questions)
	}
	c.answer(domain.Action{ID: "7/first", Text: "A"})
	if len(p.replies) != 0 {
		t.Fatal("premature response")
	}
	c.answer(domain.Action{ID: "7/second", Text: "because"})
	if len(p.replies) != 1 || len(c.state.Questions) != 0 {
		t.Fatal(p.replies)
	}
	secret := protocolMessage("item/tool/requestUserInput", map[string]any{"questions": []any{map[string]any{"id": "secret", "isSecret": true}}})
	secret.ID = json.RawMessage(`8`)
	c.onProtocol(context.Background(), secret)
	empty := protocolMessage("item/tool/requestUserInput", map[string]any{"questions": []any{}})
	empty.ID = json.RawMessage(`9`)
	c.onProtocol(context.Background(), empty)
	unknown := protocolMessage("new/future/request", map[string]any{})
	unknown.ID = json.RawMessage(`10`)
	c.onProtocol(context.Background(), unknown)
	if len(p.rejects) != 2 || len(p.replies) != 2 {
		t.Fatal(p.rejects, p.replies)
	}
}

func TestQuestionReplyCanBeRetriedWithoutLosingAnswers(t *testing.T) {
	c, p := protocolController(t)
	m := protocolMessage("item/tool/requestUserInput", map[string]any{"questions": []any{map[string]string{"id": "one", "header": "One", "question": "First?"}, map[string]string{"id": "two", "header": "Two", "question": "Second?"}}})
	m.ID = json.RawMessage(`17`)
	c.onProtocol(context.Background(), m)
	c.answer(domain.Action{ID: "17/one", Text: "first"})
	p.replyErr = errors.New("connection temporarily unavailable")
	c.answer(domain.Action{ID: "17/two", Text: "second"})
	if len(c.state.Questions) != 1 {
		t.Fatal("failed reply lost question")
	}
	p.replyErr = nil
	c.answer(domain.Action{ID: "17/two", Text: "second"})
	if len(p.replies) != 2 || len(c.state.Questions) != 0 {
		t.Fatal("retry did not finish", p.replies, c.state.Questions)
	}
	encoded, _ := json.Marshal(p.replies[1])
	if !strings.Contains(string(encoded), "first") || !strings.Contains(string(encoded), "second") {
		t.Fatal(string(encoded))
	}
}
