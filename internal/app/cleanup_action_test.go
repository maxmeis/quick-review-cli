package app

import (
	"context"
	"errors"
	"quick-review-cli/internal/domain"
	"testing"
)

func TestCleanupAction(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		preparing, polling, client, fail, want bool
	}{
		{name: "preparing", preparing: true}, {name: "polling", polling: true},
		{name: "dirty", client: true, fail: true}, {name: "clean", client: true, want: true}, {name: "no client", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(Config{})
			c.preparing = tc.preparing
			c.polling = tc.polling
			c.state.SessionDir = "/session"
			if tc.client {
				c.client = &protocolClient{}
			}
			called := false
			c.cleanup = func(_ context.Context, path string) error {
				called = true
				if path != "/session" {
					t.Fatal(path)
				}
				if tc.fail {
					return errors.New("dirty checkout")
				}
				return nil
			}
			got := c.action(context.Background(), domain.Action{Kind: "confirm-quit-remove"})
			if got != tc.want {
				t.Fatalf("exit=%v, want %v", got, tc.want)
			}
			if called == (tc.preparing || tc.polling) {
				t.Fatalf("cleanup called=%v", called)
			}
			if !tc.want && c.state.Error == "" {
				t.Fatal("missing error")
			}
			if called && c.client != nil {
				t.Fatal("client not stopped")
			}
		})
	}
}
