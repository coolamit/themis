package ocr

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// pinHint is appended to decode/validation errors: incompatible output
// usually means OCR changed its schema, and pinning is the escape hatch.
const pinHint = " (if the installed OCR release changed its output schema, pin ocr-version to the last known-good release)"

// Report is the top-level JSON document produced by
// `ocr review --format json` (OCR v1.8.4).
type Report struct {
	Status    string     `json:"status"`
	TraceID   string     `json:"trace_id,omitempty"`
	Message   string     `json:"message,omitempty"`
	Summary   *Summary   `json:"summary,omitempty"`
	ToolCalls *ToolCalls `json:"tool_calls,omitempty"`
	Comments  []Comment  `json:"comments"`
	SessionID string     `json:"session_id,omitempty"`
}

// Summary holds run-level statistics. Elapsed is a human-formatted
// duration string (e.g. "53s"), not a number.
type Summary struct {
	FilesReviewed   int64  `json:"files_reviewed"`
	Comments        int64  `json:"comments"`
	TotalTokens     int64  `json:"total_tokens"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	CacheReadTokens int64  `json:"cache_read_tokens"`
	Elapsed         string `json:"elapsed"`
	BudgetExceeded  bool   `json:"budget_exceeded,omitempty"`
}

// ToolCalls reports how many agent tool invocations the review made.
type ToolCalls struct {
	Total  int64            `json:"total"`
	ByTool map[string]int64 `json:"by_tool"`
}

// Comment is a single review finding. StartLine and EndLine are
// absolute 1-based line numbers in the new (post-change) version of the
// file — the same semantics GitHub's review API expects.
type Comment struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	SuggestionCode string `json:"suggestion_code,omitempty"`
	ExistingCode   string `json:"existing_code,omitempty"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	Thinking       string `json:"thinking,omitempty"`
	Category       string `json:"category,omitempty"`
	Severity       string `json:"severity,omitempty"`
}

// Decode parses and validates an OCR JSON report. A nil comments array
// is accepted (upstream encodes a nil slice on one path) and left nil.
// Anything after the document besides whitespace is rejected — a report
// with logs or a second JSON value appended is not trustworthy.
func Decode(r io.Reader) (*Report, error) {
	var rep Report
	dec := json.NewDecoder(r)
	if err := dec.Decode(&rep); err != nil {
		return nil, fmt.Errorf("parsing OCR output: %w"+pinHint, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("invalid OCR output: trailing data after the JSON document" + pinHint)
	}
	if err := rep.validate(); err != nil {
		return nil, fmt.Errorf("invalid OCR output: %w"+pinHint, err)
	}
	return &rep, nil
}

// DecodeFile reads and decodes an OCR JSON report from disk.
func DecodeFile(path string) (*Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading OCR output: %w", err)
	}
	defer f.Close()
	return Decode(f)
}

func (r *Report) validate() error {
	if r.Status == "" {
		return fmt.Errorf("missing status")
	}
	for i, c := range r.Comments {
		if c.Path == "" {
			return fmt.Errorf("comment %d: empty path", i)
		}
		if strings.TrimSpace(c.Content) == "" {
			return fmt.Errorf("comment %d (%s): empty content", i, c.Path)
		}
		if c.StartLine <= 0 {
			return fmt.Errorf("comment %d (%s): start_line %d is not positive", i, c.Path, c.StartLine)
		}
		if c.EndLine < c.StartLine {
			return fmt.Errorf("comment %d (%s): end_line %d precedes start_line %d", i, c.Path, c.EndLine, c.StartLine)
		}
	}
	return nil
}

// ReviewRan reports whether the review actually completed, i.e. the
// comments (or their absence) are trustworthy and publishable. Any
// other status — skipped, failed, or unknown — means do not publish.
func (r *Report) ReviewRan() bool {
	switch r.Status {
	case "success", "completed_with_warnings", "completed_with_errors":
		return true
	}
	return false
}
