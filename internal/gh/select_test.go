package gh

import (
	"fmt"
	"testing"

	"github.com/coolamit/themis/internal/ocr"
)

func makeFindings(counts map[string]int) []ocr.Comment {
	// Insertion order deliberately interleaves severities to exercise sorting.
	var out []ocr.Comment
	order := []string{"low", "critical", "medium", "high", ""}
	remaining := 0
	for _, n := range counts {
		remaining += n
	}
	i := 0
	for remaining > 0 {
		sev := order[i%len(order)]
		i++
		if counts[sev] == 0 {
			continue
		}
		counts[sev]--
		remaining--
		out = append(out, ocr.Comment{
			Path:      fmt.Sprintf("file%d.go", len(out)),
			Content:   "finding",
			StartLine: len(out) + 1,
			EndLine:   len(out) + 1,
			Severity:  sev,
		})
	}
	return out
}

func countSeverity(cs []ocr.Comment, severity string) int {
	n := 0
	for _, c := range cs {
		if c.Severity == severity {
			n++
		}
	}
	return n
}

// Worked example from the spec: 32 critical with max-comments 15 → all
// 32 inline.
func TestSelectCriticalsExceedBudget(t *testing.T) {
	sel := Select(makeFindings(map[string]int{"critical": 32}), 15, 50)
	if len(sel.Inline) != 32 {
		t.Errorf("inline = %d, want 32", len(sel.Inline))
	}
	if len(sel.Overflow) != 0 {
		t.Errorf("overflow = %d, want 0", len(sel.Overflow))
	}
}

// Worked example from the spec: 10 critical + 10 high with max-comments
// 15 → 10 critical + 5 high inline, 5 high to overflow.
func TestSelectMixedSeverities(t *testing.T) {
	sel := Select(makeFindings(map[string]int{"critical": 10, "high": 10}), 15, 50)
	if len(sel.Inline) != 15 {
		t.Fatalf("inline = %d, want 15", len(sel.Inline))
	}
	if got := countSeverity(sel.Inline, "critical"); got != 10 {
		t.Errorf("inline criticals = %d, want 10", got)
	}
	if got := countSeverity(sel.Inline, "high"); got != 5 {
		t.Errorf("inline highs = %d, want 5", got)
	}
	if len(sel.Overflow) != 5 || countSeverity(sel.Overflow, "high") != 5 {
		t.Errorf("overflow = %d highs of %d, want 5 of 5", countSeverity(sel.Overflow, "high"), len(sel.Overflow))
	}
}

func TestSelectCriticalCap(t *testing.T) {
	sel := Select(makeFindings(map[string]int{"critical": 60, "high": 20}), 25, 50)
	if got := countSeverity(sel.Inline, "critical"); got != 50 {
		t.Errorf("inline criticals = %d, want 50 (capped)", got)
	}
	if got := countSeverity(sel.Overflow, "critical"); got != 10 {
		t.Errorf("overflow criticals = %d, want 10", got)
	}
	if got := countSeverity(sel.Inline, "high"); got != 0 {
		t.Errorf("inline highs = %d, want 0 (budget consumed by criticals)", got)
	}
}

func TestSelectSeverityOrdering(t *testing.T) {
	findings := makeFindings(map[string]int{"critical": 1, "high": 1, "medium": 1, "low": 1, "": 1})
	sel := Select(findings, 25, 50)
	if len(sel.Inline) != 5 {
		t.Fatalf("inline = %d, want 5", len(sel.Inline))
	}
	want := []string{"critical", "high", "medium", "low", ""}
	for i, sev := range want {
		if sel.Inline[i].Severity != sev {
			t.Errorf("inline[%d] severity = %q, want %q", i, sel.Inline[i].Severity, sev)
		}
	}
}

func TestSelectStableWithinTier(t *testing.T) {
	findings := []ocr.Comment{
		{Path: "a.go", StartLine: 1, EndLine: 1, Severity: "high"},
		{Path: "b.go", StartLine: 1, EndLine: 1, Severity: "high"},
		{Path: "c.go", StartLine: 1, EndLine: 1, Severity: "high"},
	}
	sel := Select(findings, 25, 50)
	for i, path := range []string{"a.go", "b.go", "c.go"} {
		if sel.Inline[i].Path != path {
			t.Errorf("inline[%d] = %s, want %s (stable order)", i, sel.Inline[i].Path, path)
		}
	}
}

func TestSelectUnderBudget(t *testing.T) {
	sel := Select(makeFindings(map[string]int{"medium": 3}), 25, 50)
	if len(sel.Inline) != 3 || len(sel.Overflow) != 0 {
		t.Errorf("inline=%d overflow=%d, want 3/0", len(sel.Inline), len(sel.Overflow))
	}
}

func TestSelectEmpty(t *testing.T) {
	sel := Select(nil, 25, 50)
	if len(sel.Inline) != 0 || len(sel.Overflow) != 0 {
		t.Errorf("empty input produced inline=%d overflow=%d", len(sel.Inline), len(sel.Overflow))
	}
}
