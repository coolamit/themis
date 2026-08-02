package ocr

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// NormalizeWS collapses every whitespace run to a single space and
// trims leading/trailing whitespace, so indentation and wrapping churn
// do not change a finding's identity. The suggestion guard uses the
// same normalization when comparing flagged code against file content.
func NormalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Fingerprint returns a 16-hex-char content fingerprint identifying
// this finding across review runs. It hashes what the finding is about
// (file, flagged code, category) and deliberately excludes the
// LLM-worded content and line numbers, so rewording and line drift
// between pushes map to the same fingerprint while a change to the
// flagged code itself produces a new one.
//
// A finding without a flagged snippet (existing_code is optional) falls
// back to its line range: otherwise every snippet-less finding on the
// same path+category would collapse to one identity and dedupe would
// silently drop all but the first. The trade-off is that such findings
// repost when their lines drift — acceptable for the rare case, since
// the LLM-worded content is too nondeterministic to hash instead.
func (c Comment) Fingerprint() string {
	identity := NormalizeWS(c.ExistingCode)
	if identity == "" {
		identity = fmt.Sprintf("L%d-%d", c.StartLine, c.EndLine)
	}
	h := sha256.Sum256([]byte(c.Path + "\x00" + identity + "\x00" + c.Category))
	return hex.EncodeToString(h[:8])
}
