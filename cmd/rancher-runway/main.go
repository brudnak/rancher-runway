package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
	harancher "github.com/brudnak/ha-rancher-rke2/terratest"
)

type lifecycleCommand struct {
	Description string
	TestName    string
	Timeout     string
	CountOne    bool
}

var lifecycleCommands = map[string]lifecycleCommand{
	"setup": {
		Description: "Create Rancher Runway infrastructure",
		TestName:    "TestHaSetup",
		Timeout:     "60m",
		CountOne:    true,
	},
	"wait-ready": {
		Description: "Wait until Rancher and rancher-webhook are healthy",
		TestName:    "TestHAWaitReady",
		Timeout:     "35m",
		CountOne:    true,
	},
	"panel": {
		Description: "Open the local Rancher Runway control panel",
		TestName:    "TestHAControlPanel",
		Timeout:     "0",
		CountOne:    true,
	},
	"cleanup": {
		Description: "Destroy Rancher Runway infrastructure",
		TestName:    "TestHACleanup",
		Timeout:     "30m",
		CountOne:    true,
	},
	"provision-downstream": {
		Description: "Provision Linode downstream clusters",
		TestName:    "TestHAProvisionLinodeDownstream",
		Timeout:     "20m",
		CountOne:    true,
	},
	"delete-downstream": {
		Description: "Delete Linode downstream clusters",
		TestName:    "TestHADeleteLinodeDownstream",
		Timeout:     "25m",
		CountOne:    true,
	},
	"upgrade": {
		Description: "Upgrade Rancher",
		TestName:    "TestHAUpgradeRancher",
		Timeout:     "45m",
		CountOne:    true,
	},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		printUsage()
		return nil
	case "version":
		fmt.Println(buildinfo.DisplayLine())
		return nil
	}

	if args[0] == "status" {
		return runStatus(args[1:])
	}

	command, ok := lifecycleCommands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoFlag := fs.String("repo", "", "repository root; defaults to RANCHER_RUNWAY_REPO or HA_RANCHER_REPO or walking up from the current directory")
	timeoutFlag := fs.String("timeout", command.Timeout, "go test timeout")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	repoRoot, err := resolveRepoRoot(*repoFlag)
	if err != nil {
		return err
	}

	if args[0] == "panel" {
		fmt.Printf("[rancher-runway] %s\n", command.Description)
		fmt.Printf("[rancher-runway] repo: %s\n", repoRoot)
		return harancher.RunHAControlPanel(repoRoot)
	}

	return runLifecycleTest(context.Background(), repoRoot, command, *timeoutFlag)
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoFlag := fs.String("repo", "", "repository root; defaults to RANCHER_RUNWAY_REPO or HA_RANCHER_REPO or walking up from the current directory")
	jsonFlag := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	repoRoot, err := resolveRepoRoot(*repoFlag)
	if err != nil {
		return err
	}

	status, err := harancher.InspectLocalWorkspace(repoRoot)
	if err != nil {
		return err
	}

	if *jsonFlag {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)
	}

	printStatus(status)
	return nil
}

func printStatus(status harancher.LocalWorkspaceStatus) {
	fmt.Printf("[rancher-runway] repo: %s\n", status.RepoRoot)
	fmt.Printf("[rancher-runway] config: %s\n", status.ConfigPath)
	fmt.Printf("[rancher-runway] configured instances: %d\n", status.TotalHAs)
	if status.Panel.Running {
		fmt.Printf("[rancher-runway] panel: running at %s (pid %d)\n", status.Panel.URL, status.Panel.PID)
	} else if status.Panel.URL != "" {
		fmt.Printf("[rancher-runway] panel: stale session at %s (pid %d)\n", status.Panel.URL, status.Panel.PID)
	} else {
		fmt.Printf("[rancher-runway] panel: stopped\n")
	}
	if status.Panel.LogPath != "" {
		fmt.Printf("[rancher-runway] launch log: %s\n", status.Panel.LogPath)
	}
	fmt.Printf("[rancher-runway] workspace: %s (%s)\n", status.Workspace.Mode, status.Workspace.SlotName)
	if status.Workspace.CurrentRun != nil {
		run := status.Workspace.CurrentRun
		fmt.Printf("[rancher-runway] current run: %s %s, backend %s\n", run.RunID, run.Status, run.TerraformBackend)
		if run.HAOutputRoot != "" {
			fmt.Printf("[rancher-runway] run deployment output: %s\n", run.HAOutputRoot)
		}
		if run.TerraformStatePath != "" {
			fmt.Printf("[rancher-runway] run Terraform state: %s\n", run.TerraformStatePath)
		}
	}
	fmt.Printf("[rancher-runway] preflight: %s (%s)\n", readyLabel(status.Preflight.Ready), status.Preflight.Summary)

	for _, check := range status.Preflight.Items {
		if check.Status == "ok" {
			continue
		}
		fmt.Printf("[rancher-runway]   %s: %s - %s\n", check.Name, check.Status, check.Detail)
	}

	fmt.Printf("[rancher-runway] clusters: %d\n", len(status.Clusters))
	for _, cluster := range status.Clusters {
		state := clusterStateLabel(cluster)
		detail := cluster.RancherURL
		if detail == "" {
			detail = cluster.Error
		}
		if detail != "" {
			fmt.Printf("[rancher-runway]   %s: %s (%s)\n", cluster.Name, state, detail)
			continue
		}
		fmt.Printf("[rancher-runway]   %s: %s\n", cluster.Name, state)
	}

	for _, name := range []string{"setup", "readiness", "cleanup"} {
		operation := status.Operations[name]
		if operation.Running {
			fmt.Printf("[rancher-runway] %s: running (%s)\n", name, operation.RunID)
			continue
		}
		if operation.Error != "" {
			fmt.Printf("[rancher-runway] %s: failed/stale (%s)\n", name, operation.Error)
			continue
		}
		if operation.FinishedAt != nil {
			fmt.Printf("[rancher-runway] %s: completed\n", name)
			continue
		}
		fmt.Printf("[rancher-runway] %s: idle\n", name)
	}
}

func readyLabel(ready bool) string {
	if ready {
		return "ready"
	}
	return "blocked"
}

func clusterStateLabel(cluster harancher.LocalWorkspaceCluster) string {
	switch {
	case cluster.Reachable:
		return "reachable"
	case cluster.Provisioning:
		return "provisioning"
	case cluster.Available:
		return "unavailable"
	default:
		return "missing"
	}
}

func runLifecycleTest(ctx context.Context, repoRoot string, command lifecycleCommand, timeout string) error {
	args := []string{"test", "-v", "-run", fmt.Sprintf("^%s$", command.TestName), "-timeout", timeout}
	if command.CountOne {
		args = append(args, "-count=1")
	}
	args = append(args, "./terratest")

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Printf("[rancher-runway] %s\n", command.Description)
	fmt.Printf("[rancher-runway] repo: %s\n", repoRoot)
	fmt.Printf("[rancher-runway] command: go %s\n", strings.Join(args, " "))

	started := time.Now()
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("[rancher-runway] %s failed after %s: %w", command.TestName, time.Since(started).Round(time.Second), err)
	}

	fmt.Printf("[rancher-runway] %s completed in %s\n", command.TestName, time.Since(started).Round(time.Second))
	return nil
}

func resolveRepoRoot(explicit string) (string, error) {
	candidates := []string{}
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if envRepo := strings.TrimSpace(os.Getenv("RANCHER_RUNWAY_REPO")); envRepo != "" {
		candidates = append(candidates, envRepo)
	}
	if envRepo := strings.TrimSpace(os.Getenv("HA_RANCHER_REPO")); envRepo != "" {
		candidates = append(candidates, envRepo)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}

	exePath, err := os.Executable()
	if err == nil {
		candidates = append(candidates, filepath.Dir(exePath))
	}

	for _, candidate := range candidates {
		repoRoot, err := walkToRepoRoot(candidate)
		if err == nil {
			return repoRoot, nil
		}
	}

	return "", errors.New("could not locate repository root; pass -repo /path/to/rancher-runway or set RANCHER_RUNWAY_REPO or HA_RANCHER_REPO")
}

func walkToRepoRoot(start string) (string, error) {
	if start == "" {
		return "", errors.New("empty start path")
	}

	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	current = filepath.Clean(current)

	for {
		goMod := filepath.Join(current, "go.mod")
		terratestDir := filepath.Join(current, "terratest")
		if fileExists(goMod) && dirExists(terratestDir) {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return "", fmt.Errorf("no repository root found from %s", start)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func printUsage() {
	fmt.Print(usageText())
}

func usageText() string {
	var b strings.Builder
	b.WriteString("Usage: rancher-runway <command> [flags]\n\n")
	b.WriteString("Commands:\n")
	for _, name := range []string{"status", "setup", "wait-ready", "panel", "cleanup", "provision-downstream", "delete-downstream", "upgrade"} {
		if name == "status" {
			fmt.Fprintf(&b, "  %-20s %s\n", name, "Show local preflight, run, and workspace status")
			continue
		}
		command := lifecycleCommands[name]
		fmt.Fprintf(&b, "  %-20s %s\n", name, command.Description)
	}
	b.WriteString("\nFlags:\n")
	b.WriteString("  -repo string      repository root; defaults to RANCHER_RUNWAY_REPO or HA_RANCHER_REPO or walking up from cwd\n")
	b.WriteString("  -timeout string   go test timeout for the selected lifecycle command\n")
	b.WriteString("  -json             print JSON for status\n")
	return b.String()
}
