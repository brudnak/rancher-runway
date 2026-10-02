package test

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func openExternalURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil || u.Host == "" {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https URLs can be opened")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}
	return nil
}

func (p *localControlPanel) resolveAllowedLocalPath(rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.repoRoot, path)
	}
	path = filepath.Clean(path)

	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("path is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink paths cannot be opened from the panel")
	}

	for _, root := range p.allowedLocalPathRoots() {
		if pathWithinRoot(root, path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("path is outside this checkout's local run artifacts")
}

func (p *localControlPanel) allowedLocalPathRoots() []string {
	roots := []string{p.repoRoot, p.testDir}
	if outputRoot, err := absoluteFromWorkingDir(automationOutputDir()); err == nil {
		roots = append(roots, outputRoot)
	}

	var result []string
	seen := map[string]bool{}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		if seen[absRoot] {
			continue
		}
		seen[absRoot] = true
		result = append(result, absRoot)
	}
	return result
}

func pathWithinRoot(root, path string) bool {
	root, rootErr := filepath.Abs(filepath.Clean(root))
	path, pathErr := filepath.Abs(filepath.Clean(path))
	if rootErr != nil || pathErr != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func openLocalPath(path string, reveal bool) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if reveal {
			cmd = exec.Command("open", "-R", path)
		} else {
			cmd = exec.Command("open", path)
		}
	case "windows":
		if reveal {
			cmd = exec.Command("explorer", "/select,"+path)
		} else {
			cmd = exec.Command("explorer", path)
		}
	default:
		openPath := path
		if reveal {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				openPath = filepath.Dir(path)
			}
		}
		cmd = exec.Command("xdg-open", openPath)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open local path: %w", err)
	}
	return nil
}
