package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Client is a minimal GitHub REST API client built on the standard
// library only.
type Client struct {
	BaseURL string // e.g. https://api.github.com, no trailing slash
	Token   string
	HTTP    *http.Client
}

// NewClient returns a client with a bounded per-request timeout so a
// stalled connection surfaces as an operational error instead of
// hanging the job. Replace HTTP to customize.
func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// maxResponseBytes caps response reads. It comfortably fits a full
// 100-comment page of maximum-length bodies; anything larger fails with
// an explicit error rather than truncating into a JSON parse failure.
const maxResponseBytes = 10 << 20

// APIError is a non-2xx response from the GitHub API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub API returned %d: %s", e.StatusCode, e.Body)
}

// do performs one request. url may be a path (joined to BaseURL) or an
// absolute URL (as returned in Link headers). The response body is
// decoded into out when out is non-nil; the Link rel="next" URL is
// returned when present.
func (c *Client) do(method, url string, body, out any) (next string, err error) {
	if strings.HasPrefix(url, "/") {
		url = c.BaseURL + url
	} else if !strings.HasPrefix(url, c.BaseURL+"/") {
		// Absolute URLs come from Link headers; the token is never
		// sent anywhere but the configured API origin.
		return "", fmt.Errorf("refusing to follow %s: outside the API origin %s", url, c.BaseURL)
	}
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("encoding request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	httpClient := c.HTTP
	if httpClient == nil {
		// A zero-value Client should degrade to a working default, not
		// panic mid-publish.
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("%s %s: reading response: %w", method, url, err)
	}
	if len(respBody) > maxResponseBytes {
		return "", fmt.Errorf("%s %s: response exceeds %d bytes", method, url, maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return "", fmt.Errorf("%s %s: decoding response: %w", method, url, err)
		}
	}
	return nextLink(resp.Header.Get("Link")), nil
}

var nextLinkRe = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="next"`)

func nextLink(header string) string {
	if m := nextLinkRe.FindStringSubmatch(header); m != nil {
		return m[1]
	}
	return ""
}

// User identifies a GitHub account by its login name.
type User struct {
	Login string `json:"login"`
}

// ListedComment is one existing comment as returned by a comment-list
// endpoint: its body, the account that wrote it, and — for review
// comments — the diff position it currently anchors to. The author
// matters because fingerprint markers are only trusted in comments
// Themis itself posted (see Publisher.priorComments). The position
// fields drive repeat demotion: GitHub keeps Line at the comment's
// place in the current diff and nulls it once the comment goes
// outdated, so an outdated comment (Line 0) never occupies a position;
// issue comments have no position fields at all and decode to zero.
type ListedComment struct {
	Body      string `json:"body"`
	User      User   `json:"user"`
	Path      string `json:"path"`
	Line      int    `json:"line"`       // last (or only) line of the range; 0 when outdated
	StartLine int    `json:"start_line"` // 0 for single-line comments
}

// listComments walks every page of a comment-list endpoint.
func (c *Client) listComments(path string) ([]ListedComment, error) {
	var comments []ListedComment
	url := path + "?per_page=100"
	for url != "" {
		var page []ListedComment
		next, err := c.do(http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		comments = append(comments, page...)
		url = next
	}
	return comments, nil
}

// ListReviewComments returns all inline review comments on a pull
// request, across all pages.
func (c *Client) ListReviewComments(owner, repo string, number int) ([]ListedComment, error) {
	return c.listComments(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", owner, repo, number))
}

// ListIssueComments returns all issue comments on a pull request (where
// overflow summaries are posted), across all pages.
func (c *Client) ListIssueComments(owner, repo string, number int) ([]ListedComment, error) {
	return c.listComments(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number))
}

// AuthenticatedLogin returns the login the token authenticates as (GET
// /user). Any failure returns "" instead of an error — the default
// Actions installation token gets a 403 here, and identifying the
// poster is best-effort hardening that must never fail a publish.
func (c *Client) AuthenticatedLogin() string {
	var u User
	if _, err := c.do(http.MethodGet, "/user", nil, &u); err != nil {
		return ""
	}
	return u.Login
}

// reviewComment is one entry of a review's comments array. Line is the
// last (or only) line of the range; StartLine is set only for
// multi-line comments.
type reviewComment struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
}

type reviewRequest struct {
	Event    string          `json:"event"`
	Comments []reviewComment `json:"comments"`
}

// CreateReview posts one review carrying a batch of inline comments.
func (c *Client) CreateReview(owner, repo string, number int, comments []reviewComment) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, number)
	_, err := c.do(http.MethodPost, path, reviewRequest{Event: "COMMENT", Comments: comments}, nil)
	return err
}

// CreateIssueComment posts a plain comment on the PR conversation.
func (c *Client) CreateIssueComment(owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	_, err := c.do(http.MethodPost, path, map[string]string{"body": body}, nil)
	return err
}
