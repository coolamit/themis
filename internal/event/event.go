package event

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Event is the subset of a pull_request / pull_request_target payload
// that themis-publish needs.
type Event struct {
	Action    string
	Number    int
	Owner     string // base repository owner
	Repo      string // base repository name
	BaseRef   string
	HeadSHA   string
	IsFork    bool   // head repo differs from (or no longer exists in) the base repo
	LabelName string // set for labeled events
	Sender    string // the user who triggered the event
}

type repoRef struct {
	FullName string `json:"full_name"`
}

type payload struct {
	Action string `json:"action"`
	Number int    `json:"number"`
	Label  struct {
		Name string `json:"name"`
	} `json:"label"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	Repository  repoRef `json:"repository"`
	PullRequest struct {
		Number int `json:"number"`
		Base   struct {
			Ref  string   `json:"ref"`
			Repo *repoRef `json:"repo"`
		} `json:"base"`
		Head struct {
			SHA  string   `json:"sha"`
			Repo *repoRef `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

// Parse decodes a pull_request or pull_request_target event payload.
func Parse(r io.Reader) (*Event, error) {
	var p payload
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, fmt.Errorf("parsing event payload: %w", err)
	}

	number := p.PullRequest.Number
	if number == 0 {
		number = p.Number
	}
	if number <= 0 {
		return nil, fmt.Errorf("event payload has no pull request number")
	}
	if p.PullRequest.Head.SHA == "" {
		return nil, fmt.Errorf("event payload has no head SHA")
	}
	if p.PullRequest.Base.Ref == "" {
		return nil, fmt.Errorf("event payload has no base ref")
	}

	baseFull := p.Repository.FullName
	if p.PullRequest.Base.Repo != nil {
		baseFull = p.PullRequest.Base.Repo.FullName
	}
	owner, repo, ok := strings.Cut(baseFull, "/")
	if !ok || owner == "" || repo == "" {
		return nil, fmt.Errorf("event payload has no usable repository name (got %q)", baseFull)
	}

	// A nil head repo means the fork was deleted; treat it as a fork.
	// Full names are compared case-insensitively, as GitHub treats them.
	isFork := p.PullRequest.Head.Repo == nil || !strings.EqualFold(p.PullRequest.Head.Repo.FullName, baseFull)

	return &Event{
		Action:    p.Action,
		Number:    number,
		Owner:     owner,
		Repo:      repo,
		BaseRef:   p.PullRequest.Base.Ref,
		HeadSHA:   p.PullRequest.Head.SHA,
		IsFork:    isFork,
		LabelName: p.Label.Name,
		Sender:    p.Sender.Login,
	}, nil
}

// Load reads and parses the event payload file, normally the one
// GITHUB_EVENT_PATH points at.
func Load(path string) (*Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading event payload: %w", err)
	}
	defer f.Close()
	return Parse(f)
}
