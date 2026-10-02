package test

import (
	"fmt"
	"strings"
)

func parseHelmCommandFields(command string) ([]string, error) {
	command = strings.ReplaceAll(command, "\\\r\n", " ")
	command = strings.ReplaceAll(command, "\\\n", " ")
	var fields []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	hadChars := false

	flush := func() {
		if hadChars {
			fields = append(fields, current.String())
			current.Reset()
			hadChars = false
		}
	}

	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			hadChars = true
			escaped = false
			continue
		}
		switch {
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			hadChars = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			hadChars = true
		case (r == ' ' || r == '\t' || r == '\n' || r == '\r') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
			hadChars = true
		}
	}
	if escaped {
		current.WriteRune('\\')
		hadChars = true
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	flush()
	return fields, nil
}

func isShellControlField(field string) bool {
	switch field {
	case ";", "&&", "||", "|", ">", ">>", "<":
		return true
	default:
		return strings.Contains(field, "\x00")
	}
}

func helmFlagConsumesValue(flag string) bool {
	if strings.Contains(flag, "=") {
		return false
	}
	switch flag {
	case "-n", "--namespace", "-f", "--values", "--version", "--set", "--set-string", "--set-literal", "--set-file", "--set-json", "--timeout", "--kube-version", "--kubeconfig", "--registry-config", "--repository-config", "--repository-cache", "--username", "--password":
		return true
	default:
		return false
	}
}

func firstNLines(value string, maxLines int) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n... (%d more lines)", len(lines)-maxLines)
}

func rancherHelmCommandUsesExternalTLS(helmCommand string) bool {
	fields := strings.Fields(helmCommand)
	for i, field := range fields {
		field = cleanHelmCommandField(field)
		switch {
		case field == "--set" || field == "--set-string":
			if i+1 < len(fields) && isExternalTLSHelmSet(cleanHelmCommandField(fields[i+1])) {
				return true
			}
		case strings.HasPrefix(field, "--set="):
			if isExternalTLSHelmSet(strings.TrimPrefix(field, "--set=")) {
				return true
			}
		case strings.HasPrefix(field, "--set-string="):
			if isExternalTLSHelmSet(strings.TrimPrefix(field, "--set-string=")) {
				return true
			}
		}
	}
	return false
}

func cleanHelmCommandField(field string) string {
	field = strings.TrimSpace(field)
	field = strings.TrimSuffix(field, `\`)
	return strings.Trim(strings.TrimSpace(field), `"'`)
}

func isExternalTLSHelmSet(value string) bool {
	value = cleanHelmCommandField(value)
	return value == "tls=external" || strings.HasPrefix(value, "tls=external,")
}
