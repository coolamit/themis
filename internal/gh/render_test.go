package gh

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/coolamit/themis/internal/ocr"
)

var guardComment = ocr.Comment{
	Path:           "index.php",
	Content:        "The array key may be missing.",
	SuggestionCode: "$code = $_GET['code'] ?? null;",
	ExistingCode:   "$code = $_GET['code'];",
	StartLine:      3,
	EndLine:        3,
	Category:       "bug",
	Severity:       "high",
}

// headFile has guardComment's flagged code on line 3.
const headFile = "<?php\n\n$code = $_GET['code'];\n\nif ( 1 > $code ) {\n\techo 'Code is greater';\n} else {\n\techo 'Code is less';\n}\n"

func lookupHead(path string) (string, error) { return headFile, nil }

func TestRenderCommentWithSuggestion(t *testing.T) {
	body := RenderComment(guardComment, lookupHead)
	if !strings.HasPrefix(body, "🟠 **high · bug**\n\n") {
		t.Errorf("header wrong: %q", body)
	}
	if !strings.Contains(body, "\n\n```suggestion\n$code = $_GET['code'] ?? null;\n```") {
		t.Errorf("suggestion block missing or malformed: %q", body)
	}
	marker := "<!-- themis-fp:" + guardComment.Fingerprint() + " -->"
	if !strings.HasSuffix(body, marker) {
		t.Errorf("body does not end with fp marker %q: %q", marker, body)
	}
}

func TestRenderCommentSuggestionSurvivesTrailingWhitespace(t *testing.T) {
	c := guardComment
	c.ExistingCode = "$code = $_GET['code'];  \t"
	body := RenderComment(c, lookupHead)
	if !strings.Contains(body, "```suggestion") {
		t.Error("trailing-whitespace difference dropped the suggestion block")
	}
}

func TestRenderCommentSuggestionSurvivesTrailingNewline(t *testing.T) {
	c := guardComment
	c.ExistingCode = "$code = $_GET['code'];\n"
	if !strings.Contains(RenderComment(c, lookupHead), "```suggestion") {
		t.Error("a snippet carrying its final line terminator dropped the suggestion block")
	}
}

func TestRenderCommentSuggestionSurvivesCRLF(t *testing.T) {
	crlf := strings.ReplaceAll(headFile, "\n", "\r\n")
	body := RenderComment(guardComment, func(string) (string, error) { return crlf, nil })
	if !strings.Contains(body, "```suggestion") {
		t.Error("CRLF line endings in the head file dropped the suggestion block")
	}
}

func TestRenderCommentSuggestionDroppedOnIndentationDrift(t *testing.T) {
	// Leading whitespace can be semantic (Python blocks, YAML nesting);
	// an indentation-only difference must drop the suggestion.
	c := guardComment
	c.ExistingCode = "  $code = $_GET['code'];"
	if strings.Contains(RenderComment(c, lookupHead), "```suggestion") {
		t.Error("indentation drift kept the suggestion block")
	}

	c.ExistingCode = "$code   =   $_GET['code'];"
	if strings.Contains(RenderComment(c, lookupHead), "```suggestion") {
		t.Error("internal-spacing drift kept the suggestion block")
	}
}

func TestRenderCommentSuggestionDroppedOnMismatch(t *testing.T) {
	c := guardComment
	c.ExistingCode = "$code = $_GET['token'];" // no longer what line 3 says
	body := RenderComment(c, lookupHead)
	if strings.Contains(body, "```suggestion") {
		t.Error("stale existing_code kept the suggestion block")
	}
	if !strings.Contains(body, c.Content) {
		t.Error("comment content lost when suggestion dropped")
	}
	if !strings.Contains(body, "themis-fp:") {
		t.Error("fp marker lost when suggestion dropped")
	}
}

func TestRenderCommentSuggestionGuardEdgeCases(t *testing.T) {
	noSuggestion := guardComment
	noSuggestion.SuggestionCode = ""
	if strings.Contains(RenderComment(noSuggestion, lookupHead), "```suggestion") {
		t.Error("suggestion block rendered without suggestion_code")
	}

	noExisting := guardComment
	noExisting.ExistingCode = ""
	if strings.Contains(RenderComment(noExisting, lookupHead), "```suggestion") {
		t.Error("suggestion block rendered without existing_code")
	}

	if strings.Contains(RenderComment(guardComment, nil), "```suggestion") {
		t.Error("suggestion block rendered without a content lookup")
	}

	failing := func(path string) (string, error) { return "", errors.New("no such object") }
	if strings.Contains(RenderComment(guardComment, failing), "```suggestion") {
		t.Error("suggestion block rendered despite lookup failure")
	}

	outOfRange := guardComment
	outOfRange.StartLine = 500
	outOfRange.EndLine = 500
	if strings.Contains(RenderComment(outOfRange, lookupHead), "```suggestion") {
		t.Error("suggestion block rendered for lines beyond EOF")
	}

	reversed := guardComment
	reversed.StartLine = 5
	reversed.EndLine = 2
	if strings.Contains(RenderComment(reversed, lookupHead), "```suggestion") {
		t.Error("suggestion block rendered for a reversed line range")
	}
}

func TestRenderCommentMultiLineGuard(t *testing.T) {
	c := ocr.Comment{
		Path:           "index.php",
		Content:        "Inverted comparison.",
		SuggestionCode: "if ($code > 1) {\n\techo 'Code is greater';\n}",
		ExistingCode:   "if ( 1 > $code ) {\n\techo 'Code is greater';\n} else {\n\techo 'Code is less';\n}",
		StartLine:      5,
		EndLine:        9,
		Category:       "bug",
		Severity:       "high",
	}
	if !strings.Contains(RenderComment(c, lookupHead), "```suggestion") {
		t.Error("multi-line existing_code matching lines 5-9 did not keep the suggestion")
	}
}

func TestRenderCommentHeaderVariants(t *testing.T) {
	cases := []struct {
		severity, category, want string
	}{
		{"critical", "bug", "🔴 **critical · bug**"},
		{"high", "security", "🟠 **high · security**"},
		{"medium", "", "🟡 **medium**"},
		{"low", "style", "🔵 **low · style**"},
		{"", "bug", "⚪ **unspecified · bug**"},
		{"", "", "⚪ **unspecified**"},
	}
	for _, tc := range cases {
		c := ocr.Comment{Path: "a.go", Content: "x", StartLine: 1, EndLine: 1, Severity: tc.severity, Category: tc.category}
		body := RenderComment(c, nil)
		if !strings.HasPrefix(body, tc.want+"\n\n") {
			t.Errorf("severity=%q category=%q: got %q, want prefix %q", tc.severity, tc.category, body, tc.want)
		}
	}
}

func TestDiffAnchor(t *testing.T) {
	sum := sha256.Sum256([]byte("src/api.php"))
	want := "#diff-" + hex.EncodeToString(sum[:]) + "R42"
	if got := DiffAnchor("src/api.php", 42); got != want {
		t.Errorf("DiffAnchor = %q, want %q", got, want)
	}
	// A non-positive line links to the file's diff without a fragment.
	want = "#diff-" + hex.EncodeToString(sum[:])
	if got := DiffAnchor("src/api.php", 0); got != want {
		t.Errorf("DiffAnchor line 0 = %q, want %q", got, want)
	}
}

func TestRenderOverflowEntry(t *testing.T) {
	c := ocr.Comment{
		Path:      "src/api.php",
		Content:   "First sentence. Second sentence. Third sentence should be cut.",
		StartLine: 42,
		EndLine:   48,
		Severity:  "high",
	}
	entry := RenderOverflowEntry(c, "https://github.com/o/r/pull/7/files")
	if !strings.HasPrefix(entry, "- 🟠 [**`src/api.php` L42–48**](https://github.com/o/r/pull/7/files#diff-") {
		t.Errorf("entry = %q", entry)
	}
	if !strings.Contains(entry, "R42)") {
		t.Errorf("entry link does not anchor to start line: %q", entry)
	}
	if !strings.Contains(entry, "First sentence. Second sentence.…") {
		t.Errorf("content not truncated to two sentences: %q", entry)
	}
	if !strings.Contains(entry, "themis-fp:"+c.Fingerprint()) {
		t.Errorf("entry missing fp marker: %q", entry)
	}

	single := c
	single.EndLine = 42
	if !strings.Contains(RenderOverflowEntry(single, ""), "L42**") {
		t.Errorf("single-line entry should say L42: %q", RenderOverflowEntry(single, ""))
	}
}

func TestRenderOverflowEntryWithoutUsableLines(t *testing.T) {
	c := ocr.Comment{Path: "src/api.php", Content: "No line info.", Severity: "high"}
	sum := sha256.Sum256([]byte(c.Path))
	entry := RenderOverflowEntry(c, "https://github.com/o/r/pull/7/files")
	want := "- 🟠 [**`src/api.php`**](https://github.com/o/r/pull/7/files#diff-" + hex.EncodeToString(sum[:]) + ")"
	if !strings.HasPrefix(entry, want) {
		t.Errorf("line-less entry = %q, want prefix %q", entry, want)
	}
	if strings.Contains(entry, "R0") || strings.Contains(entry, "L0") {
		t.Errorf("line-less entry renders a bogus line reference: %q", entry)
	}
	if !strings.Contains(entry, "themis-fp:"+c.Fingerprint()) {
		t.Errorf("entry missing fp marker: %q", entry)
	}

	reversed := ocr.Comment{Path: "src/api.php", Content: "x", StartLine: 5, EndLine: 2, Severity: "high"}
	if got := RenderOverflowEntry(reversed, ""); strings.Contains(got, "R5") || strings.Contains(got, "L5") {
		t.Errorf("reversed-range entry renders a bogus line reference: %q", got)
	}
}

func TestRenderOverflowSummaries(t *testing.T) {
	if got := RenderOverflowSummaries(nil, ""); got != nil {
		t.Errorf("empty overflow should render nothing, got %q", got)
	}
	comments := []ocr.Comment{
		{Path: "a.go", Content: "One.", StartLine: 1, EndLine: 1, Severity: "high"},
		{Path: "b.go", Content: "Two.", StartLine: 2, EndLine: 2, Severity: "low"},
	}
	filesURL := "https://github.com/o/r/pull/7/files"
	out := RenderOverflowSummaries(comments, filesURL)
	if len(out) != 1 {
		t.Fatalf("small set rendered %d comments, want 1", len(out))
	}
	// A set that fits in one comment must keep the unchunked format:
	// plain heading, entries newline-joined, no part annotation.
	want := "### Themis review — additional findings\n\n" +
		"These findings did not fit the inline comment budget or could not be anchored to the diff:\n\n" +
		RenderOverflowEntry(comments[0], filesURL) + "\n" + RenderOverflowEntry(comments[1], filesURL)
	if out[0] != want {
		t.Errorf("single-chunk summary = %q, want %q", out[0], want)
	}
}

func TestRenderOverflowSummariesChunks(t *testing.T) {
	var comments []ocr.Comment
	for i := 0; i < 400; i++ {
		comments = append(comments, ocr.Comment{
			Path: fmt.Sprintf("dir/file%03d.go", i), Content: strings.Repeat("y", 299) + ".",
			StartLine: i + 1, EndLine: i + 1, Severity: "low",
		})
	}
	out := RenderOverflowSummaries(comments, "https://github.com/o/r/pull/7/files")
	if len(out) < 2 {
		t.Fatalf("large set rendered %d comments, want several", len(out))
	}
	for i, body := range out {
		if len(body) > maxOverflowBodyBytes {
			t.Errorf("chunk %d is %d bytes, above the %d cap", i, len(body), maxOverflowBodyBytes)
		}
		if !strings.HasPrefix(body, fmt.Sprintf("### Themis review — additional findings (part %d/%d)\n\n", i+1, len(out))) {
			t.Errorf("chunk %d heading wrong: %q", i, body[:80])
		}
	}
	// Every entry lands in exactly one chunk, marker intact.
	all := strings.Join(out, "\n")
	for _, c := range comments {
		if got := strings.Count(all, "themis-fp:"+c.Fingerprint()); got != 1 {
			t.Errorf("marker for %s appears %d times across chunks, want 1", c.Path, got)
		}
	}
}

func TestTruncateContent(t *testing.T) {
	if got := truncateContent("Short text without sentence end"); got != "Short text without sentence end" {
		t.Errorf("short text altered: %q", got)
	}
	if got := truncateContent("One. Two. Three."); got != "One. Two.…" {
		t.Errorf("sentence truncation = %q", got)
	}
	long := strings.Repeat("word ", 100) // 500 runes, no sentence ends
	got := truncateContent(long)
	if len([]rune(got)) > 301 { // 300 + ellipsis
		t.Errorf("long text not capped: %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("capped text missing ellipsis: %q", got)
	}
}
