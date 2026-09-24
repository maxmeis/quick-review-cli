package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func server(t *testing.T, handler func(Message) any) (*Client, func()) {
	t.Helper()
	a, b := net.Pipe()
	c := New(a, a, func() { a.Close() })
	go func() {
		s := bufio.NewScanner(b)
		for s.Scan() {
			var m Message
			_ = json.Unmarshal(s.Bytes(), &m)
			if len(m.ID) > 0 {
				result := handler(m)
				var v any = map[string]any{"id": m.ID, "result": result}
				if e, ok := result.(*RPCError); ok {
					v = map[string]any{"id": m.ID, "error": e}
				}
				_ = json.NewEncoder(b).Encode(v)
			}
		}
	}()
	return c, func() { c.Close(); b.Close() }
}
func TestProtocolLifecycle(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	c, close := server(t, func(m Message) any {
		mu.Lock()
		defer mu.Unlock()
		methods = append(methods, m.Method)
		switch m.Method {
		case "account/read":
			return map[string]any{"account": map[string]string{"type": "chatgpt"}}
		case "thread/start", "thread/resume":
			return map[string]any{"thread": map[string]string{"id": "thread"}}
		case "turn/start":
			return map[string]any{"turn": map[string]string{"id": "turn"}}
		}
		return map[string]any{}
	})
	defer close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	for _, existing := range []string{"", "thread"} {
		id, err := c.Thread(ctx, "/tmp", existing, "playbook")
		if err != nil || id != "thread" {
			t.Fatalf("%s %v", id, err)
		}
	}
	if id, err := c.Turn(ctx, "thread", "/tmp", "hello"); err != nil || id != "turn" {
		t.Fatalf("%s %v", id, err)
	}
	if err := c.Steer(ctx, "thread", "turn", "update"); err != nil {
		t.Fatal(err)
	}
	if err := c.Interrupt(ctx, "thread", "turn"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 7 {
		t.Fatal(methods)
	}
}
func TestCallsFailCleanly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result any
		call   func(*Client) error
	}{
		{"rpc error", &RPCError{-1, "oops"}, func(c *Client) error { return c.Call(context.Background(), "x", nil, nil) }},
		{"invalid result", "bad", func(c *Client) error { var v int; return c.Call(context.Background(), "x", nil, &v) }},
		{"no account", map[string]any{"account": nil}, func(c *Client) error { return c.Initialize(context.Background()) }},
		{"no thread", map[string]any{}, func(c *Client) error { _, e := c.Thread(context.Background(), "", "", ""); return e }},
		{"no turn", map[string]any{}, func(c *Client) error { _, e := c.Turn(context.Background(), "", "", ""); return e }},
		{"thread rpc", &RPCError{-1, "oops"}, func(c *Client) error { _, e := c.Thread(context.Background(), "", "", ""); return e }},
		{"initialize rpc", &RPCError{-1, "oops"}, func(c *Client) error { return c.Initialize(context.Background()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, close := server(t, func(Message) any { return tc.result })
			defer close()
			if e := tc.call(c); e == nil {
				t.Fatal("wanted error")
			}
		})
	}
}

func TestRPCErrorString(t *testing.T) {
	if got := (&RPCError{Code: -32601, Message: "unsupported"}).Error(); got != "Codex: unsupported (-32601)" {
		t.Fatalf("RPCError.Error() = %q", got)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func TestWriteFailureAndCancellation(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	c := New(r, failWriter{}, func() { r.Close() })
	defer c.Close()
	if e := c.Call(context.Background(), "x", nil, nil); e == nil {
		t.Fatal("wanted write error")
	}
	if e := c.Notify("x", nil); e == nil {
		t.Fatal("notify error")
	}
	if e := c.Reply(json.RawMessage(`"s"`), nil); e == nil {
		t.Fatal("reply error")
	}
	if e := c.Reject(json.RawMessage(`1`), "unsupported"); e == nil {
		t.Fatal("reject error")
	}
	r2, w2 := io.Pipe()
	defer w2.Close()
	c2 := New(r2, io.Discard, func() { r2.Close() })
	defer c2.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := c2.Call(ctx, "x", nil, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	c2.Close()
	if e := c2.Call(context.Background(), "x", nil, nil); e == nil {
		t.Fatal("wanted disconnected")
	}
}
func TestMalformedAndEOF(t *testing.T) {
	for _, input := range []string{"not json\n", ""} {
		c := New(strings.NewReader(input), io.Discard, nil)
		select {
		case <-c.Done():
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
		if c.Err() == nil {
			t.Fatal("expected termination")
		}
		c.Close()
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadErrorAndDroppedResponse(t *testing.T) {
	readErr := errors.New("read failed")
	c := &Client{done: make(chan struct{}), pending: map[string]chan Message{}, once: sync.Once{}}
	c.read(failingReader{readErr})
	if !errors.Is(c.Err(), readErr) {
		t.Fatalf("read error = %v", c.Err())
	}

	responses := `{"id":1,"result":{}}` + "\n" + `{"id":1,"result":{}}` + "\n"
	responseClient := &Client{done: make(chan struct{}), pending: map[string]chan Message{"1": make(chan Message, 1)}, once: sync.Once{}}
	responseClient.read(strings.NewReader(responses))
	if len(responseClient.pending["1"]) != 1 {
		t.Fatal("buffered response was lost or duplicate response blocked the reader")
	}

	done := make(chan struct{})
	close(done)
	closedClient := &Client{done: done, incoming: make(chan Message), pending: map[string]chan Message{}, once: sync.Once{}}
	closedClient.read(strings.NewReader(`{"method":"event"}` + "\n"))
	select {
	case <-closedClient.done:
	default:
		t.Fatal("read did not return when client was already done")
	}
}
func TestNotificationsAndServerRequests(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := New(a, a, func() { a.Close() })
	defer c.Close()
	go func() {
		fmt.Fprintln(b, `{"method":"turn/started","params":{"id":"t"}}`)
		fmt.Fprintln(b, `{"id":"request","method":"approval","params":{}}`)
	}()
	for _, method := range []string{"turn/started", "approval"} {
		select {
		case m := <-c.Events():
			if m.Method != method {
				t.Fatal(m)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
	reply := make(chan Message, 1)
	go func() { var m Message; _ = json.NewDecoder(b).Decode(&m); reply <- m }()
	if e := c.Reply(json.RawMessage(`"request"`), map[string]string{"decision": "decline"}); e != nil {
		t.Fatal(e)
	}
	if m := <-reply; string(m.ID) != `"request"` {
		t.Fatal(m)
	}
}
func TestNotificationBacklogDoesNotBlockRPC(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := New(a, a, func() { a.Close() })
	defer c.Close()
	go func() {
		for i := 0; i < 2000; i++ {
			fmt.Fprintln(b, `{"method":"event","params":{}}`)
		}
		var m Message
		_ = json.NewDecoder(b).Decode(&m)
		fmt.Fprintf(b, "{\"id\":%s,\"result\":{}}\n", m.ID)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if e := c.Call(ctx, "x", nil, nil); e != nil {
		t.Fatal(e)
	}
}
func TestStartMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, e := Start(context.Background()); e == nil {
		t.Fatal("wanted executable error")
	}
}

func TestStartCommandPipeErrors(t *testing.T) {
	stdout := exec.Command("unused")
	stdout.Stdout = io.Discard
	if _, err := startCommand(stdout); err == nil {
		t.Fatal("startCommand accepted preconfigured stdout")
	}
	stdin := exec.Command("unused")
	stdin.Stdin = strings.NewReader("")
	if _, err := startCommand(stdin); err == nil {
		t.Fatal("startCommand accepted preconfigured stdin")
	}
}
func TestStartLocalProcess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	if e := os.WriteFile(script, []byte("#!/bin/sh\nwhile read line; do :; done\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir)
	c, e := Start(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
}

func TestStartForceStopsProcessThatIgnoresStdin(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLongLivedCodexChild$")
	cmd.Env = append(os.Environ(), "QUICK_REVIEW_CODEX_CHILD=1")
	configureProcess(cmd)
	c, err := startCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	c.Close()
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Close took %s; process was not force-stopped", elapsed)
	}
}

func TestLongLivedCodexChild(t *testing.T) {
	if os.Getenv("QUICK_REVIEW_CODEX_CHILD") != "1" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}

type scriptedResponseWriter struct {
	writer *io.PipeWriter
	writes int
	failAt int
}

func (w *scriptedResponseWriter) Write(p []byte) (int, error) {
	w.writes++
	switch w.writes {
	case 1:
		_, err := io.WriteString(w.writer, `{"id":1,"result":{}}`+"\n")
		return len(p), err
	case 2:
		if w.failAt == 2 {
			return 0, errors.New("second write failed")
		}
	case 3:
		_, err := io.WriteString(w.writer, `{"id":2,"error":{"code":-1,"message":"account read failed"}}`+"\n")
		return len(p), err
	}
	return len(p), nil
}

func TestInitializeNotifyAndAccountErrors(t *testing.T) {
	initReader, initWriter := io.Pipe()
	initialized := New(initReader, &scriptedResponseWriter{writer: initWriter, failAt: 2}, func() { _ = initReader.Close(); _ = initWriter.Close() })
	defer initialized.Close()
	if err := initialized.Initialize(context.Background()); err == nil || !strings.Contains(err.Error(), "second write failed") {
		t.Fatalf("Initialize notification error = %v", err)
	}

	accountReader, accountWriter := io.Pipe()
	accountError := New(accountReader, &scriptedResponseWriter{writer: accountWriter}, func() { _ = accountReader.Close(); _ = accountWriter.Close() })
	defer accountError.Close()
	if err := accountError.Initialize(context.Background()); err == nil || !strings.Contains(err.Error(), "account read failed") {
		t.Fatalf("Initialize account error = %v", err)
	}
}
