// Command review is the terminal client for a watched GitHub pull request review.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"quick-review-cli/internal/app"
	"quick-review-cli/internal/domain"
	"quick-review-cli/internal/ui"
)

var exit = os.Exit

func main() { exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(errOut)
	var root, resume string
	var demo bool
	var interval time.Duration
	flags.StringVar(&root, "data-dir", "", "session storage directory (default: user config/quick-review/sessions)")
	flags.StringVar(&resume, "resume", "", "resume a saved session directory")
	flags.DurationVar(&interval, "poll", 30*time.Second, "GitHub polling interval (minimum 5s)")
	flags.BoolVar(&demo, "demo", false, "explore the interface with simulated events; no network or model calls")
	flags.Usage = func() {
		fmt.Fprintln(out, "Usage: review [options] [https://github.com/OWNER/REPO/pull/NUMBER]\n\nUses the existing logged-in gh and codex CLIs. Run without a URL for the launcher.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 || interval < 5*time.Second {
		fmt.Fprintln(errOut, "Provide at most one PR URL and --poll of at least 5s.")
		return 2
	}
	if resume != "" && flags.NArg() > 0 {
		fmt.Fprintln(errOut, "Use --resume or a PR URL, not both.")
		return 2
	}
	if root == "" {
		var err error
		root, err = defaultRoot()
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	actions := make(chan domain.Action, 32)
	dispatch := func(a domain.Action) {
		select {
		case actions <- a:
		case <-ctx.Done():
		}
	}
	var updates <-chan domain.State
	done := make(chan error, 1)
	if demo {
		stream := make(chan domain.State, 1)
		updates = stream
		go func() { done <- runDemo(ctx, stream, actions) }()
	} else {
		controller := app.New(app.Config{Root: root, ResumeDir: resume, URL: flags.Arg(0), PollInterval: interval})
		updates = controller.Updates()
		go func() { done <- controller.Run(ctx, actions) }()
	}
	err := runUI(ctx, updates, dispatch)
	cancel()
	workerErr := <-done
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if workerErr != nil {
		fmt.Fprintln(errOut, workerErr)
		return 1
	}
	return 0
}

var runUI = ui.Run
var defaultRoot = app.DefaultRoot
