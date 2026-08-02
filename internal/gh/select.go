package gh

import (
	"sort"

	"github.com/coolamit/themis/internal/ocr"
)

// Selection is the outcome of comment budgeting: Inline findings become
// review comments, Overflow findings go to the summary comment(s).
// Nothing is ever dropped.
type Selection struct {
	Inline   []ocr.Comment
	Overflow []ocr.Comment
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	}
	return 4
}

// Select applies the comment budget. Findings are sorted critical →
// high → medium → low → unspecified (stable within a tier). Critical
// findings may exceed maxComments up to maxCritical; the remaining
// budget fills from high severity downward. Findings without usable
// line info cannot anchor to the diff, so they bypass selection and go
// straight to overflow regardless of severity or budget (fail-open).
func Select(findings []ocr.Comment, maxComments, maxCritical int) Selection {
	// The append-built slices never alias the caller's backing array, so
	// sorting in place below is safe.
	var lineless, sorted []ocr.Comment
	for _, c := range findings {
		if c.HasUsableLines() {
			sorted = append(sorted, c)
		} else {
			lineless = append(lineless, c)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return severityRank(sorted[i].Severity) < severityRank(sorted[j].Severity)
	})

	var criticals, rest []ocr.Comment
	for _, c := range sorted {
		if c.Severity == "critical" {
			criticals = append(criticals, c)
		} else {
			rest = append(rest, c)
		}
	}

	// Negative budgets would corrupt the slice bounds below; direct
	// library callers bypass the CLI's env validation.
	maxComments = max(maxComments, 0)
	maxCritical = max(maxCritical, 0)

	criticalsInline := min(len(criticals), maxCritical)
	inlineBudget := max(maxComments, criticalsInline)
	restInline := min(inlineBudget-criticalsInline, len(rest))

	var sel Selection
	sel.Inline = append(sel.Inline, criticals[:criticalsInline]...)
	sel.Inline = append(sel.Inline, rest[:restInline]...)
	sel.Overflow = append(sel.Overflow, criticals[criticalsInline:]...)
	sel.Overflow = append(sel.Overflow, rest[restInline:]...)
	sel.Overflow = append(sel.Overflow, lineless...)
	return sel
}
