package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"quick-review-cli/internal/domain"
)

// Runner runs a command and returns its standard output. Tests can inject a
// runner to exercise the client without requiring gh authentication.
type Runner func(context.Context, string, ...string) ([]byte, error)

// Client reads pull request data from the GitHub CLI.
type Client struct{ run Runner }

func NewClient(r Runner) *Client {
	if r == nil {
		r = commandRunner
	}
	return &Client{run: r}
}

func commandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() != 0 {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

func (c *Client) CheckAuth(ctx context.Context) error {
	_, err := c.run(ctx, "gh", "auth", "status", "--hostname", "github.com")
	if err != nil {
		return fmt.Errorf("gh authentication check failed: %w", err)
	}
	return nil
}

type prView struct {
	Title             string            `json:"title"`
	Body              string            `json:"body"`
	State             string            `json:"state"`
	HeadRefOID        string            `json:"headRefOid"`
	BaseRefOID        string            `json:"baseRefOid"`
	BaseRefName       string            `json:"baseRefName"`
	StatusCheckRollup json.RawMessage   `json:"statusCheckRollup"`
	Files             []domain.File     `json:"files"`
	Commits           []json.RawMessage `json:"commits"`
	URL               string            `json:"url"`
}

func (c *Client) Snapshot(ctx context.Context, pr domain.PR) (domain.Snapshot, error) {
	if err := validatePR(pr); err != nil {
		return domain.Snapshot{}, err
	}
	repo := pr.Owner + "/" + pr.Repo
	out, err := c.run(ctx, "gh", "pr", "view", fmt.Sprint(pr.Number), "--repo", repo, "--json", "title,body,state,headRefOid,baseRefOid,baseRefName,statusCheckRollup,files,commits,url")
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("read pull request %s#%d: %w", repo, pr.Number, err)
	}
	var view prView
	if err := json.Unmarshal(out, &view); err != nil {
		return domain.Snapshot{}, fmt.Errorf("decode pull request %s#%d: %w", repo, pr.Number, err)
	}
	checks, err := decodeChecks(view.StatusCheckRollup)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("decode pull request checks: %w", err)
	}
	commits, err := decodeCommits(view.Commits)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("decode pull request commits: %w", err)
	}
	return domain.Snapshot{
		PR: pr, Title: view.Title, URL: view.URL, Body: view.Body, State: view.State,
		HeadSHA: view.HeadRefOID, BaseSHA: view.BaseRefOID, BaseBranch: view.BaseRefName,
		Checks: checks, Files: view.Files, Commits: commits,
	}, nil
}

func validatePR(pr domain.PR) error {
	if pr.Number < 1 {
		return fmt.Errorf("pull request number must be positive")
	}
	if !safeRepoPart(pr.Owner) || !safeRepoPart(pr.Repo) {
		return fmt.Errorf("invalid GitHub repository %q/%q", pr.Owner, pr.Repo)
	}
	return nil
}

func safeRepoPart(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

func decodeCommits(raw []json.RawMessage) ([]domain.Commit, error) {
	commits := make([]domain.Commit, 0, len(raw))
	for i, item := range raw {
		var value struct {
			OID             string `json:"oid"`
			MessageHeadline string `json:"messageHeadline"`
			Commit          struct {
				OID             string `json:"oid"`
				MessageHeadline string `json:"messageHeadline"`
			} `json:"commit"`
		}
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, fmt.Errorf("commit %d: %w", i, err)
		}
		sha, title := value.OID, value.MessageHeadline
		if sha == "" {
			sha = value.Commit.OID
		}
		if title == "" {
			title = value.Commit.MessageHeadline
		}
		commits = append(commits, domain.Commit{SHA: sha, Title: title})
	}
	return commits, nil
}

func decodeChecks(raw json.RawMessage) ([]domain.Check, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return []domain.Check{}, nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	checks := make([]domain.Check, 0, len(values))
	for i, item := range values {
		var value struct {
			Type        string `json:"__typename"`
			Name        string `json:"name"`
			Status      string `json:"status"`
			Conclusion  string `json:"conclusion"`
			DetailsURL  string `json:"detailsUrl"`
			Context     string `json:"context"`
			State       string `json:"state"`
			TargetURL   string `json:"targetUrl"`
			StartedAt   string `json:"startedAt"`
			CompletedAt string `json:"completedAt"`
			CreatedAt   string `json:"createdAt"`
			UpdatedAt   string `json:"updatedAt"`
		}
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, fmt.Errorf("check %d: %w", i, err)
		}
		check := domain.Check{StartedAt: value.StartedAt, CompletedAt: value.CompletedAt}
		if value.Type == "StatusContext" || value.Context != "" {
			check.Name = value.Context
			check.State = normalizeStatusContext(value.State)
			check.URL = value.TargetURL
			if check.StartedAt == "" {
				check.StartedAt = value.CreatedAt
			}
			if check.CompletedAt == "" && check.State != "PENDING" && check.State != "EXPECTED" {
				check.CompletedAt = value.UpdatedAt
			}
		} else {
			check.Name = value.Name
			check.State = normalizeCheckRun(value.Status, value.Conclusion)
			check.URL = value.DetailsURL
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func normalizeStatusContext(state string) string {
	switch strings.ToUpper(state) {
	case "EXPECTED":
		return "PENDING"
	case "ERROR", "FAILURE":
		return "FAILURE"
	case "PENDING":
		return "PENDING"
	case "SUCCESS":
		return "SUCCESS"
	default:
		return strings.ToUpper(state)
	}
}

func normalizeCheckRun(status, conclusion string) string {
	status = strings.ToUpper(status)
	conclusion = strings.ToUpper(conclusion)
	switch status {
	case "QUEUED", "WAITING", "REQUESTED", "PENDING":
		return "QUEUED"
	case "IN_PROGRESS":
		return "IN_PROGRESS"
	case "COMPLETED":
		switch conclusion {
		case "SUCCESS":
			return "SUCCESS"
		case "NEUTRAL":
			return "NEUTRAL"
		case "SKIPPED":
			return "SKIPPED"
		case "CANCELLED", "CANCELED":
			return "CANCELLED"
		case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return "FAILURE"
		default:
			return conclusion
		}
	default:
		if conclusion != "" {
			return conclusion
		}
		return status
	}
}
