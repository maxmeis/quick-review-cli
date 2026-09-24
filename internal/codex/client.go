// Package codex implements the local Codex app-server JSONL protocol.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("Codex: %s (%d)", e.Message, e.Code) }

type Client struct {
	writer   io.Writer
	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]chan Message
	seq      atomic.Uint64
	incoming chan Message
	events   chan Message
	done     chan struct{}
	once     sync.Once
	closeFn  func()
	err      error
}

// New connects to a JSONL stream. Responses bypass the notification queue so
// a busy event consumer cannot deadlock an outstanding RPC call.
func New(r io.Reader, w io.Writer, closeFn func()) *Client {
	c := &Client{writer: w, pending: make(map[string]chan Message), incoming: make(chan Message), events: make(chan Message), done: make(chan struct{}), closeFn: closeFn}
	go c.deliver()
	go c.read(r)
	return c
}
func Start(ctx context.Context) (*Client, error) {
	cmd := exec.CommandContext(ctx, "codex", "app-server", "--listen", "stdio://")
	configureProcess(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// Never mix human diagnostic output into the protocol stream.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Codex: %w", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	return New(out, in, func() {
		_ = in.Close()
		select {
		case <-exited:
		case <-time.After(500 * time.Millisecond):
			stopProcess(cmd)
			<-exited
		}
	}), nil
}
func (c *Client) Events() <-chan Message { return c.events }
func (c *Client) Done() <-chan struct{}  { return c.done }
func (c *Client) Err() error             { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *Client) finish(err error) {
	c.once.Do(func() { c.mu.Lock(); c.err = err; c.mu.Unlock(); close(c.done) })
}
func (c *Client) Close() {
	c.finish(io.EOF)
	if c.closeFn != nil {
		c.closeFn()
	}
}
func (c *Client) read(r io.Reader) {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 65536), 16*1024*1024)
	for scan.Scan() {
		var m Message
		if err := json.Unmarshal(scan.Bytes(), &m); err != nil {
			c.finish(fmt.Errorf("invalid Codex protocol event: %w", err))
			return
		}
		if m.Method == "" && len(m.ID) > 0 {
			c.mu.Lock()
			ch := c.pending[string(m.ID)]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- m:
				default:
				}
			}
		} else {
			select {
			case c.incoming <- m:
			case <-c.done:
				return
			}
		}
	}
	err := scan.Err()
	if err == nil {
		err = io.EOF
	}
	c.finish(err)
}
func (c *Client) deliver() {
	defer close(c.events)
	queue := []Message{}
	for {
		var out chan Message
		var next Message
		if len(queue) > 0 {
			out = c.events
			next = queue[0]
		}
		select {
		case m := <-c.incoming:
			queue = append(queue, m)
		case out <- next:
			queue[0] = Message{}
			queue = queue[1:]
		case <-c.done:
			return
		}
	}
}
func (c *Client) write(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return json.NewEncoder(c.writer).Encode(v)
}
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	id := fmt.Sprint(c.seq.Add(1))
	ch := make(chan Message, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.write(map[string]any{"id": json.RawMessage(id), "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result != nil {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("Codex disconnected: %w", c.Err())
	}
}
func (c *Client) Notify(method string, params any) error {
	return c.write(map[string]any{"method": method, "params": params})
}
func (c *Client) Reply(id json.RawMessage, result any) error {
	return c.write(map[string]any{"id": id, "result": result})
}
func (c *Client) Reject(id json.RawMessage, reason string) error {
	return c.write(map[string]any{"id": id, "error": RPCError{-32601, reason}})
}
func (c *Client) Initialize(ctx context.Context) error {
	if err := c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "quick_review", "title": "Quick Review", "version": "0.1.0"}, "capabilities": map[string]any{"experimentalApi": true}}, nil); err != nil {
		return err
	}
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		return err
	}
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if err := c.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &account); err != nil {
		return err
	}
	if len(account.Account) == 0 || string(account.Account) == "null" {
		return errors.New("Codex is not logged in; run codex login, then retry")
	}
	return nil
}
func (c *Client) Thread(ctx context.Context, cwd, existing, instructions string) (string, error) {
	params := map[string]any{"cwd": cwd, "approvalPolicy": "on-request", "sandbox": "read-only", "developerInstructions": instructions}
	method := "thread/start"
	if existing != "" {
		method = "thread/resume"
		params["threadId"] = existing
		params["excludeTurns"] = true
	}
	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := c.Call(ctx, method, params, &result); err != nil {
		return "", err
	}
	if result.Thread.ID == "" {
		return "", errors.New("Codex returned no thread ID")
	}
	return result.Thread.ID, nil
}
func (c *Client) Turn(ctx context.Context, thread, cwd, text string) (string, error) {
	var result struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	err := c.Call(ctx, "turn/start", map[string]any{"threadId": thread, "cwd": cwd, "input": []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}}}, &result)
	if err == nil && result.Turn.ID == "" {
		err = errors.New("Codex returned no turn ID")
	}
	return result.Turn.ID, err
}
func (c *Client) Steer(ctx context.Context, thread, turn, text string) error {
	return c.Call(ctx, "turn/steer", map[string]any{"threadId": thread, "expectedTurnId": turn, "input": []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}}}, nil)
}
func (c *Client) Interrupt(ctx context.Context, thread, turn string) error {
	return c.Call(ctx, "turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, nil)
}
