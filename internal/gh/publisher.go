package gh

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/coolamit/themis/internal/ocr"
)

// batchLimit is the maximum number of comments per created review; the
// API rejects larger batches.
const batchLimit = 50

// defaultBotLogin is the account the default Actions GITHUB_TOKEN posts
// as; comments it authored are always trusted for dedupe markers.
const defaultBotLogin = "github-actions[bot]"

var fpMarkerRe = regexp.MustCompile(`themis-fp:([0-9a-f]{16})`)

// Publisher posts review findings to one pull request.
type Publisher struct {
	Client      *Client
	Owner       string
	Repo        string
	Number      int
	MaxComments int
	MaxCritical int
	FilesURL    string          // the PR's Files Changed URL, for overflow links
	Lookup      FileContentFunc // head file content, for the suggestion guard
}

// Result reports what Publish did. NewFindings holds every post-dedupe
// finding (inline, overflow, and demoted alike) — the severity gate
// runs on it, so demotion never weakens the gate. Overflow counts
// everything that landed in the summary comments; Demoted is the
// subset relocated there as suspected repeats.
type Result struct {
	NewFindings []ocr.Comment
	Inline      int
	Overflow    int
	Deduped     int
	Demoted     int
}

// Publish deduplicates findings against every fingerprint Themis
// itself already posted on the PR (markers written by anyone else are
// ignored — see priorComments), demotes suspected repeats — new-fp
// findings whose line range overlaps a comment Themis already has on
// the same path (OCR rewords its own identity fields between runs, so
// the same finding can mint a fresh fingerprint) — to the overflow
// summary, budgets the rest, posts inline comments in batches, and
// folds everything else into overflow summary comments, chunked to
// stay under GitHub's body-size cap. Demotion never suppresses: a
// demoted finding is still published and still gates. When there is
// nothing new it posts nothing at all.
func (p *Publisher) Publish(findings []ocr.Comment) (*Result, error) {
	seen, occupied, err := p.priorComments()
	if err != nil {
		return nil, fmt.Errorf("listing existing comments: %w", err)
	}

	res := &Result{}
	var fresh []ocr.Comment
	for _, c := range findings {
		fp := c.Fingerprint()
		if seen[fp] {
			res.Deduped++
			continue
		}
		seen[fp] = true
		fresh = append(fresh, c)
	}
	res.NewFindings = fresh
	if len(fresh) == 0 {
		return res, nil
	}

	// Only pre-existing comments occupy positions: two findings from
	// this run on the same lines are distinct by construction and both
	// post inline.
	var clean, demoted []ocr.Comment
	for _, c := range fresh {
		if c.HasUsableLines() && overlapsAny(occupied[c.Path], c.StartLine, c.EndLine) {
			demoted = append(demoted, c)
		} else {
			clean = append(clean, c)
		}
	}
	res.Demoted = len(demoted)

	sel := Select(clean, p.MaxComments, p.MaxCritical)
	overflow := sel.Overflow

	failed, err := p.postInline(sel.Inline)
	if err != nil {
		return nil, err
	}
	res.Inline = len(sel.Inline) - len(failed)
	overflow = append(overflow, failed...)

	for _, summary := range RenderOverflowSummaries(overflow, demoted, p.FilesURL) {
		if err := p.Client.CreateIssueComment(p.Owner, p.Repo, p.Number, summary); err != nil {
			return nil, fmt.Errorf("posting overflow summary: %w", err)
		}
	}
	res.Overflow = len(overflow) + len(demoted)
	return res, nil
}

// priorComments collects what Themis's own existing comments on the PR
// contribute to this run: every themis-fp marker (for dedupe) from
// inline review comments and issue comments (where overflow summaries
// live) alike, and the diff positions of the live inline ones (for
// repeat demotion), as path → [start, end] ranges. Only comments
// authored by the publishing identity are honored: github-actions[bot]
// (what the default Actions token posts as) or the token's own login
// when it resolves. A fingerprint is computable from the PR's code
// alone, so trusting markers from arbitrary commenters would let a PR
// author suppress findings — and the severity gate — by pre-posting
// them. Positions additionally require a marker in the body: other
// tooling also posts as github-actions[bot], and its comments must not
// demote Themis findings. Demotion is the worst a planted position
// could ever cause — the finding still publishes and still gates.
func (p *Publisher) priorComments() (map[string]bool, map[string][][2]int, error) {
	review, err := p.Client.ListReviewComments(p.Owner, p.Repo, p.Number)
	if err != nil {
		return nil, nil, err
	}
	issue, err := p.Client.ListIssueComments(p.Owner, p.Repo, p.Number)
	if err != nil {
		return nil, nil, err
	}
	self := p.Client.AuthenticatedLogin()
	seen := make(map[string]bool)
	occupied := make(map[string][][2]int)
	for _, c := range append(review, issue...) {
		if !trustedAuthor(c.User.Login, self) {
			continue
		}
		marks := fpMarkerRe.FindAllStringSubmatch(c.Body, -1)
		for _, m := range marks {
			seen[m[1]] = true
		}
		if len(marks) == 0 || c.Path == "" || c.Line <= 0 {
			continue
		}
		start := c.Line
		if c.StartLine > 0 {
			start = c.StartLine
		}
		occupied[c.Path] = append(occupied[c.Path], [2]int{start, c.Line})
	}
	return seen, occupied, nil
}

// overlapsAny reports whether [start, end] intersects any occupied
// range. Exact overlap only — no proximity window: OCR anchors the
// same finding consistently enough for overlap to catch snippet-extent
// drift, while any wider window has been observed to swallow genuinely
// distinct findings a few lines apart.
func overlapsAny(ranges [][2]int, start, end int) bool {
	for _, r := range ranges {
		if start <= r[1] && r[0] <= end {
			return true
		}
	}
	return false
}

// trustedAuthor reports whether dedupe markers in a comment by login
// may be honored. GitHub logins are case-insensitively unique, hence
// EqualFold.
func trustedAuthor(login, self string) bool {
	return strings.EqualFold(login, defaultBotLogin) ||
		(self != "" && strings.EqualFold(login, self))
}

// postInline posts findings as review comments in batches of at most
// batchLimit. A rejected batch (422, typically a line outside the diff)
// is retried comment by comment; comments that still fail with 422 are
// returned so the caller can fold them into the overflow summary. Any
// other API failure — on a batch or an individual retry — aborts.
func (p *Publisher) postInline(findings []ocr.Comment) (failed []ocr.Comment, err error) {
	for start := 0; start < len(findings); start += batchLimit {
		batch := findings[start:min(start+batchLimit, len(findings))]
		comments := make([]reviewComment, len(batch))
		for i, c := range batch {
			comments[i] = toReviewComment(c, p.Lookup)
		}
		err := p.Client.CreateReview(p.Owner, p.Repo, p.Number, comments)
		if err == nil {
			continue
		}
		if !isUnprocessable(err) {
			return nil, fmt.Errorf("creating review: %w", err)
		}
		for i, c := range batch {
			if err := p.Client.CreateReview(p.Owner, p.Repo, p.Number, comments[i:i+1]); err != nil {
				if !isUnprocessable(err) {
					return nil, fmt.Errorf("creating review comment: %w", err)
				}
				failed = append(failed, c)
			}
		}
	}
	return failed, nil
}

func toReviewComment(c ocr.Comment, lookup FileContentFunc) reviewComment {
	rc := reviewComment{
		Path: c.Path,
		Body: RenderComment(c, lookup),
		Line: c.EndLine,
		Side: "RIGHT",
	}
	if c.EndLine > c.StartLine {
		rc.StartLine = c.StartLine
		rc.StartSide = "RIGHT"
	}
	return rc
}

func isUnprocessable(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 422
}
