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
// file content at start_line..end_line (compared line by line,
// ignoring only trailing whitespace) — GitHub applies a suggestion by
// replacing exactly the commented lines, so a stale mismatch would
// apply a wrong patch. On mismatch (or any lookup failure) the comment
// is kept and only the suggestion is dropped.
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
	if c.StartLine < 1 || c.EndLine < c.StartLine || c.EndLine > len(lines) {
		return false
	}
	flagged := strings.Join(lines[c.StartLine-1:c.EndLine], "\n")
	// A snippet may legitimately carry the final line's terminator; the
	// selected range never does, so drop one before comparing.
	return linesMatchTrimmed(flagged, strings.TrimSuffix(c.ExistingCode, "\n"))
}

// linesMatchTrimmed compares two snippets line by line, ignoring only
// trailing whitespace (and the CR of CRLF files) on each line.
// Indentation stays significant: leading whitespace can be semantic
// (Python blocks, YAML nesting), so a suggestion must not apply when
// the flagged code drifted in indentation.
func linesMatchTrimmed(a, b string) bool {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(al) != len(bl) {
		return false
	}
	for i := range al {
		if strings.TrimRight(al[i], " \t\r") != strings.TrimRight(bl[i], " \t\r") {
			return false
		}
	}
	return true
}

// DiffAnchor returns the fragment GitHub uses to link to a line of a
// file in a PR's Files Changed view: sha256 of the path, plus the
// right-side line number. A non-positive line links to the file's diff
// as a whole, without a line fragment.
func DiffAnchor(path string, line int) string {
	sum := sha256.Sum256([]byte(path))
	anchor := "#diff-" + hex.EncodeToString(sum[:])
	if line > 0 {
		anchor += fmt.Sprintf("R%d", line)
	}
	return anchor
}

// RenderOverflowEntry builds one list entry of the overflow summary.
// filesURL is the PR's Files Changed URL (…/pull/N/files). The entry
// carries the same fingerprint marker as inline comments so overflow
// findings dedupe across runs too. A finding without usable line info
// is labeled by file alone and linked to the file's whole diff.
func RenderOverflowEntry(c ocr.Comment, filesURL string) string {
	return renderOverflowEntry(c, filesURL, "")
}

// overflowRepeatNote labels entries demoted from inline because their
// line range overlaps a comment Themis already posted.
const overflowRepeatNote = "*(possible repeat of an existing comment)*"

func renderOverflowEntry(c ocr.Comment, filesURL, note string) string {
	label, line := fmt.Sprintf("`%s`", c.Path), 0
	switch {
	case !c.HasUsableLines():
	case c.EndLine > c.StartLine:
		label, line = fmt.Sprintf("`%s` L%d–%d", c.Path, c.StartLine, c.EndLine), c.StartLine
	default:
		label, line = fmt.Sprintf("`%s` L%d", c.Path, c.StartLine), c.StartLine
	}
	entry := fmt.Sprintf("- %s [**%s**](%s%s) — %s",
		severityIcon(c.Severity), label,
		filesURL, DiffAnchor(c.Path, line),
		truncateContent(c.Content))
	if note != "" {
		entry += " " + note
	}
	return entry + " " + fpMarker(c)
}

// overflowHeading and overflowIntro open every overflow summary comment.
const (
	overflowHeading = "### Themis review — additional findings"
	overflowIntro   = "These findings did not fit the inline comment budget, could not be anchored to the diff, or appear to repeat an existing comment:"
)

// maxOverflowBodyBytes caps each overflow summary body, with margin
// under GitHub's 65536-character comment limit.
const maxOverflowBodyBytes = 60000

// RenderOverflowSummaries builds the issue comments that carry findings
// which did not fit the inline budget, could not be anchored to the
// diff, or were demoted as suspected repeats of existing comments
// (repeats — rendered last, each labeled with overflowRepeatNote).
// Entries are split at entry boundaries into as many bodies as needed
// to stay under maxOverflowBodyBytes; each chunk repeats the heading
// (annotated "(part n/N)" when there is more than one) and carries its
// own entries' fingerprint markers, so dedupe keeps working across
// chunks. Returns nil when there is nothing to report.
func RenderOverflowSummaries(comments, repeats []ocr.Comment, filesURL string) []string {
	if len(comments)+len(repeats) == 0 {
		return nil
	}
	entries := make([]string, 0, len(comments)+len(repeats))
	for _, c := range comments {
		entries = append(entries, renderOverflowEntry(c, filesURL, ""))
	}
	for _, c := range repeats {
		entries = append(entries, renderOverflowEntry(c, filesURL, overflowRepeatNote))
	}
	// Greedily pack rendered entries. The allowance reserves room for
	// the part annotation, whose exact width is unknown until the chunk
	// count is; entries are bounded (truncateContent), so a chunk never
	// meaningfully undershoots the cap.
	const partAllowance = len(" (part 9999/9999)")
	budget := maxOverflowBodyBytes - len(overflowHeading) - partAllowance - len("\n\n") - len(overflowIntro) - len("\n\n")
	var chunks [][]string
	var cur []string
	size := 0
	for _, entry := range entries {
		if len(cur) > 0 && size+len("\n")+len(entry) > budget {
			chunks = append(chunks, cur)
			cur, size = nil, 0
		}
		if len(cur) > 0 {
			size += len("\n")
		}
		cur = append(cur, entry)
		size += len(entry)
	}
	chunks = append(chunks, cur)

	out := make([]string, len(chunks))
	for i, entries := range chunks {
		var b strings.Builder
		b.WriteString(overflowHeading)
		if len(chunks) > 1 {
			fmt.Fprintf(&b, " (part %d/%d)", i+1, len(chunks))
		}
		b.WriteString("\n\n")
		b.WriteString(overflowIntro)
		b.WriteString("\n\n")
		b.WriteString(strings.Join(entries, "\n"))
		out[i] = b.String()
	}
	return out
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
