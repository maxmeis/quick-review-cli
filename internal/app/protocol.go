package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"quick-review-cli/internal/codex"
	"quick-review-cli/internal/domain"
)

type wireParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
	Turn     struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"turn"`
	Thread struct {
		ID     string `json:"id"`
		Parent string `json:"parentThreadId"`
		Name   string `json:"agentNickname"`
		Role   string `json:"agentRole"`
	} `json:"thread"`
	Item        wireItem        `json:"item"`
	Command     string          `json:"command"`
	Reason      string          `json:"reason"`
	Permissions json.RawMessage `json:"permissions"`
	Questions   []struct {
		ID       string `json:"id"`
		Header   string `json:"header"`
		Question string `json:"question"`
		IsSecret bool   `json:"isSecret"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
}
type wireItem struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Text      string   `json:"text"`
	Phase     string   `json:"phase"`
	Command   string   `json:"command"`
	Output    string   `json:"aggregatedOutput"`
	Status    string   `json:"status"`
	Tool      string   `json:"tool"`
	Prompt    string   `json:"prompt"`
	Receivers []string `json:"receiverThreadIds"`
	Agents    map[string]struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"agentsStates"`
	AgentThread string `json:"agentThreadId"`
	AgentPath   string `json:"agentPath"`
	Kind        string `json:"kind"`
}

func (c *Controller) onProtocol(ctx context.Context, m codex.Message) {
	var p wireParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		c.event("Codex", "protocol", "Unrecognized event", string(m.Params), "")
		return
	}
	if len(m.ID) > 0 {
		c.onRequest(m, p)
		return
	}
	isChild := p.ThreadID != "" && p.ThreadID != c.state.ThreadID
	switch m.Method {
	case "thread/started":
		if p.Thread.Parent != "" {
			c.upsertAgent(domain.Agent{ID: p.Thread.ID, Name: p.Thread.Name, Scope: p.Thread.Role, Status: "started"})
			c.event("Agents", "started", "Reviewer connected: "+p.Thread.Name, p.Thread.Role, c.state.ReviewedHead)
		}
	case "turn/started":
		if isChild {
			c.childTurns[p.ThreadID] = p.Turn.ID
		} else {
			c.activeTurn = p.Turn.ID
		}
	case "item/agentMessage/delta":
		if !isChild {
			c.state.DraftReply += p.Delta
		}
	case "item/started", "item/completed":
		c.onItem(p, m.Method == "item/completed", isChild)
	case "turn/completed":
		if isChild {
			delete(c.childTurns, p.ThreadID)
			c.upsertAgent(domain.Agent{ID: p.ThreadID, Status: p.Turn.Status})
			return
		}
		if p.Turn.ID != c.activeTurn {
			return
		}
		c.activeTurn = ""
		c.state.DraftReply = ""
		c.state.Questions = nil
		c.requests = map[string]*request{}
		if p.Turn.Status == "completed" {
			if c.reviewTurn && c.finalText != "" && c.store != nil {
				text := fmt.Sprintf("# Review: %s #%d\n\nHead: `%s`  \nMerge-base: `%s`\n\n%s\n", c.state.Snapshot.PR.Owner+"/"+c.state.Snapshot.PR.Repo, c.state.Snapshot.PR.Number, c.state.ReviewedHead, c.reviewBase, c.finalText)
				r, err := c.store.SaveReport(c.state.ReviewedHead, c.reviewBase, text)
				if err != nil {
					c.failure(err)
				} else {
					r.Stale = c.refreshPending
					c.state.Reports = append(c.state.Reports, r)
					c.event("App", "report", "Markdown report saved", r.Path, r.HeadSHA)
					go c.notify(c.state.Snapshot.PR, "✓ Review complete", short(r.HeadSHA))
				}
			}
		} else {
			message := "Review turn " + p.Turn.Status
			if p.Turn.Error != nil {
				message += ": " + p.Turn.Error.Message
			}
			c.event("Codex", "error", message, "", c.state.ReviewedHead)
		}
		c.reviewTurn = false
		c.finalText = ""
		c.drain(ctx)
	case "error":
		c.event("Codex", "error", "Codex reported an error", string(m.Params), c.state.ReviewedHead)
	default:
		// Keep useful protocol events in Activity; streaming deltas are represented by
		// completed items to avoid thousands of redundant persisted entries.
		if !strings.HasSuffix(m.Method, "/delta") && !strings.HasSuffix(m.Method, "Delta") && !strings.HasPrefix(m.Method, "codex/event/") {
			c.event("Codex", "activity", m.Method, string(m.Params), c.state.ReviewedHead)
		}
	}
}
func (c *Controller) onItem(p wireParams, complete, child bool) {
	it := p.Item
	switch it.Type {
	case "agentMessage":
		if complete {
			source := "Codex"
			if child {
				source = "Agents"
			}
			c.event(source, "message", it.Text, "", c.state.ReviewedHead)
			if !child {
				c.state.DraftReply = ""
				if it.Phase == "final_answer" || it.Phase == "" {
					c.finalText = it.Text
				}
			}
		}
	case "collabAgentToolCall":
		for _, id := range it.Receivers {
			a := domain.Agent{ID: id, Name: short(id), Scope: it.Prompt, Status: it.Status}
			if st, ok := it.Agents[id]; ok {
				a.Status = st.Status
				a.Detail = st.Message
			}
			c.upsertAgent(a)
		}
		suffix := "started"
		if complete {
			suffix = "completed"
		}
		c.event("Agents", suffix, it.Tool+" "+suffix, it.Prompt, c.state.ReviewedHead)
	case "subAgentActivity":
		c.upsertAgent(domain.Agent{ID: it.AgentThread, Name: it.AgentPath, Status: it.Kind})
		c.event("Agents", it.Kind, it.AgentPath+" "+it.Kind, "", c.state.ReviewedHead)
	case "commandExecution":
		phase := "started"
		if complete {
			phase = "completed"
		}
		source := "Codex"
		if child {
			source = "Agents"
		}
		c.event(source, "tool", it.Command+" · "+phase, it.Output, c.state.ReviewedHead)
	default:
		if complete {
			detail, _ := json.Marshal(it)
			c.event("Codex", "tool", it.Type+" "+it.Status, string(detail), c.state.ReviewedHead)
		}
	}
}
func (c *Controller) upsertAgent(a domain.Agent) {
	for i, old := range c.state.Agents {
		if old.ID == a.ID {
			if a.Name == "" {
				a.Name = old.Name
			}
			if a.Scope == "" {
				a.Scope = old.Scope
			}
			if a.Detail == "" {
				a.Detail = old.Detail
			}
			c.state.Agents[i] = a
			return
		}
	}
	if a.Name == "" {
		a.Name = short(a.ID)
	}
	c.state.Agents = append(c.state.Agents, a)
}
func (c *Controller) onRequest(m codex.Message, p wireParams) {
	key := string(m.ID)
	r := &request{id: m.ID, method: m.Method, answers: map[string]any{}, permissions: p.Permissions}
	switch m.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval":
		r.remaining = 1
		c.requests[key] = r
		c.state.Questions = append(c.state.Questions, domain.Question{ID: key, Title: "Codex requests approval", Prompt: p.Command + "\n" + p.Reason + "\n" + string(p.Permissions), Options: []string{"Decline", "Approve once"}})
	case "item/tool/requestUserInput":
		for _, q := range p.Questions {
			if q.IsSecret {
				_ = c.client.Reject(m.ID, "Secret input is not supported in this interface")
				return
			}
		}
		r.remaining = len(p.Questions)
		if r.remaining == 0 {
			_ = c.client.Reply(m.ID, map[string]any{"answers": map[string]any{}})
			return
		}
		c.requests[key] = r
		for _, q := range p.Questions {
			opts := []string{}
			for _, o := range q.Options {
				opts = append(opts, o.Label)
			}
			id := key + "/" + q.ID
			c.requests[id] = r
			c.state.Questions = append(c.state.Questions, domain.Question{ID: id, Title: q.Header, Prompt: q.Question, Options: opts})
		}
	default:
		_ = c.client.Reject(m.ID, "This request type is not supported by quick-review")
		c.event("App", "request", "Unsupported Codex request: "+m.Method, string(m.Params), c.state.ReviewedHead)
		return
	}
	c.event("Codex", "question", "Codex is waiting for your response", p.Reason, c.state.ReviewedHead)
}
func (c *Controller) answer(a domain.Action) {
	r := c.requests[a.ID]
	if r == nil || c.client == nil {
		return
	}
	var response any
	switch r.method {
	case "item/tool/requestUserInput":
		key := strings.TrimPrefix(a.ID, string(r.id)+"/")
		if _, answered := r.answers[key]; !answered {
			r.remaining--
		}
		r.answers[key] = map[string]any{"answers": []string{a.Text}}
		if r.remaining == 0 {
			response = map[string]any{"answers": r.answers}
		}
	case "item/permissions/requestApproval":
		permissions := json.RawMessage(`{}`)
		if a.Text == "Approve once" && len(r.permissions) > 0 {
			permissions = r.permissions
		}
		response = map[string]any{"permissions": permissions, "scope": "turn"}
	default:
		decision := "decline"
		if a.Text == "Approve once" {
			decision = "accept"
		}
		response = map[string]any{"decision": decision}
	}
	if response != nil {
		if err := c.client.Reply(r.id, response); err != nil {
			c.failure(err)
			return
		}
	}
	delete(c.requests, a.ID)
	if r.remaining <= 0 || r.method != "item/tool/requestUserInput" {
		delete(c.requests, string(r.id))
	}
	for i, q := range c.state.Questions {
		if q.ID == a.ID {
			c.state.Questions = append(c.state.Questions[:i], c.state.Questions[i+1:]...)
			break
		}
	}
	c.event("You", "answer", a.Text, "", c.state.ReviewedHead)
}
