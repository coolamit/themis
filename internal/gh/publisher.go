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
// finding (inline and overflow alike) — the severity gate runs on it.
type Result struct {
	NewFindings []ocr.Comment
	Inline      int
	Overflow    int
	Deduped     int
}

// Publish deduplicates findings against every fingerprint Themis
// itself already posted on the PR (markers written by anyone else are
// ignored — see existingFingerprints), budgets the survivors, posts
// inline comments in batches, and folds everything else into overflow
// summary comments, chunked to stay under GitHub's body-size cap. When
// there is nothing new it posts nothing at all.
func (p *Publisher) Publish(findings []ocr.Comment) (*Result, error) {
	seen, err := p.existingFingerprints()
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

	sel := Select(fresh, p.MaxComments, p.MaxCritical)
	overflow := sel.Overflow

	failed, err := p.postInline(sel.Inline)
	if err != nil {
		return nil, err
	}
	res.Inline = len(sel.Inline) - len(failed)
	overflow = append(overflow, failed...)

	for _, summary := range RenderOverflowSummaries(overflow, p.FilesURL) {
		if err := p.Client.CreateIssueComment(p.Owner, p.Repo, p.Number, summary); err != nil {
			return nil, fmt.Errorf("posting overflow summary: %w", err)
		}
	}
	res.Overflow = len(overflow)
	return res, nil
}

// existingFingerprints collects every themis-fp marker already present
// on the PR — inline review comments and issue comments (where overflow
// summaries live) alike. Only comments authored by the publishing
// identity are honored: github-actions[bot] (what the default Actions
// token posts as) or the token's own login when it resolves. A
// fingerprint is computable from the PR's code alone, so trusting
// markers from arbitrary commenters would let a PR author suppress
// findings — and the severity gate — by pre-posting them.
func (p *Publisher) existingFingerprints() (map[string]bool, error) {
	review, err := p.Client.ListReviewComments(p.Owner, p.Repo, p.Number)
	if err != nil {
		return nil, err
	}
	issue, err := p.Client.ListIssueComments(p.Owner, p.Repo, p.Number)
	if err != nil {
		return nil, err
	}
	self := p.Client.AuthenticatedLogin()
	seen := make(map[string]bool)
	for _, c := range append(review, issue...) {
		if !trustedAuthor(c.User.Login, self) {
			continue
		}
		for _, m := range fpMarkerRe.FindAllStringSubmatch(c.Body, -1) {
			seen[m[1]] = true
		}
	}
	return seen, nil
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
