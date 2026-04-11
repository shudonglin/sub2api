// Package logger provides token masking helpers for log fields.
//
// NOTE: For structural JSON/text redaction (key=value pairs, nested maps,
// GOCSPX-/AIza-prefixed keys), use the richer `internal/util/logredact`
// package. This file only exports the narrow "partial visibility" masking
// helpers that logredact intentionally doesn't provide — logredact replaces
// whole values with "***", whereas MaskToken keeps a short prefix/suffix so
// operators can still correlate masked tokens across logs.
package logger

import (
	"regexp"
)

// fill is the mask string used between kept prefix and suffix.
const fill = "***"

// MaskToken masks a secret-like string while preserving a short prefix and
// suffix for correlation:
//   - len >= 20: keep first 6 + fill + last 4
//   - len 12–19: keep first 4 + fill + last 2
//   - len < 12:  return "***" (too short to leak anything useful)
func MaskToken(s string) string {
	n := len(s)
	switch {
	case n >= 20:
		return s[:6] + fill + s[n-4:]
	case n >= 12:
		return s[:4] + fill + s[n-2:]
	default:
		return fill
	}
}

// apiKeyPattern matches strings that look like API keys (common prefixes + suffix).
var apiKeyPattern = regexp.MustCompile(`^(sk-|cr_|pk_|rk_|Bearer )[A-Za-z0-9\-_\.]{8,}$`)

// MaskAPIKey masks the input only when it matches a known API-key pattern;
// otherwise it returns s unchanged. Use this when you don't know whether a
// field holds a key.
func MaskAPIKey(s string) string {
	if apiKeyPattern.MatchString(s) {
		return MaskToken(s)
	}
	return s
}
