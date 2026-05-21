package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
	harancher "github.com/brudnak/ha-rancher-rke2/terratest"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx      context.Context
	mu       sync.Mutex
	server   *harancher.ControlPanelServer
	handler  http.Handler
	url      string
	err      string
	repoRoot string
}

type DesktopPanelStatus struct {
	URL      string         `json:"url"`
	RepoRoot string         `json:"repoRoot"`
	Build    buildinfo.Info `json:"build"`
	Error    string         `json:"error,omitempty"`
}

var bundledRepoRoot string
var desktopEnvOnce sync.Once

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	server := a.server
	a.server = nil
	a.mu.Unlock()

	if server == nil || server.Reused() {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	server := a.server
	a.mu.Unlock()

	if server == nil || !server.LifecycleRunning() {
		return false
	}

	title, message := lifecycleCloseBlockedDialog(server.RunningOperation())
	_, _ = wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{
		Type:          wailsruntime.WarningDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{"OK"},
		DefaultButton: "OK",
	})
	return true
}

func (a *App) SystemTheme() string {
	switch runtime.GOOS {
	case "darwin":
		output, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").CombinedOutput()
		if err == nil && strings.EqualFold(strings.TrimSpace(string(output)), "dark") {
			return "dark"
		}
		return "light"
	default:
		return ""
	}
}

func lifecycleCloseBlockedDialog(operation string) (string, string) {
	operation = strings.TrimSpace(strings.ToLower(operation))
	switch operation {
	case "setup":
		return "Setup is still running", "Rancher Runway is creating a run slot or provisioning infrastructure. Keep the app open and wait for setup to finish before closing it."
	case "cleanup":
		return "Cleanup is still running", "Rancher Runway is cleaning up infrastructure. Keep the app open and wait for cleanup to finish before closing it."
	case "readiness":
		return "Readiness checks are still running", "Rancher Runway is checking cluster readiness. Keep the app open and wait for the checks to finish before closing it."
	default:
		return "Lifecycle operation is still running", "Rancher Runway is running setup, slot creation, readiness, or cleanup work. Keep the app open and wait for the operation to finish before closing it."
	}
}

func (a *App) PanelStatus() DesktopPanelStatus {
	a.mu.Lock()
	defer a.mu.Unlock()

	hydrateDesktopEnvironment()

	if a.url != "" {
		return DesktopPanelStatus{URL: a.url, RepoRoot: currentRepoHint(), Build: buildinfo.Current(), Error: a.err}
	}

	repoRoot, err := resolveDesktopRepoRoot()
	if err != nil {
		a.err = err.Error()
		return DesktopPanelStatus{RepoRoot: currentRepoHint(), Build: buildinfo.Current(), Error: a.err}
	}

	server, err := harancher.StartHAControlPanelServer(repoRoot, harancher.ControlPanelServerOptions{
		OpenBrowser:   false,
		ReuseExisting: true,
	})
	if err != nil {
		a.err = err.Error()
		return DesktopPanelStatus{RepoRoot: repoRoot, Build: buildinfo.Current(), Error: a.err}
	}

	a.server = server
	a.url = server.URL()
	a.err = ""
	a.repoRoot = repoRoot
	return DesktopPanelStatus{URL: a.url, RepoRoot: repoRoot, Build: buildinfo.Current()}
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, err := a.panelHandler()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	handler.ServeHTTP(w, r)
}

func (a *App) panelHandler() (http.Handler, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	hydrateDesktopEnvironment()
	if a.handler != nil {
		return a.handler, nil
	}

	repoRoot, err := resolveDesktopRepoRoot()
	if err != nil {
		a.err = err.Error()
		return nil, err
	}

	server, handler, err := harancher.StartHAControlPanelHandler(repoRoot)
	if err != nil {
		a.err = err.Error()
		return nil, err
	}
	a.server = server
	a.handler = handler
	a.url = server.URL()
	a.repoRoot = repoRoot
	return handler, nil
}

func resolveDesktopRepoRoot() (string, error) {
	candidates := []string{
		os.Getenv("RANCHER_RUNWAY_REPO"),
		os.Getenv("HA_RANCHER_REPO"),
		currentRepoHint(),
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executable))
	}

	for _, candidate := range candidates {
		root, err := walkToRepoRoot(candidate)
		if err == nil {
			return root, nil
		}
	}

	return "", errors.New("could not find the Rancher Runway repository; set RANCHER_RUNWAY_REPO or HA_RANCHER_REPO, or rebuild the Wails app from the checkout")
}

func currentRepoHint() string {
	if strings.TrimSpace(bundledRepoRoot) != "" {
		return strings.TrimSpace(bundledRepoRoot)
	}

	data, err := os.ReadFile("repo_hint.txt")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func walkToRepoRoot(start string) (string, error) {
	start = strings.TrimSpace(start)
	if start == "" {
		return "", errors.New("empty path")
	}

	info, err := os.Stat(start)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		start = filepath.Dir(start)
	}

	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}

	for {
		if isRepoRoot(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no repository root found above %s", start)
}

func isRepoRoot(dir string) bool {
	modulePath := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(modulePath)
	if err != nil {
		return false
	}
	if !strings.Contains(string(data), "module github.com/brudnak/ha-rancher-rke2") {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "terratest", "control_panel.go")); err != nil {
		return false
	}
	return true
}

func hydrateDesktopEnvironment() {
	desktopEnvOnce.Do(func() {
		mergeLoginShellEnvironment()
		hardenDesktopPath()
	})
}

func mergeLoginShellEnvironment() {
	if os.Getenv("RANCHER_RUNWAY_SKIP_SHELL_ENV") == "1" || os.Getenv("HA_RANCHER_SKIP_SHELL_ENV") == "1" {
		return
	}
	if _, err := os.Stat("/bin/zsh"); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, "/bin/zsh", "-lc", "env").Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		if shouldImportShellEnv(key) {
			_ = os.Setenv(key, value)
		}
	}
}

func shouldImportShellEnv(key string) bool {
	if key == "PATH" || key == "HOME" || key == "KUBECONFIG" || key == "GOPATH" || key == "GOBIN" {
		return true
	}

	prefixes := []string{
		"AWS_",
		"DOCKERHUB_",
		"LINODE_",
		"RANCHER_",
		"TF_",
		"TERRAFORM_",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func hardenDesktopPath() {
	home, _ := os.UserHomeDir()
	preferred := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/local/go/bin",
	}
	if home != "" {
		preferred = append(preferred, filepath.Join(home, "go", "bin"))
	}
	preferred = append(preferred, "/usr/bin", "/bin", "/usr/sbin", "/sbin")

	seen := map[string]bool{}
	var merged []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		merged = append(merged, path)
	}

	for _, path := range preferred {
		add(path)
	}
	for _, path := range filepath.SplitList(os.Getenv("PATH")) {
		add(path)
	}
	_ = os.Setenv("PATH", strings.Join(merged, string(os.PathListSeparator)))
}
