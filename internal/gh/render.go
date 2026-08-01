package gh

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/coolamit/themis/internal/ocr"
)

// FileContentFunc returns the full content of a repo-relative path at
// the PR head. It must read from git objects (e.g. `git show SHA:path`),
// never from a working tree containing untrusted code.
type FileContentFunc func(path string) (string, error)

// fpMarker renders the invisible dedupe marker embedded in every
// comment Themis posts.
func fpMarker(c ocr.Comment) string {
	return "<!-- themis-fp:" + c.Fingerprint() + " -->"
}

func severityIcon(severity string) string {
	switch severity {
	case "critical":
		return "🔴"
	case "high":
		return "🟠"
	case "medium":
		return "🟡"
	case "low":
		return "🔵"
	}
	return "⚪"
}

// header renders e.g. "🔴 **critical · bug**", omitting the category
// segment when empty and substituting "unspecified" for a missing
// severity.
func header(c ocr.Comment) string {
	severity := c.Severity
	if severity == "" {
		severity = "unspecified"
	}
	h := severityIcon(c.Severity) + " **" + severity
	if c.Category != "" {
		h += " · " + c.Category
	}
	return h + "**"
}

// RenderComment builds the body of an inline review comment. The
// suggestion block is included only when both suggestion_code and
// existing_code are present AND existing_code still matches the head
// file content at start_line..end_line (whitespace-normalized) —
// GitHub applies a suggestion by replacing exactly the commented
// lines, so a stale mismatch would apply a wrong patch. On mismatch
// (or any lookup failure) the comment is kept and only the suggestion
// is dropped.
func RenderComment(c ocr.Comment, lookup FileContentFunc) string {
	parts := []string{header(c), c.Content}
	if suggestionApplies(c, lookup) {
		parts = append(parts, "```suggestion\n"+c.SuggestionCode+"\n```")
	}
	parts = append(parts, fpMarker(c))
	return strings.Join(parts, "\n\n")
}

func suggestionApplies(c ocr.Comment, lookup FileContentFunc) bool {
	if c.SuggestionCode == "" || c.ExistingCode == "" || lookup == nil {
		return false
	}
	content, err := lookup(c.Path)
	if err != nil {
		return false
	}
	lines := strings.Split(content, "\n")
	if c.StartLine < 1 || c.EndLine > len(lines) {
		return false
	}
	flagged := strings.Join(lines[c.StartLine-1:c.EndLine], "\n")
	return ocr.NormalizeWS(flagged) == ocr.NormalizeWS(c.ExistingCode)
}

// DiffAnchor returns the fragment GitHub uses to link to a line of a
// file in a PR's Files Changed view: sha256 of the path, plus the
// right-side line number.
func DiffAnchor(path string, line int) string {
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("#diff-%sR%d", hex.EncodeToString(sum[:]), line)
}

// RenderOverflowEntry builds one list entry of the overflow summary.
// filesURL is the PR's Files Changed URL (…/pull/N/files). The entry
// carries the same fingerprint marker as inline comments so overflow
// findings dedupe across runs too.
func RenderOverflowEntry(c ocr.Comment, filesURL string) string {
	lines := fmt.Sprintf("L%d", c.StartLine)
	if c.EndLine > c.StartLine {
		lines = fmt.Sprintf("L%d–%d", c.StartLine, c.EndLine)
	}
	return fmt.Sprintf("- %s [**`%s` %s**](%s%s) — %s %s",
		severityIcon(c.Severity), c.Path, lines,
		filesURL, DiffAnchor(c.Path, c.StartLine),
		truncateContent(c.Content), fpMarker(c))
}

// RenderOverflowSummary builds the issue comment that carries findings
// which did not fit the inline budget (or could not be anchored to the
// diff). Returns "" when there is nothing to report.
func RenderOverflowSummary(comments []ocr.Comment, filesURL string) string {
	if len(comments) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Themis review — additional findings\n\n")
	b.WriteString("These findings did not fit the inline comment budget or could not be anchored to the diff:\n\n")
	for _, c := range comments {
		b.WriteString(RenderOverflowEntry(c, filesURL))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// truncateContent trims a finding text to roughly two sentences,
// hard-capped at 300 runes, for use in summary entries.
func truncateContent(s string) string {
	const maxSentences = 2
	const maxRunes = 300
	runes := []rune(s)
	sentences := 0
	cut := len(runes)
	for i, r := range runes {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i+1 == len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n' {
			sentences++
			if sentences == maxSentences {
				cut = i + 1
				break
			}
		}
	}
	if cut > maxRunes {
		cut = maxRunes
	}
	out := strings.TrimSpace(string(runes[:cut]))
	if cut < len(runes) {
		out += "…"
	}
	return out
}
