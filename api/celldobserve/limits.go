// Package celldobserve contains the shared limits for the private observation protocol.
package celldobserve

import (
	"regexp"
	"strings"
)

const (
	MaxRecordBytes  = 8 << 10
	MaxBatchBytes   = 256 << 10
	MaxRecords      = 128
	MaxQueueBytes   = 2 << 20
	MaxQueueRecords = 2048
	MaxWorkloads    = 256
	MaxStateBytes   = 1 << 20
)

var secretField = regexp.MustCompile(`(?i)(authorization|cookie|password|secret|token|api[_-]?key|access[_-]?key)(["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;]+)`)
var bearer = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[a-zA-Z0-9._~+/=-]+`)
var query = regexp.MustCompile(`(https?://[^\s?"']+)\?[^\s"']+`)

// Sanitize removes known secrets and URL query values. App-authored free text is
// still application data: do not log secrets in the first place.
func Sanitize(s string, secrets ...string) string {
	if len(s) > MaxRecordBytes {
		s = s[:MaxRecordBytes-12] + " [truncated]"
	}
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	s = query.ReplaceAllString(s, "$1?[redacted]")
	s = secretField.ReplaceAllString(s, "${1}${2}[redacted]")
	s = bearer.ReplaceAllString(s, "$1 [redacted]")
	if len(s) > MaxRecordBytes {
		s = s[:MaxRecordBytes]
	}
	return strings.ToValidUTF8(s, "�")
}
