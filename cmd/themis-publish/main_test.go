package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/coolamit/themis/internal/ocr"
)

// fakeAPI stubs the GitHub endpoints run() touches for PR #7 of
// coolamit/themis (the pull_request_opened.json fixture).
type fakeAPI struct {
	existingReviewBodies []string
	reviewPosts          [][]string // paths of the comments in each posted review
	issuePosts           []string
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	type body struct {
		Body string `json:"body"`
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/user":
		// The default Actions installation token cannot see /user.
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Resource not accessible by integration"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/coolamit/themis/pulls/7/comments":
		// Existing comments are authored by github-actions[bot] — the
		// identity whose dedupe markers the publisher trusts.
		type comment struct {
			Body string            `json:"body"`
			User map[string]string `json:"user"`
		}
		var out []comment
		for _, b := range f.existingReviewBodies {
			out = append(out, comment{Body: b, User: map[string]string{"login": "github-actions[bot]"}})
		}
		json.NewEncoder(w).Encode(out)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/coolamit/themis/issues/7/comments":
		fmt.Fprint(w, "[]")
	case r.Method == http.MethodPost && r.URL.Path == "/repos/coolamit/themis/pulls/7/reviews":
		var req struct {
			Comments []struct {
				Path string `json:"path"`
			} `json:"comments"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var paths []string
		for _, c := range req.Comments {
			paths = append(paths, c.Path)
		}
		f.reviewPosts = append(f.reviewPosts, paths)
		fmt.Fprint(w, "{}")
	case r.Method == http.MethodPost && r.URL.Path == "/repos/coolamit/themis/issues/7/comments":
		var b body
		json.NewDecoder(r.Body).Decode(&b)
		f.issuePosts = append(f.issuePosts, b.Body)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "{}")
	default:
		http.NotFound(w, r)
	}
}

// setupEnv wires run() to the fixture event payload and a fake API.
func setupEnv(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_EVENT_PATH", "../../internal/event/testdata/pull_request_opened.json")
	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	return f
}

const fixtures = "../../internal/ocr/testdata/"

func TestRunMissingResultFile(t *testing.T) {
	setupEnv(t)
	if got := run([]string{"no-such-file.json"}); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestRunUntrustedStatus(t *testing.T) {
	f := setupEnv(t)
	for _, fixture := range []string{"status_failed.json", "status_skipped.json", "status_unknown.json"} {
		if got := run([]string{fixtures + fixture}); got != 1 {
			t.Errorf("%s: exit = %d, want 1", fixture, got)
		}
	}
	if len(f.reviewPosts) != 0 || len(f.issuePosts) != 0 {
		t.Error("untrusted status must not publish anything")
	}
}

// v1.8.5 reports "partial" when some selected items failed; findings
// from the items that did complete are still publishable.
func TestRunPublishesPartialReview(t *testing.T) {
	f := setupEnv(t)
	path := filepath.Join(t.TempDir(), "partial.json")
	doc := `{"status": "partial", "comments": [{"path": "index.php", "content": "finding", "start_line": 3, "end_line": 3, "severity": "high"}]}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run([]string{path}); got != 0 {
		t.Errorf("exit = %d, want 0", got)
	}
	if len(f.reviewPosts) != 1 || len(f.reviewPosts[0]) != 1 {
		t.Fatalf("reviewPosts = %v, want one review with one comment", f.reviewPosts)
	}
}

func TestRunCleanReview(t *testing.T) {
	f := setupEnv(t)
	if got := run([]string{fixtures + "clean.json"}); got != 0 {
		t.Errorf("exit = %d, want 0", got)
	}
	if len(f.reviewPosts) != 0 || len(f.issuePosts) != 0 {
		t.Error("clean run must post nothing")
	}
}

func TestRunPublishesFindings(t *testing.T) {
	f := setupEnv(t)
	if got := run([]string{fixtures + "round1.json"}); got != 0 {
		t.Errorf("exit = %d, want 0", got)
	}
	if len(f.reviewPosts) != 1 || len(f.reviewPosts[0]) != 2 {
		t.Fatalf("reviewPosts = %v, want one review with two comments", f.reviewPosts)
	}
}

func TestRunSeverityGate(t *testing.T) {
	cases := []struct {
		gate string
		want int
	}{
		{"", 0},
		{"critical", 0}, // round1 findings are high
		{"high", 2},
		{"medium", 2},
		{"low", 2},
	}
	for _, tc := range cases {
		t.Run("gate="+tc.gate, func(t *testing.T) {
			setupEnv(t)
			t.Setenv("THEMIS_FAIL_ON_SEVERITY", tc.gate)
			if got := run([]string{fixtures + "round1.json"}); got != tc.want {
				t.Errorf("exit = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRunGateIgnoresDeduplicatedFindings(t *testing.T) {
	f := setupEnv(t)
	rep, err := ocr.DecodeFile(fixtures + "round1.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Comments {
		f.existingReviewBodies = append(f.existingReviewBodies, "<!-- themis-fp:"+c.Fingerprint()+" -->")
	}
	t.Setenv("THEMIS_FAIL_ON_SEVERITY", "high")
	if got := run([]string{fixtures + "round1.json"}); got != 0 {
		t.Errorf("exit = %d, want 0: already-posted findings must not trip the gate", got)
	}
	if len(f.reviewPosts) != 0 {
		t.Error("deduplicated findings were posted again")
	}
}

func TestRunMissingToken(t *testing.T) {
	setupEnv(t)
	t.Setenv("GITHUB_TOKEN", "")
	if got := run([]string{fixtures + "round1.json"}); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestRunMissingEventPath(t *testing.T) {
	setupEnv(t)
	t.Setenv("GITHUB_EVENT_PATH", "")
	if got := run([]string{fixtures + "round1.json"}); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestRunInvalidConfig(t *testing.T) {
	setupEnv(t)
	t.Setenv("THEMIS_FAIL_ON_SEVERITY", "catastrophic")
	if got := run([]string{fixtures + "round1.json"}); got != 1 {
		t.Errorf("bad gate value: exit = %d, want 1", got)
	}

	setupEnv(t)
	t.Setenv("THEMIS_MAX_COMMENTS", "many")
	if got := run([]string{fixtures + "round1.json"}); got != 1 {
		t.Errorf("bad max-comments: exit = %d, want 1", got)
	}

	setupEnv(t)
	t.Setenv("THEMIS_MAX_CRITICAL_COMMENTS", "-2")
	if got := run([]string{fixtures + "round1.json"}); got != 1 {
		t.Errorf("negative max-critical-comments: exit = %d, want 1", got)
	}
}

func TestRunRejectsMalformedHeadSHA(t *testing.T) {
	f := setupEnv(t)
	t.Setenv("THEMIS_HEAD_SHA", "--upload-pack=evil")
	if got := run([]string{fixtures + "round1.json"}); got != 1 {
		t.Errorf("exit = %d, want 1 for a non-hex head SHA", got)
	}
	if len(f.reviewPosts) != 0 {
		t.Error("published despite a malformed head SHA")
	}
}

func TestRunLabeledForkEvent(t *testing.T) {
	// The publisher itself is event-shape agnostic; verify a
	// pull_request_target labeled payload parses and publishes too.
	f := &fakeAPI{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{}`)
		case "/repos/coolamit/themis/pulls/12/comments", "/repos/coolamit/themis/issues/12/comments":
			if r.Method == http.MethodGet {
				fmt.Fprint(w, "[]")
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, "{}")
		case "/repos/coolamit/themis/pulls/12/reviews":
			f.reviewPosts = append(f.reviewPosts, nil)
			fmt.Fprint(w, "{}")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_EVENT_PATH", "../../internal/event/testdata/pull_request_target_labeled.json")
	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GITHUB_API_URL", srv.URL)
	if got := run([]string{fixtures + "round2.json"}); got != 0 {
		t.Errorf("exit = %d, want 0", got)
	}
	if len(f.reviewPosts) != 1 {
		t.Errorf("reviewPosts = %d, want 1", len(f.reviewPosts))
	}
}
