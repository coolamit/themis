package ocr

import (
	"crypto/sha256"
	"encoding/hex"
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
func (c Comment) Fingerprint() string {
	h := sha256.Sum256([]byte(c.Path + "\x00" + NormalizeWS(c.ExistingCode) + "\x00" + c.Category))
	return hex.EncodeToString(h[:8])
}
