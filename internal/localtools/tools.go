package localtools

import (
	"context"
	_ "embed"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"time"
)

func Resolve(command string) (string, error) {
	if command == "git" {
		return ResolveGit()
	}
	if strings.Contains(command, string(os.PathSeparator)) {
		if info, err := os.Stat(command); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return command, nil
		}
		return "", fmt.Errorf("%s not found", command)
	}
	for _, dir := range SearchDirs() {
		path := filepath.Join(dir, command)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return path, nil
		}
	}
	if path, err := exec.LookPath(command); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%s not found", command)
}

func ResolveGit() (string, error) {
	if path := XcrunGitPath(); path != "" {
		return path, nil
	}
	for _, path := range []string{
		"/Library/Developer/CommandLineTools/usr/bin/git",
		"/Applications/Xcode.app/Contents/Developer/usr/bin/git",
		"/usr/bin/git",
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("git not found; install or repair Xcode Command Line Tools")
}

func XcrunGitPath() string {
	xcrunPath := "/usr/bin/xcrun"
	if info, err := os.Stat(xcrunPath); err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, xcrunPath, "--find", "git").Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(output))
	if path == "" || strings.HasPrefix(path, "/opt/homebrew/") || strings.HasPrefix(path, "/usr/local/") {
		return ""
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
		return path
	}
	return ""
}

func Environment(extra []string) []string {
	env := os.Environ()
	pathValue := Path()
	found := false
	for i, value := range env {
		if strings.HasPrefix(value, "PATH=") {
			env[i] = "PATH=" + pathValue
			found = true
			break
		}
	}
	if !found {
		env = append(env, "PATH="+pathValue)
	}
	return append(env, extra...)
}

func Path() string {
	parts := []string{}
	parts = append(parts, SearchDirs()...)
	if current := strings.TrimSpace(os.Getenv("PATH")); current != "" {
		parts = append(parts, strings.Split(current, string(os.PathListSeparator))...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func SearchDirs() []string {
	return []string{
		"/opt/homebrew/opt/helm@3/bin",
		"/usr/local/opt/helm@3/bin",
		"/opt/homebrew/opt/terraform/bin",
		"/usr/local/opt/terraform/bin",
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/Library/Developer/CommandLineTools/usr/bin",
		"/Applications/Xcode.app/Contents/Developer/usr/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
		"/Applications/Docker.app/Contents/Resources/bin",
	}
}
