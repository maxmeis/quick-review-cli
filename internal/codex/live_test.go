package codex

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// This opt-in contract test uses the logged-in CLI. It makes no GitHub changes.
func TestLiveCodexContract(t *testing.T) {
	if os.Getenv("QUICK_REVIEW_LIVE_CODEX") != "1" {
		t.Skip("set QUICK_REVIEW_LIVE_CODEX=1 for the authenticated CLI contract test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := c.Thread(ctx, cwd, "", "This is a connection test. Do not use tools or subagents. Reply with exactly QUICK_REVIEW_OK.")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := c.Turn(ctx, thread, cwd, "Reply with exactly QUICK_REVIEW_OK. Do not use any tools.")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for {
		select {
		case <-ctx.Done():
			t.Fatal("live turn timed out")
		case m, ok := <-c.Events():
			if !ok {
				t.Fatal("connection closed", c.Err())
			}
			var p struct {
				ThreadID string                      `json:"threadId"`
				Item     struct{ Type, Text string } `json:"item"`
				Turn     struct{ ID, Status string } `json:"turn"`
			}
			_ = json.Unmarshal(m.Params, &p)
			if m.Method == "item/completed" && p.Item.Type == "agentMessage" {
				t.Logf("Assistant response: %q", p.Item.Text)
			}
			if m.Method == "item/completed" && p.Item.Type == "agentMessage" && strings.Trim(p.Item.Text, " \r\n\t.!") == "QUICK_REVIEW_OK" {
				found = true
			}
			if m.Method == "turn/completed" && p.Turn.ID == turn {
				if !found || p.Turn.Status != "completed" {
					t.Fatalf("unexpected result: found=%v status=%s", found, p.Turn.Status)
				}
				return
			}
		}
	}
}
