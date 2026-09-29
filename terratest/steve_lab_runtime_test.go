package test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

const steveBaseHelp = `GLOBAL OPTIONS:
   --kubeconfig value
   --https-listen-port value (default: 9443)
   --http-listen-port value (default: 9080)
`

func TestSteveEndpointSQLCacheCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name, help                          string
		packagePresent, wantCache, wantFlag bool
	}{
		{"modern built-in cache", steveBaseHelp + "   --enable-metrics (default: false)\n", true, true, false},
		{"legacy opt-in cache", steveBaseHelp + "   --sql-cache (default: false)\n", true, true, true},
		{"pre-cache release", steveBaseHelp, false, false, false},
		{"package alone does not imply a flag", steveBaseHelp + "DESCRIPTION:\n The old --sql-cache option was removed.\n   --sql-cache-other value\n", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := t.TempDir()
			if tc.packagePresent {
				if err := os.MkdirAll(filepath.Join(source, "pkg", "sqlcache"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			record := steveLabRunRecord{SourceDir: source, SteveRef: "test", SQLCache: true, SQLCacheFlag: true}
			if err := configureSteveCapabilities(&record, tc.help); err != nil {
				t.Fatal(err)
			}
			if record.SQLCache != tc.wantCache || record.SQLCacheFlag != tc.wantFlag {
				t.Fatalf("cache=%v flag=%v; want cache=%v flag=%v", record.SQLCache, record.SQLCacheFlag, tc.wantCache, tc.wantFlag)
			}
			if slices.Contains(steveEndpointArgs(&record), "--sql-cache") != tc.wantFlag {
				t.Fatalf("incorrect runtime flags: %v", steveEndpointArgs(&record))
			}
			if slices.Contains(steveEndpointArgs(&record), "run") {
				t.Fatal("runtime must use the prebuilt executable")
			}
		})
	}
}

func TestSteveEndpointCapabilitiesRejectUnverifiedCLIAndUnavailableMetrics(t *testing.T) {
	record := steveLabRunRecord{SteveRef: "test", SourceDir: t.TempDir()}
	if err := configureSteveCapabilities(&record, "unrelated output"); err == nil {
		t.Fatal("expected required-flag error")
	}
	record.EnableMetrics = true
	if err := configureSteveCapabilities(&record, steveBaseHelp); err == nil || !strings.Contains(err.Error(), "Standard profile") {
		t.Fatalf("expected metrics guidance, got %v", err)
	}
	record.MetricsUpdateIntervalSeconds = 7
	help := steveBaseHelp + "   --enable-metrics (default: false)\n"
	if err := configureSteveCapabilities(&record, help); err == nil || !strings.Contains(err.Error(), "default interval") {
		t.Fatalf("expected interval guidance, got %v", err)
	}
	if err := configureSteveCapabilities(&record, help+"   --metrics-update-interval-seconds value\n"); err != nil {
		t.Fatal(err)
	}
}

func TestSteveEndpointCLIProbeUsesBuiltExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	record := steveLabRunRecord{RunDir: dir, SourceDir: dir}
	script := "#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --help ] || exit 9\nprintf '%s\\n' '" + steveBaseHelp + "'\n"
	if err := os.WriteFile(steveEndpointBinaryPath(&record), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	help, err := inspectSteveCLI(ctx, &record)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureSteveCapabilities(&record, help); err != nil {
		t.Fatal(err)
	}
	if record.SQLCacheFlag {
		t.Fatal("probe must not invent the removed flag")
	}
}

func TestSteveEndpointFailureIncludesBoundedRuntimeCause(t *testing.T) {
	path := filepath.Join(t.TempDir(), "steve.log")
	text := strings.Repeat("old log line\n", 3000) + "Incorrect Usage: flag provided but not defined: -sql-cache\nGLOBAL OPTIONS:\n--kubeconfig value\ntime=now level=fatal msg=\"flag provided but not defined: -sql-cache\"\nexit status 1\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	tail := steveLogTail(path)
	if len(tail) > 16384 || len(strings.Split(tail, "\n")) > 40 {
		t.Fatalf("unbounded log tail: %d bytes", len(tail))
	}
	cause := errors.New("Steve process exited before 127.0.0.1:60166 opened")
	err := steveEndpointFailure(&steveLabRunRecord{LogPath: path}, cause)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "flag provided but not defined: -sql-cache") || !strings.Contains(err.Error(), path) {
		t.Fatalf("missing useful cause: %v", err)
	}
	missing := steveEndpointFailure(&steveLabRunRecord{LogPath: path + "-missing"}, cause)
	if !errors.Is(missing, cause) || !strings.Contains(missing.Error(), "runtime log:") {
		t.Fatalf("missing fallback: %v", missing)
	}
}

func TestSteveLabOperationRetainsOwnershipBetweenChildProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process liveness semantics")
	}
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())
	command := exec.Command("/bin/sh", "-c", "exit 0")
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	panel := &localControlPanel{operations: newPanelOperations()}
	op := panel.operations[panelOperationSteveLab]
	op.Running = true
	op.InProcess = true
	op.PID = command.Process.Pid
	op.RunID = "steve-test"
	if snapshot := panel.snapshotOperation(panelOperationSteveLab); !snapshot.Running || snapshot.Error != "" {
		t.Fatalf("exited phase incorrectly ended orchestration: %#v", snapshot)
	}
	panel.mu.Lock()
	anyRunning := panel.anyOperationRunningLocked()
	conflict := panel.conflictingOperationRunningLocked(panelOperationSteveLab)
	panel.mu.Unlock()
	if !anyRunning || !conflict {
		t.Fatal("a gap between phases must still block a second launch")
	}
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	var restored panelOperationState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.InProcess {
		t.Fatal("ownership must never survive a panel restart")
	}
	panel.operations[panelOperationSteveLab] = &restored
	if snapshot := panel.snapshotOperation(panelOperationSteveLab); snapshot.Running || snapshot.Error == "" {
		t.Fatalf("unowned exited process must still become stale: %#v", snapshot)
	}
}

func TestSteveLabCommandClearsRetiredPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	withTempWorkingDir(t, func(dir string) {
		t.Setenv("GITHUB_WORKSPACE", dir)
		panel := &localControlPanel{operations: newPanelOperations()}
		op := panel.operations[panelOperationSteveLab]
		op.Running = true
		op.InProcess = true
		record := steveLabRunRecord{RunID: "steve-test", RunDir: dir, SourceDir: dir}
		if err := panel.runSteveLabCommand(&record, dir, "/bin/sh", "-c", "exit 0"); err != nil {
			t.Fatal(err)
		}
		if op.PID != 0 || !op.Running {
			t.Fatalf("completed phase left stale ownership: %#v", op)
		}
		if err := panel.runSteveLabCommand(&record, dir, "/bin/sh", "-c", "exit 7"); err == nil {
			t.Fatal("expected child error")
		}
		if op.PID != 0 || !op.Running {
			t.Fatal("failed phase should clear the child PID before the caller finishes the operation")
		}
		op.PID = 321
		panel.clearOperationPID(panelOperationSteveLab, 123)
		if op.PID != 321 {
			t.Fatal("a completed old child cleared a newer PID")
		}
		panel.finishSteveLabOperation(errors.New("specific cause"))
		if op.InProcess || op.Running || op.PID != 0 {
			t.Fatal("completion must release orchestration ownership")
		}
	})
}

func TestSteveEndpointExitSurfacesRuntimeLogInActivity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	withTempWorkingDir(t, func(dir string) {
		t.Setenv("GITHUB_WORKSPACE", dir)
		panel := &localControlPanel{operations: newPanelOperations()}
		op := panel.operations[panelOperationSteveLab]
		op.Running = true
		op.InProcess = true
		record := steveLabRunRecord{RunID: "steve-test", RunDir: dir, SourceDir: dir, LogPath: filepath.Join(dir, "steve.log"), HTTPSPort: 0}
		script := "#!/bin/sh\nprintf '%s\\n' 'Incorrect Usage: flag provided but not defined: -sql-cache' >&2\nexit 1\n"
		if err := os.WriteFile(steveEndpointBinaryPath(&record), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		err := panel.startSteveEndpoint(&record)
		if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -sql-cache") {
			t.Fatalf("runtime failure hidden: %v", err)
		}
		if record.StevePID != 0 || op.PID != 0 {
			t.Fatal("exited startup left a PID behind")
		}
		output := strings.Join(panel.snapshotOperation(panelOperationSteveLab).Output, "\n")
		if !strings.Contains(output, "Startup log tail:") || !strings.Contains(output, "flag provided but not defined") {
			t.Fatalf("Activity lost runtime diagnosis: %s", output)
		}
	})
}
