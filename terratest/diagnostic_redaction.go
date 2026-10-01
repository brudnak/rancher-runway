package test

import (
	"net/netip"
	"os"
	"regexp"
	"strings"
)

var diagnosticHostname = regexp.MustCompile(`\bip-(?:[0-9]{1,3}-){3}[0-9]{1,3}(?:\.[a-zA-Z0-9.-]+)?`)
var diagnosticAddress = regexp.MustCompile(`(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|(?:[0-9a-fA-F]{1,4}:){0,6}[0-9a-fA-F]{0,4}::(?:[0-9a-fA-F]{1,4}:){0,6}[0-9a-fA-F]{0,4}|(?:[0-9]{1,3}\.){3}[0-9]{1,3}`)

// Redact before logging so CI output and captured log files both omit node addresses.
// Leave local diagnostics intact, and never alter remote command return values.
func redactDiagnosticOutput(output string) string {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return output
	}
	output = diagnosticHostname.ReplaceAllString(output, "[REDACTED_HOST]")
	return diagnosticAddress.ReplaceAllStringFunc(output, func(candidate string) string {
		if _, err := netip.ParseAddr(candidate); err == nil {
			return "[REDACTED_IP]"
		}
		// A compressed address at the end of a TLS annotation can include its separator.
		if trimmed := strings.TrimRight(candidate, ":"); trimmed != candidate {
			if _, err := netip.ParseAddr(trimmed); err == nil {
				return "[REDACTED_IP]" + candidate[len(trimmed):]
			}
		}
		return candidate
	})
}
