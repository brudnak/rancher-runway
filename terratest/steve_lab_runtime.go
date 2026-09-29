package test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func steveEndpointBinaryPath(record *steveLabRunRecord) string {
	name := "steve-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(record.RunDir, name)
}

// Build once before provisioning Kubernetes. Inspect the executable's CLI rather
// than assuming that a source package also implies a flag (SQL cache became
// unconditional in newer Steve releases, which removed --sql-cache).
func (p *localControlPanel) prepareSteveEndpoint(record *steveLabRunRecord) error {
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Building Steve before creating the cluster...")
	if err := p.runSteveLabCommandWithEnv(record, record.SourceDir, steveEndpointEnv(record), "go",
		"build", "-buildvcs=false", "-o", steveEndpointBinaryPath(record), "main.go"); err != nil {
		return fmt.Errorf("Steve build failed before cluster creation: %w; see build output above", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	help, err := inspectSteveCLI(ctx, record)
	if err != nil {
		return err
	}
	if err := configureSteveCapabilities(record, help); err != nil {
		return err
	}
	switch {
	case record.SQLCacheFlag:
		p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] SQL cache enabled using this release's --sql-cache flag")
	case record.SQLCache:
		p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] SQL cache is built in; no --sql-cache flag needed")
	default:
		p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] This Steve release does not include SQL cache")
	}
	return nil
}

func inspectSteveCLI(ctx context.Context, record *steveLabRunRecord) (string, error) {
	cmd := exec.CommandContext(ctx, steveEndpointBinaryPath(record), "--help")
	cmd.Dir = record.SourceDir
	cmd.Env = localToolEnv(steveEndpointEnv(record))
	help, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("could not inspect Steve's supported flags: %w: %s", err, strings.TrimSpace(string(help)))
	}
	return string(help), nil
}

func steveHelpHasFlag(help, name string) bool {
	pattern := `(?m)^\s*--` + regexp.QuoteMeta(name) + `(?:[\s,=]|$)`
	return regexp.MustCompile(pattern).MatchString(help)
}

func configureSteveCapabilities(record *steveLabRunRecord, help string) error {
	// A successful command with unrelated/empty output is not a verified CLI.
	for _, name := range []string{"kubeconfig", "https-listen-port", "http-listen-port"} {
		if !steveHelpHasFlag(help, name) {
			return fmt.Errorf("Steve %s does not advertise the required --%s option", record.SteveRef, name)
		}
	}
	record.SQLCacheFlag = steveHelpHasFlag(help, "sql-cache")
	info, err := os.Stat(filepath.Join(record.SourceDir, "pkg", "sqlcache"))
	record.SQLCache = record.SQLCacheFlag || (err == nil && info.IsDir())
	if record.EnableMetrics && !steveHelpHasFlag(help, "enable-metrics") {
		return fmt.Errorf("Steve %s does not support --enable-metrics; use the Standard profile for this release", record.SteveRef)
	}
	if record.EnableMetrics && record.MetricsUpdateIntervalSeconds > 0 && record.MetricsUpdateIntervalSeconds != 15 && !steveHelpHasFlag(help, "metrics-update-interval-seconds") {
		return fmt.Errorf("Steve %s does not support a custom metrics interval; use the default interval for this release", record.SteveRef)
	}
	return nil
}

// Do not keep an exited phase's PID while the orchestration moves to its next
// step. The equality check prevents an older child from clearing a newer PID.
func (p *localControlPanel) clearOperationPID(operation panelOperationName, pid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	op := p.operationLocked(operation)
	if op.PID != pid {
		return
	}
	op.PID = 0
	now := time.Now()
	op.UpdatedAt = &now
	p.persistOperationsLocked()
}

func steveLogTail(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	const limit = int64(16 * 1024)
	start := max(int64(0), info.Size()-limit)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return ""
	}
	text := string(data)
	if start > 0 {
		if end := strings.IndexByte(text, '\n'); end >= 0 {
			text = text[end+1:]
		}
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return strings.Join(lines, "\n")
}

func steveEndpointFailure(record *steveLabRunRecord, cause error) error {
	tail := steveLogTail(record.LogPath)
	if tail == "" {
		return fmt.Errorf("%w (runtime log: %s)", cause, record.LogPath)
	}
	lines := strings.Split(tail, "\n")
	detail := lines[0]
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.ToLower(lines[i])
		if strings.Contains(line, "level=fatal") || strings.Contains(line, "panic:") || strings.Contains(line, "error:") || strings.Contains(line, "flag provided but not defined") {
			detail = lines[i]
			break
		}
	}
	if len(detail) > 600 {
		detail = detail[:600] + "…"
	}
	return fmt.Errorf("%w: %s (runtime log: %s)", cause, detail, record.LogPath)
}
