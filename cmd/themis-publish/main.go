// Command themis-publish reads an OCR review result and publishes its
// findings to a GitHub pull request.
//
// Exit codes: 0 = published (with or without findings); 1 = operational
// failure (bad config, untrusted OCR status, API failure); 2 = the
// severity gate tripped and everything else succeeded.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/coolamit/themis/internal/event"
	"github.com/coolamit/themis/internal/gh"
	"github.com/coolamit/themis/internal/ocr"
)

var severityLevels = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}

// shaRe matches an abbreviated or full hex commit hash — the head SHA
// is handed to git on a command line, so nothing else may pass.
var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	resultPath := "result.json"
	if len(args) > 0 {
		resultPath = args[0]
	}

	rep, err := ocr.DecodeFile(resultPath)
	if err != nil {
		return fail("%v", err)
	}
	if !rep.ReviewRan() {
		return fail("review did not complete (status %q); not publishing%s", rep.Status, ocr.PinHint)
	}

	ev, err := event.Load(os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return fail("%v", err)
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fail("GITHUB_TOKEN is not set")
	}

	failOn := os.Getenv("THEMIS_FAIL_ON_SEVERITY")
	if _, ok := severityLevels[failOn]; failOn != "" && !ok {
		return fail("invalid THEMIS_FAIL_ON_SEVERITY %q: want critical, high, medium, or low", failOn)
	}
	maxComments, err := envInt("THEMIS_MAX_COMMENTS", 25)
	if err != nil {
		return fail("%v", err)
	}
	maxCritical, err := envInt("THEMIS_MAX_CRITICAL_COMMENTS", 50)
	if err != nil {
		return fail("%v", err)
	}

	headSHA := os.Getenv("THEMIS_HEAD_SHA")
	if headSHA == "" {
		headSHA = ev.HeadSHA
	}
	if headSHA != "" && !shaRe.MatchString(headSHA) {
		return fail("invalid head SHA %q", headSHA)
	}
	apiURL := envOr("GITHUB_API_URL", "https://api.github.com")
	serverURL := envOr("GITHUB_SERVER_URL", "https://github.com")

	pub := &gh.Publisher{
		Client:      gh.NewClient(apiURL, token),
		Owner:       ev.Owner,
		Repo:        ev.Repo,
		Number:      ev.Number,
		MaxComments: maxComments,
		MaxCritical: maxCritical,
		FilesURL:    fmt.Sprintf("%s/%s/%s/pull/%d/files", serverURL, ev.Owner, ev.Repo, ev.Number),
		Lookup:      headFileLookup(headSHA),
	}

	res, err := pub.Publish(rep.Comments)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Printf("themis-publish: %d new finding(s): %d inline, %d in overflow summary; %d duplicate(s) skipped\n",
		len(res.NewFindings), res.Inline, res.Overflow, res.Deduped)

	if failOn != "" {
		threshold := severityLevels[failOn]
		for _, c := range res.NewFindings {
			// Unknown or empty severities map to 0 and never trip the gate.
			if severityLevels[c.Severity] >= threshold {
				fmt.Fprintf(os.Stderr, "themis-publish: severity gate tripped: new %s finding in %s at/above %q\n",
					c.Severity, c.Path, failOn)
				return 2
			}
		}
	}
	return 0
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "themis-publish: "+format+"\n", args...)
	return 1
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envInt(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid %s: %q (want a non-negative integer)", name, v)
	}
	return n, nil
}

// headFileLookup reads file content at the PR head from git objects
// (`git show SHA:path`) — the head is never checked out into the
// working tree, so this is the only safe way to see it.
func headFileLookup(sha string) gh.FileContentFunc {
	if sha == "" {
		return nil
	}
	return func(path string) (string, error) {
		// Bounded like the HTTP client: a stalled subprocess must
		// surface as an error, not hang the job.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", "show", sha+":"+path).Output()
		if err != nil {
			return "", fmt.Errorf("git show %s:%s: %w", sha, path, err)
		}
		return string(out), nil
	}
}
