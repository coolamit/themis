package ocr

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

var baseComment = Comment{
	Path:         "index.php",
	Content:      "The array key may be missing.",
	ExistingCode: "$code = $_GET['code'];",
	StartLine:    3,
	EndLine:      3,
	Category:     "bug",
	Severity:     "high",
}

// TestFingerprintByteLayout locks the exact hash input format:
// path NUL normalized-existing-code NUL category, sha256, first 8 bytes.
func TestFingerprintByteLayout(t *testing.T) {
	sum := sha256.Sum256([]byte("index.php\x00$code = $_GET['code'];\x00bug"))
	want := hex.EncodeToString(sum[:8])
	if got := baseComment.Fingerprint(); got != want {
		t.Errorf("Fingerprint() = %q, want %q", got, want)
	}
	if len(want) != 16 {
		t.Fatalf("fingerprint length = %d, want 16 hex chars", len(want))
	}
}

func TestFingerprintIgnoresRewording(t *testing.T) {
	reworded := baseComment
	reworded.Content = "Accessing $_GET without isset() risks an undefined array key warning."
	if reworded.Fingerprint() != baseComment.Fingerprint() {
		t.Error("rewording the finding text changed the fingerprint")
	}
}

func TestFingerprintIgnoresLineDrift(t *testing.T) {
	drifted := baseComment
	drifted.StartLine = 42
	drifted.EndLine = 42
	if drifted.Fingerprint() != baseComment.Fingerprint() {
		t.Error("line drift changed the fingerprint")
	}
}

func TestFingerprintIgnoresWhitespaceChurn(t *testing.T) {
	indented := baseComment
	indented.ExistingCode = "\t$code   = $_GET['code'];  \n"
	if indented.Fingerprint() != baseComment.Fingerprint() {
		t.Error("whitespace churn in the flagged code changed the fingerprint")
	}

	multiline := Comment{Path: "a.php", ExistingCode: "if (x) {\n\tdoThing();\n}", Category: "bug"}
	collapsed := Comment{Path: "a.php", ExistingCode: "if (x) { doThing(); }", Category: "bug"}
	if multiline.Fingerprint() != collapsed.Fingerprint() {
		t.Error("newline/indent-only variants produced different fingerprints")
	}
}

func TestFingerprintChangesWithCode(t *testing.T) {
	changed := baseComment
	changed.ExistingCode = "$code = $_GET['code'] ?? null;"
	if changed.Fingerprint() == baseComment.Fingerprint() {
		t.Error("changing the flagged code did not change the fingerprint")
	}
}

func TestFingerprintChangesWithCategory(t *testing.T) {
	recategorized := baseComment
	recategorized.Category = "security"
	if recategorized.Fingerprint() == baseComment.Fingerprint() {
		t.Error("changing the category did not change the fingerprint")
	}
}

func TestFingerprintSnippetlessFindingsStayDistinct(t *testing.T) {
	a := Comment{Path: "a.go", Content: "First problem.", StartLine: 3, EndLine: 3, Category: "bug"}
	b := Comment{Path: "a.go", Content: "Second problem.", StartLine: 20, EndLine: 25, Category: "bug"}
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("distinct snippet-less findings on the same path+category share a fingerprint")
	}

	whitespaceOnly := a
	whitespaceOnly.ExistingCode = " \n\t"
	if whitespaceOnly.Fingerprint() != a.Fingerprint() {
		t.Error("whitespace-only existing_code treated differently from empty")
	}

	same := a
	same.Content = "Reworded description of the first problem."
	if same.Fingerprint() != a.Fingerprint() {
		t.Error("rewording changed a snippet-less finding's fingerprint")
	}
}

func TestFingerprintChangesWithPath(t *testing.T) {
	moved := baseComment
	moved.Path = "other.php"
	if moved.Fingerprint() == baseComment.Fingerprint() {
		t.Error("changing the path did not change the fingerprint")
	}
}
