package gh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/coolamit/themis/internal/ocr"
)

// fakeGitHub simulates the five GitHub endpoints the publisher touches.
type fakeGitHub struct {
	srv             *httptest.Server
	reviewPages     [][]ListedComment // pages of existing review comments
	issuePages      [][]ListedComment // pages of existing issue comments
	userLogin       string            // GET /user login; "" = 403, like the Actions token
	reviews         []reviewRequest
	issueComments   []string
	reviewResponder func(req reviewRequest) int // 0 or 200 = accepted
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/user":
		if f.userLogin == "" {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"Resource not accessible by integration"}`)
			return
		}
		json.NewEncoder(w).Encode(User{Login: f.userLogin})
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/1/comments":
		f.servePages(w, r, f.reviewPages)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1/comments":
		f.servePages(w, r, f.issuePages)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/pulls/1/reviews":
		var req reviewRequest
		json.NewDecoder(r.Body).Decode(&req)
		f.reviews = append(f.reviews, req)
		status := http.StatusOK
		if f.reviewResponder != nil {
			if s := f.reviewResponder(req); s != 0 {
				status = s
			}
		}
		w.WriteHeader(status)
		fmt.Fprint(w, `{"message":"stub"}`)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments":
		var cb ListedComment
		json.NewDecoder(r.Body).Decode(&cb)
		f.issueComments = append(f.issueComments, cb.Body)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{}`)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeGitHub) servePages(w http.ResponseWriter, r *http.Request, pages [][]ListedComment) {
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}
	comments := []ListedComment{}
	if page <= len(pages) {
		comments = append(comments, pages[page-1]...)
	}
	if page < len(pages) {
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?per_page=100&page=%d>; rel="next"`, f.srv.URL, r.URL.Path, page+1))
	}
	json.NewEncoder(w).Encode(comments)
}

func (f *fakeGitHub) publisher() *Publisher {
	return &Publisher{
		Client:      NewClient(f.srv.URL, "test-token"),
		Owner:       "o",
		Repo:        "r",
		Number:      1,
		MaxComments: 25,
		MaxCritical: 50,
		FilesURL:    "https://github.com/o/r/pull/1/files",
	}
}

func finding(path, severity string) ocr.Comment {
	return ocr.Comment{Path: path, Content: "Something is wrong here.", StartLine: 1, EndLine: 1, Severity: severity}
}

func marker(c ocr.Comment) string {
	return "<!-- themis-fp:" + c.Fingerprint() + " -->"
}

// botComment wraps a body as a comment authored by github-actions[bot],
// the identity whose markers the publisher trusts by default.
func botComment(body string) ListedComment {
	return ListedComment{Body: body, User: User{Login: "github-actions[bot]"}}
}

func authoredComment(login, body string) ListedComment {
	return ListedComment{Body: body, User: User{Login: login}}
}

func TestPublishDedupesAcrossPagesAndCommentTypes(t *testing.T) {
	a, b, c, d := finding("a.go", "high"), finding("b.go", "high"), finding("c.go", "high"), finding("d.go", "high")
	f := newFakeGitHub(t)
	f.reviewPages = [][]ListedComment{
		{botComment("some other comment"), botComment("review comment\n" + marker(a))},
		{botComment("page two comment\n" + marker(b))},
	}
	f.issuePages = [][]ListedComment{{botComment("overflow summary\n" + marker(c))}}

	res, err := f.publisher().Publish([]ocr.Comment{a, b, c, d})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Deduped != 3 {
		t.Errorf("Deduped = %d, want 3", res.Deduped)
	}
	if res.Inline != 1 || len(res.NewFindings) != 1 {
		t.Errorf("Inline = %d, NewFindings = %d, want 1/1", res.Inline, len(res.NewFindings))
	}
	if len(f.reviews) != 1 || len(f.reviews[0].Comments) != 1 {
		t.Fatalf("reviews posted = %+v, want one review with one comment", f.reviews)
	}
	if !strings.Contains(f.reviews[0].Comments[0].Body, d.Fingerprint()) {
		t.Errorf("posted comment is not finding d: %q", f.reviews[0].Comments[0].Body)
	}
}

func TestPublishDedupesWithinOneReport(t *testing.T) {
	a := finding("a.go", "high")
	f := newFakeGitHub(t)

	res, err := f.publisher().Publish([]ocr.Comment{a, a})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Deduped != 1 || res.Inline != 1 {
		t.Errorf("Deduped = %d, Inline = %d, want 1/1", res.Deduped, res.Inline)
	}
	if len(f.reviews) != 1 || len(f.reviews[0].Comments) != 1 {
		t.Fatalf("reviews posted = %+v, want one review with one comment", f.reviews)
	}
}

func TestPublishSilentWhenNothingNew(t *testing.T) {
	a, b := finding("a.go", "high"), finding("b.go", "low")
	f := newFakeGitHub(t)
	f.reviewPages = [][]ListedComment{{botComment(marker(a)), botComment(marker(b))}}

	res, err := f.publisher().Publish([]ocr.Comment{a, b})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(f.reviews) != 0 || len(f.issueComments) != 0 {
		t.Errorf("posted %d reviews and %d issue comments, want none", len(f.reviews), len(f.issueComments))
	}
	if res.Deduped != 2 || len(res.NewFindings) != 0 {
		t.Errorf("Deduped = %d, NewFindings = %d, want 2/0", res.Deduped, len(res.NewFindings))
	}
}

func TestPublishNoFindings(t *testing.T) {
	f := newFakeGitHub(t)
	res, err := f.publisher().Publish(nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(f.reviews) != 0 || len(f.issueComments) != 0 || len(res.NewFindings) != 0 {
		t.Error("empty input should post nothing")
	}
}

func TestPublishSplitsBatchesAt50(t *testing.T) {
	var findings []ocr.Comment
	for i := 0; i < 120; i++ {
		findings = append(findings, finding(fmt.Sprintf("file%03d.go", i), "critical"))
	}
	f := newFakeGitHub(t)
	p := f.publisher()
	p.MaxCritical = 200 // let all 120 through the budget

	res, err := p.Publish(findings)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(f.reviews) != 3 {
		t.Fatalf("got %d review posts, want 3", len(f.reviews))
	}
	for i, want := range []int{50, 50, 20} {
		if len(f.reviews[i].Comments) != want {
			t.Errorf("batch %d size = %d, want %d", i, len(f.reviews[i].Comments), want)
		}
	}
	if res.Inline != 120 {
		t.Errorf("Inline = %d, want 120", res.Inline)
	}
}

func TestPublish422FallbackChain(t *testing.T) {
	good1, bad, good2 := finding("good1.go", "high"), finding("bad.go", "high"), finding("good2.go", "high")
	f := newFakeGitHub(t)
	f.reviewResponder = func(req reviewRequest) int {
		if len(req.Comments) > 1 {
			return 422 // reject the batch
		}
		if req.Comments[0].Path == "bad.go" {
			return 422 // this line is not in the diff
		}
		return 200
	}

	res, err := f.publisher().Publish([]ocr.Comment{good1, bad, good2})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(f.reviews) != 4 {
		t.Errorf("got %d review posts, want 4 (1 batch + 3 singles)", len(f.reviews))
	}
	if res.Inline != 2 || res.Overflow != 1 {
		t.Errorf("Inline = %d, Overflow = %d, want 2/1", res.Inline, res.Overflow)
	}
	if len(f.issueComments) != 1 {
		t.Fatalf("got %d issue comments, want 1 overflow summary", len(f.issueComments))
	}
	if !strings.Contains(f.issueComments[0], bad.Fingerprint()) {
		t.Errorf("overflow summary does not carry the failed finding: %q", f.issueComments[0])
	}
	if strings.Contains(f.issueComments[0], good1.Fingerprint()) {
		t.Error("successfully posted finding leaked into the overflow summary")
	}
}

func TestPublishAbortsOnServerErrorDuringRetry(t *testing.T) {
	f := newFakeGitHub(t)
	f.reviewResponder = func(req reviewRequest) int {
		if len(req.Comments) > 1 {
			return 422 // reject the batch to force individual retries
		}
		if req.Comments[0].Path == "boom.go" {
			return 500
		}
		return 200
	}

	_, err := f.publisher().Publish([]ocr.Comment{finding("ok.go", "high"), finding("boom.go", "high")})
	if err == nil {
		t.Fatal("Publish succeeded despite 500 during an individual retry")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error does not surface the status: %v", err)
	}
	if len(f.issueComments) != 0 {
		t.Errorf("overflow summary posted despite aborted publish: %v", f.issueComments)
	}
}

func TestPublishOverflowOnBudget(t *testing.T) {
	var findings []ocr.Comment
	for i := 0; i < 30; i++ {
		findings = append(findings, finding(fmt.Sprintf("file%02d.go", i), "medium"))
	}
	f := newFakeGitHub(t)
	res, err := f.publisher().Publish(findings)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Inline != 25 || res.Overflow != 5 {
		t.Errorf("Inline = %d, Overflow = %d, want 25/5", res.Inline, res.Overflow)
	}
	if len(f.issueComments) != 1 {
		t.Fatalf("got %d issue comments, want 1", len(f.issueComments))
	}
	if got := strings.Count(f.issueComments[0], "themis-fp:"); got != 5 {
		t.Errorf("overflow summary has %d markers, want 5", got)
	}
}

func TestPublishAbortsOnServerError(t *testing.T) {
	f := newFakeGitHub(t)
	f.reviewResponder = func(reviewRequest) int { return 500 }
	_, err := f.publisher().Publish([]ocr.Comment{finding("a.go", "high")})
	if err == nil {
		t.Fatal("Publish succeeded despite 500 from the API")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error does not surface the status: %v", err)
	}
}

func TestPublishMultiLineCommentShape(t *testing.T) {
	c := ocr.Comment{Path: "index.php", Content: "Inverted logic.", StartLine: 5, EndLine: 9, Severity: "high"}
	f := newFakeGitHub(t)
	if _, err := f.publisher().Publish([]ocr.Comment{c}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	rc := f.reviews[0].Comments[0]
	if rc.Line != 9 || rc.StartLine != 5 || rc.Side != "RIGHT" || rc.StartSide != "RIGHT" {
		t.Errorf("multi-line comment shape = %+v", rc)
	}

	f2 := newFakeGitHub(t)
	single := ocr.Comment{Path: "index.php", Content: "x", StartLine: 3, EndLine: 3, Severity: "low"}
	if _, err := f2.publisher().Publish([]ocr.Comment{single}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	rc = f2.reviews[0].Comments[0]
	if rc.Line != 3 || rc.StartLine != 0 || rc.StartSide != "" {
		t.Errorf("single-line comment shape = %+v", rc)
	}
}

// A PR author can compute fingerprints for their own code, so markers
// in comments from anyone but the publishing identity must not dedupe —
// otherwise pre-posting them would suppress findings and the gate.
// Markers from github-actions[bot] must keep deduping even though GET
// /user answers 403 for the Actions token (userLogin is unset here).
func TestPublishIgnoresMarkersFromUntrustedAuthors(t *testing.T) {
	a, b, c := finding("a.go", "high"), finding("b.go", "high"), finding("c.go", "high")
	f := newFakeGitHub(t)
	f.reviewPages = [][]ListedComment{{
		authoredComment("mallory", "nice PR!\n"+marker(a)),
		botComment(marker(b)),
	}}
	f.issuePages = [][]ListedComment{{authoredComment("mallory", marker(c))}}

	res, err := f.publisher().Publish([]ocr.Comment{a, b, c})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Deduped != 1 {
		t.Errorf("Deduped = %d, want 1 (only the bot-authored marker)", res.Deduped)
	}
	if res.Inline != 2 || len(res.NewFindings) != 2 {
		t.Errorf("Inline = %d, NewFindings = %d, want 2/2", res.Inline, len(res.NewFindings))
	}
	if len(f.reviews) != 1 || len(f.reviews[0].Comments) != 2 {
		t.Fatalf("reviews posted = %+v, want one review with two comments", f.reviews)
	}
	for i, want := range []ocr.Comment{a, c} {
		if !strings.Contains(f.reviews[0].Comments[i].Body, want.Fingerprint()) {
			t.Errorf("posted comment %d is not the expected finding: %q", i, f.reviews[0].Comments[i].Body)
		}
	}
}

// When the token resolves to a real user (a PAT), that user's own
// comments carry trusted markers. GitHub logins are case-insensitive,
// so a case difference must not break the match.
func TestPublishTrustsAuthenticatedLogin(t *testing.T) {
	a, b := finding("a.go", "high"), finding("b.go", "high")
	f := newFakeGitHub(t)
	f.userLogin = "review-bot"
	f.reviewPages = [][]ListedComment{{
		authoredComment("Review-Bot", marker(a)),
		authoredComment("mallory", marker(b)),
	}}

	res, err := f.publisher().Publish([]ocr.Comment{a, b})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Deduped != 1 {
		t.Errorf("Deduped = %d, want 1 (the PAT user's marker)", res.Deduped)
	}
	if len(f.reviews) != 1 || len(f.reviews[0].Comments) != 1 {
		t.Fatalf("reviews posted = %+v, want one review with one comment", f.reviews)
	}
	if !strings.Contains(f.reviews[0].Comments[0].Body, b.Fingerprint()) {
		t.Errorf("posted comment is not finding b: %q", f.reviews[0].Comments[0].Body)
	}
}

func TestPublishChunksOverflowSummary(t *testing.T) {
	// 300 findings with 300-rune contents overflow entirely (zero
	// budgets) and exceed one comment body's worth of entries.
	content := strings.Repeat("x", 300) + "."
	var findings []ocr.Comment
	for i := 0; i < 300; i++ {
		findings = append(findings, ocr.Comment{
			Path: fmt.Sprintf("file%03d.go", i), Content: content,
			StartLine: 1, EndLine: 1, Severity: "medium",
		})
	}
	f := newFakeGitHub(t)
	p := f.publisher()
	p.MaxComments = 0
	p.MaxCritical = 0

	res, err := p.Publish(findings)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Overflow != 300 {
		t.Errorf("Overflow = %d, want 300", res.Overflow)
	}
	if len(f.issueComments) < 2 {
		t.Fatalf("got %d issue comments, want the overflow summary chunked into several", len(f.issueComments))
	}
	markers := 0
	for i, body := range f.issueComments {
		if len(body) > 65536 {
			t.Errorf("chunk %d is %d bytes, above GitHub's comment cap", i, len(body))
		}
		if !strings.Contains(body, fmt.Sprintf("(part %d/%d)", i+1, len(f.issueComments))) {
			t.Errorf("chunk %d missing its part annotation: %q", i, body[:80])
		}
		markers += strings.Count(body, "themis-fp:")
	}
	if markers != 300 {
		t.Errorf("chunks carry %d markers in total, want 300 (no entry lost)", markers)
	}
}
