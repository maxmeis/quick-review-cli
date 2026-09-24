package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"quick-review-cli/internal/domain"
)

func TestArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0}, {[]string{"--unknown"}, 2}, {[]string{"--poll", "1s"}, 2}, {[]string{"a", "b"}, 2}, {[]string{"--resume", "session", "https://github.com/o/r/pull/1"}, 2},
	} {
		var out, err bytes.Buffer
		if code := run(tc.args, &out, &err); code != tc.code {
			t.Fatalf("%v: %d", tc.args, code)
		}
	}
}
func TestMain(t *testing.T) {
	oldExit, oldArgs := exit, os.Args
	defer func() { exit = oldExit; os.Args = oldArgs }()
	code := -1
	exit = func(c int) { code = c }
	os.Args = []string{"review", "--help"}
	main()
	if code != 0 {
		t.Fatal(code)
	}
}
func TestRunLauncherAndDemo(t *testing.T) {
	old := runUI
	defer func() { runUI = old }()
	for _, demo := range []bool{false, true} {
		runUI = func(ctx context.Context, states <-chan domain.State, dispatch func(domain.Action)) error {
			s := <-states
			if demo && s.Snapshot.PR.Number == 0 {
				t.Fatal("no demo")
			}
			dispatch(domain.Action{Kind: "confirm-quit"})
			for range states {
			}
			return nil
		}
		args := []string{"--data-dir", t.TempDir()}
		if demo {
			args = append(args, "--demo")
		}
		var out bytes.Buffer
		if code := run(args, &out, &out); code != 0 {
			t.Fatal(code, out.String())
		}
	}
}
func TestRunErrors(t *testing.T) {
	oldUI, oldRoot := runUI, defaultRoot
	defer func() { runUI = oldUI; defaultRoot = oldRoot }()
	var out bytes.Buffer
	defaultRoot = func() (string, error) { return "", errors.New("no config directory") }
	if code := run(nil, &out, &out); code != 1 {
		t.Fatal(code)
	}
	defaultRoot = func() (string, error) { return t.TempDir(), nil }
	runUI = func(context.Context, <-chan domain.State, func(domain.Action)) error {
		return errors.New("no terminal")
	}
	if code := run(nil, &out, &out); code != 1 {
		t.Fatal(code)
	}
	runUI = func(_ context.Context, updates <-chan domain.State, _ func(domain.Action)) error {
		for range updates {
		}
		return nil
	}
	if code := run([]string{"--resume", t.TempDir()}, &out, &out); code != 1 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "no terminal") {
		t.Fatal(out.String())
	}
}
func TestDemoEventsAndActions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan domain.State, 1)
	actions := make(chan domain.Action)
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- demoLoop(ctx, updates, actions, ticks) }()
	initial := <-updates
	if initial.Snapshot.HeadSHA == "" {
		t.Fatal("missing revision")
	}
	for _, a := range []domain.Action{{Kind: "quit"}, {Kind: "cancel-quit"}, {Kind: "message", Text: "hello"}, {Kind: "refresh"}, {Kind: "open", Text: "https://example.com"}, {Kind: "pause"}} {
		actions <- a
		s := <-updates
		if a.Kind == "message" && len(s.Events) <= len(initial.Events) {
			t.Fatal("no message")
		}
	}
	// Paused ticks produce no events.
	ticks <- time.Now()
	actions <- domain.Action{Kind: "resume"}
	<-updates
	for i := 0; i < 4; i++ {
		ticks <- time.Now()
		s := <-updates
		if i == 0 && s.Snapshot.Checks[1].State != "SUCCESS" {
			t.Fatal(s.Snapshot.Checks)
		}
		if i == 2 && s.ReviewedHead != s.Snapshot.HeadSHA {
			t.Fatal("not refreshed")
		}
	}
	// Exercise replacement of a pending UI frame without dropping activity.
	actions <- domain.Action{Kind: "refresh"}
	actions <- domain.Action{Kind: "refresh"}
	<-updates
	actions <- domain.Action{Kind: "confirm-quit"}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestDemoShutdown(t *testing.T) {
	for _, byContext := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		updates := make(chan domain.State, 1)
		actions := make(chan domain.Action)
		done := make(chan error, 1)
		go func() { done <- demoLoop(ctx, updates, actions, make(chan time.Time)) }()
		<-updates
		if byContext {
			cancel()
		} else {
			close(actions)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("demo leaked")
		}
		cancel()
	}
}
