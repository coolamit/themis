package ocr

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeFileRound1(t *testing.T) {
	rep, err := DecodeFile("testdata/round1.json")
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if rep.Status != "success" {
		t.Errorf("status = %q, want success", rep.Status)
	}
	if rep.SessionID != "2139a98e-1963-46f7-b252-f0d2ebbc1f7b" {
		t.Errorf("session_id = %q", rep.SessionID)
	}

	wantSummary := &Summary{
		FilesReviewed:   3,
		Comments:        2,
		TotalTokens:     46157,
		InputTokens:     41378,
		OutputTokens:    4779,
		CacheReadTokens: 29117,
		Elapsed:         "53s",
	}
	if !reflect.DeepEqual(rep.Summary, wantSummary) {
		t.Errorf("summary = %+v, want %+v", rep.Summary, wantSummary)
	}

	wantTools := &ToolCalls{
		Total: 7,
		ByTool: map[string]int64{
			"code_comment":   1,
			"code_search":    1,
			"file_read":      3,
			"file_read_diff": 2,
		},
	}
	if !reflect.DeepEqual(rep.ToolCalls, wantTools) {
		t.Errorf("tool_calls = %+v, want %+v", rep.ToolCalls, wantTools)
	}

	if len(rep.Comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(rep.Comments))
	}

	c0 := rep.Comments[0]
	if c0.Path != "index.php" || c0.StartLine != 3 || c0.EndLine != 3 ||
		c0.Category != "bug" || c0.Severity != "high" {
		t.Errorf("comment 0 metadata = %+v", c0)
	}
	if c0.ExistingCode != "$code = $_GET['code'];" {
		t.Errorf("comment 0 existing_code = %q", c0.ExistingCode)
	}
	if !strings.HasPrefix(c0.Content, "`$_GET['code']` is accessed without verifying") {
		t.Errorf("comment 0 content = %q", c0.Content)
	}
	if !strings.HasPrefix(c0.SuggestionCode, "$code = $_GET['code'] ?? null;") {
		t.Errorf("comment 0 suggestion_code = %q", c0.SuggestionCode)
	}

	c1 := rep.Comments[1]
	if c1.Path != "index.php" || c1.StartLine != 5 || c1.EndLine != 9 ||
		c1.Category != "bug" || c1.Severity != "high" {
		t.Errorf("comment 1 metadata = %+v", c1)
	}
	wantExisting := "if ( 1 > $code ) {\n\techo 'Code is greater';\n} else {\n\techo 'Code is less';\n}"
	if c1.ExistingCode != wantExisting {
		t.Errorf("comment 1 existing_code = %q, want %q", c1.ExistingCode, wantExisting)
	}
}

func TestDecodeFileRound2(t *testing.T) {
	rep, err := DecodeFile("testdata/round2.json")
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	wantSummary := &Summary{
		FilesReviewed:   2,
		Comments:        1,
		TotalTokens:     29598,
		InputTokens:     27117,
		OutputTokens:    2481,
		CacheReadTokens: 14848,
		Elapsed:         "11s",
	}
	if !reflect.DeepEqual(rep.Summary, wantSummary) {
		t.Errorf("summary = %+v, want %+v", rep.Summary, wantSummary)
	}
	if len(rep.Comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(rep.Comments))
	}
	c := rep.Comments[0]
	if c.Path != "index.php" || c.StartLine != 18 || c.EndLine != 18 ||
		c.Category != "bug" || c.Severity != "critical" {
		t.Errorf("comment metadata = %+v", c)
	}
	if c.ExistingCode != "$isValidFruit = in_array($fruits, $fruit, true);" {
		t.Errorf("existing_code = %q", c.ExistingCode)
	}
	if c.SuggestionCode != "$isValidFruit = in_array($fruit, $fruits, true);" {
		t.Errorf("suggestion_code = %q", c.SuggestionCode)
	}
}

func TestDecodeCleanRun(t *testing.T) {
	rep, err := DecodeFile("testdata/clean.json")
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if rep.Message != "No comments generated. Looks good to me." {
		t.Errorf("message = %q", rep.Message)
	}
	if len(rep.Comments) != 0 {
		t.Errorf("got %d comments, want 0", len(rep.Comments))
	}
	if rep.ToolCalls == nil || rep.ToolCalls.Total != 0 {
		t.Errorf("tool_calls = %+v", rep.ToolCalls)
	}
	if !rep.ReviewRan() {
		t.Error("ReviewRan() = false on a clean success run")
	}
}

func TestDecodeNullComments(t *testing.T) {
	rep, err := DecodeFile("testdata/null_comments.json")
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if rep.Comments != nil {
		t.Errorf("comments = %v, want nil", rep.Comments)
	}
	if !rep.ReviewRan() {
		t.Error("ReviewRan() = false, want true")
	}
}

func TestReviewRanStatuses(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"success", true},
		{"completed_with_warnings", true},
		{"completed_with_errors", true},
		{"skipped", false},
		{"failed", false},
		{"exploded", false},
	}
	for _, tc := range cases {
		rep, err := Decode(strings.NewReader(`{"status": "` + tc.status + `"}`))
		if err != nil {
			t.Fatalf("status %q: %v", tc.status, err)
		}
		if got := rep.ReviewRan(); got != tc.want {
			t.Errorf("ReviewRan() with status %q = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestUntrustedStatusFixtures(t *testing.T) {
	for _, f := range []string{"testdata/status_failed.json", "testdata/status_skipped.json", "testdata/status_unknown.json"} {
		rep, err := DecodeFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if rep.ReviewRan() {
			t.Errorf("%s: ReviewRan() = true, want false", f)
		}
	}
}

func TestDecodeRejectsBadInput(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"not JSON", "this is not json"},
		{"missing status", `{}`},
		{"empty status", `{"status": ""}`},
		{"empty comment path", `{"status": "success", "comments": [{"path": "", "content": "x", "start_line": 1, "end_line": 1}]}`},
		{"zero start_line", `{"status": "success", "comments": [{"path": "a.go", "content": "x", "start_line": 0, "end_line": 1}]}`},
		{"negative start_line", `{"status": "success", "comments": [{"path": "a.go", "content": "x", "start_line": -3, "end_line": 1}]}`},
		{"end before start", `{"status": "success", "comments": [{"path": "a.go", "content": "x", "start_line": 5, "end_line": 4}]}`},
		{"empty content", `{"status": "success", "comments": [{"path": "a.go", "content": "", "start_line": 1, "end_line": 1}]}`},
		{"whitespace-only content", `{"status": "success", "comments": [{"path": "a.go", "content": "  \n ", "start_line": 1, "end_line": 1}]}`},
		{"trailing JSON document", `{"status": "success"} {"status": "failed"}`},
		{"trailing garbage", `{"status": "success"} review log line`},
	}
	for _, tc := range cases {
		_, err := Decode(strings.NewReader(tc.input))
		if err == nil {
			t.Errorf("%s: Decode accepted invalid input", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "ocr-version") {
			t.Errorf("%s: error %q does not suggest pinning ocr-version", tc.name, err)
		}
	}
}
