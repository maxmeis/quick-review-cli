package domain

import "time"

type PR struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}
type Check struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	URL         string `json:"url"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
}
type File struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}
type Commit struct {
	SHA   string `json:"sha"`
	Title string `json:"title"`
}
type Snapshot struct {
	PR         PR       `json:"pr"`
	Title      string   `json:"title"`
	URL        string   `json:"url"`
	Body       string   `json:"body"`
	State      string   `json:"state"`
	HeadSHA    string   `json:"headSHA"`
	BaseSHA    string   `json:"baseSHA"`
	BaseBranch string   `json:"baseBranch"`
	Checks     []Check  `json:"checks"`
	Files      []File   `json:"files"`
	Commits    []Commit `json:"commits"`
}
type Event struct {
	ID     int       `json:"id"`
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
	Detail string    `json:"detail,omitempty"`
	SHA    string    `json:"sha,omitempty"`
}
type Agent struct {
	ID     string
	Name   string
	Scope  string
	Status string
	Detail string
}
type Report struct {
	Path       string    `json:"path"`
	HeadSHA    string    `json:"headSHA"`
	BaseSHA    string    `json:"baseSHA"`
	Text       string    `json:"text"`
	CreatedAt  time.Time `json:"createdAt"`
	Stale      bool      `json:"stale"`
	InProgress bool      `json:"inProgress,omitempty"`
}
type Question struct {
	ID      string
	Title   string
	Prompt  string
	Options []string
}
type State struct {
	PendingMessages []string
	WatcherContext  string
	RefreshPending  bool
	Snapshot        Snapshot
	ReviewedHead    string
	Phase           string
	Connection      string
	Events          []Event
	Agents          []Agent
	Reports         []Report
	Diff            string
	DraftReply      string
	Questions       []Question
	Error           string
	SessionDir      string
	ThreadID        string
	QuitRequested   bool
	ClosedPrompt    bool
	Paused          bool
}

// Actions are submitted by the interface to the single controller event loop.
// Kinds: start (Text=PR URL), message, refresh, pause, resume, quit,
// confirm-quit, cancel-quit, answer (ID=question ID, Text=choice), open (Text=URL/path).
type Action struct {
	Kind string
	Text string
	ID   string
}
