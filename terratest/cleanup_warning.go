package test

import (
	"fmt"
	"log"
	"strings"
)

const cleanupManualWarningMarker = "MANUAL LINODE CLEANUP REQUIRED:"

type cleanupDestroyResult struct {
	Warning string
}

// runCleanupDestroyPhases keeps downstream cleanup best-effort while preserving
// management infrastructure cleanup as the authoritative success or failure.
func runCleanupDestroyPhases(cleanupDownstreams func() error, destroyManagement func() error, cleanupLocalArtifacts func()) (cleanupDestroyResult, error) {
	if cleanupDownstreams == nil {
		return cleanupDestroyResult{}, fmt.Errorf("downstream cleanup function must not be nil")
	}
	if destroyManagement == nil {
		return cleanupDestroyResult{}, fmt.Errorf("management cleanup function must not be nil")
	}
	if cleanupLocalArtifacts == nil {
		return cleanupDestroyResult{}, fmt.Errorf("local artifact cleanup function must not be nil")
	}

	result := cleanupDestroyResult{}
	if err := cleanupDownstreams(); err != nil {
		result.Warning = manualLinodeCleanupWarning(err)
		log.Print(cleanupWarningLogLine(result.Warning))
	}
	if err := destroyManagement(); err != nil {
		return result, err
	}
	cleanupLocalArtifacts()
	return result, nil
}

func manualLinodeCleanupWarning(err error) string {
	detail := "unknown downstream cleanup error"
	if err != nil {
		detail = compactCleanupWarningText(err.Error())
	}
	return compactCleanupWarningText("Automatic deletion of recorded Linode downstream resources failed. " +
		"AWS management cleanup will continue. Any Linode resources that remain after this operation must be removed manually; " +
		"if AWS cleanup also fails, fix management access and retry. Details: " + detail)
}

func cleanupWarningLogLine(warning string) string {
	return fmt.Sprintf("[cleanup] WARNING: %s %s", cleanupManualWarningMarker, compactCleanupWarningText(warning))
}

func cleanupWarningFromOutputLine(line string) string {
	markerIndex := strings.Index(line, cleanupManualWarningMarker)
	if markerIndex < 0 {
		return ""
	}
	return compactCleanupWarningText(line[markerIndex+len(cleanupManualWarningMarker):])
}

func appendPanelWarning(existing, warning string) string {
	existing = strings.TrimSpace(existing)
	warning = strings.TrimSpace(warning)
	if warning == "" {
		return existing
	}
	if existing == "" {
		return warning
	}
	for _, item := range strings.Split(existing, "\n") {
		if strings.TrimSpace(item) == warning {
			return existing
		}
	}
	return existing + "\n" + warning
}

func compactCleanupWarningText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maxWarningLength = 4000
	runes := []rune(value)
	if len(runes) > maxWarningLength {
		return string(runes[:maxWarningLength]) + "…"
	}
	return value
}
