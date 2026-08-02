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

	resp, err := c.HTTP.Do(req)
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

type commentBody struct {
	Body string `json:"body"`
}

// listCommentBodies walks every page of a comment-list endpoint and
// returns just the comment bodies.
func (c *Client) listCommentBodies(path string) ([]string, error) {
	var bodies []string
	url := path + "?per_page=100"
	for url != "" {
		var page []commentBody
		next, err := c.do(http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, cb := range page {
			bodies = append(bodies, cb.Body)
		}
		url = next
	}
	return bodies, nil
}

// ListReviewCommentBodies returns the bodies of all inline review
// comments on a pull request, across all pages.
func (c *Client) ListReviewCommentBodies(owner, repo string, number int) ([]string, error) {
	return c.listCommentBodies(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", owner, repo, number))
}

// ListIssueCommentBodies returns the bodies of all issue comments on a
// pull request (where overflow summaries are posted), across all pages.
func (c *Client) ListIssueCommentBodies(owner, repo string, number int) ([]string, error) {
	return c.listCommentBodies(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number))
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
