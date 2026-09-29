package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const cacheLabHelperURL = "https://github.com/brudnak/vai-vacuum/releases/download/v1.0.0-beta/vai-vacuum"
const cacheLabHelperSHA = "b853a594b8fbb33ef1be030664853bf1346a3443c40570cbb6daeb5354a1d8e6"

type cacheLabCommandRunner func(context.Context, []string, io.Reader, io.Writer) error

// Both output channels are bounded by their callers. Never include command args
// in errors: ephemeral kubeconfig paths and tokens must not end up in logs.
func cacheLabRunCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	bin, err := resolveLocalToolPath("kubectl")
	if err != nil {
		return fmt.Errorf("kubectl is required for live capture; install it and reopen Runway")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = localToolEnv(nil)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	stderr := &cacheLabBuffer{max: 12000}
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("Kubernetes request failed: %s", cacheLabText(stderr.String(), 1200))
	}
	return nil
}

type cacheLabBuffer struct {
	bytes.Buffer
	max int
}

func (b *cacheLabBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("response exceeds the size limit")
	}
	return b.Buffer.Write(p)
}
func cacheLabURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("enter an http(s) Rancher URL without embedded credentials, query parameters, or a fragment")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/"), nil
}
func cacheLabSourceLabel(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host + strings.TrimSuffix(strings.Split(u.Path, "/k8s/clusters/")[0], "/")
}
func cacheLabKeychain(action, id, secret string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("saved tokens require macOS Keychain; use a kubeconfig or session-only token")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args := []string{"find-generic-password", "-s", "com.rancher-runway.cache-lab", "-a", id, "-w"}
	if action == "delete" {
		args = []string{"delete-generic-password", "-s", "com.rancher-runway.cache-lab", "-a", id}
	}
	if action == "write" {
		// security's interactive input avoids exposing the password in process argv.
		quote := func(v string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"` }
		if strings.ContainsAny(secret, "\r\n\x00") {
			return "", fmt.Errorf("invalid token")
		}
		cmd := exec.CommandContext(ctx, "/usr/bin/security", "-i")
		cmd.Stdin = strings.NewReader("add-generic-password -U -s com.rancher-runway.cache-lab -a " + quote(id) + " -w " + quote(secret) + "\n")
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Run(); err != nil || strings.Contains(output.String(), "SecKeychain") || strings.Contains(output.String(), "error") {
			return "", fmt.Errorf("could not save token in macOS Keychain")
		}
		saved, err := cacheLabKeychain("read", id, "")
		if err != nil || saved != secret {
			return "", fmt.Errorf("macOS Keychain did not save the token")
		}
		return "", nil
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	b, err := cmd.Output()
	if err != nil {
		if action == "delete" {
			if code, ok := err.(*exec.ExitError); ok && code.ExitCode() == 44 {
				return "", nil
			}
		}
		return "", fmt.Errorf("macOS Keychain credential is unavailable")
	}
	return strings.TrimSpace(string(b)), nil
}
func (s *cacheLabService) saveWorkspace(ctx context.Context, req cacheLabRequest) (any, error) {
	profile := req.Profile
	profile.Name = cacheLabText(profile.Name, 120)
	profile.Notes = cacheLabText(profile.Notes, 8000)
	if profile.Kind != "rancher" && profile.Kind != "kubeconfig" && profile.Kind != "import" && profile.Kind != "steve" {
		return nil, fmt.Errorf("choose Rancher URL, kubeconfig, or local files")
	}
	if profile.Namespace == "" {
		profile.Namespace = "cattle-system"
	}
	if profile.Container == "" {
		profile.Container = "rancher"
	}
	if profile.Cluster == "" {
		profile.Cluster = "local"
	}
	if strings.ContainsAny(profile.Namespace+profile.Cluster+profile.Container, "\x00\r\n") {
		return nil, fmt.Errorf("invalid connection field")
	}
	var err error
	if profile.Kind == "rancher" {
		profile.URL, err = cacheLabURL(profile.URL)
		if err != nil {
			return nil, err
		}
	}
	if profile.Kind == "kubeconfig" {
		path := strings.TrimSpace(profile.Kubeconfig)
		if strings.HasPrefix(path, "~/") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[2:])
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("use the full path to your kubeconfig")
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("kubeconfig file is unavailable")
		}
		profile.Kubeconfig = path
		configCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		args := []string{"--kubeconfig", path}
		if profile.Context != "" {
			args = append(args, "--context", profile.Context)
		}
		args = append(args, "config", "view", "--minify", "-o", "json")
		out := &cacheLabBuffer{max: 2 << 20}
		if err = s.runner(configCtx, args, nil, out); err != nil {
			return nil, err
		}
		var cfg struct {
			Clusters []struct {
				Cluster struct {
					Server string `json:"server"`
				} `json:"cluster"`
			} `json:"clusters"`
			Current string `json:"current-context"`
		}
		if err = json.Unmarshal(out.Bytes(), &cfg); err != nil || len(cfg.Clusters) != 1 {
			return nil, fmt.Errorf("select a kubeconfig context with one cluster")
		}
		profile.URL, err = cacheLabURL(cfg.Clusters[0].Cluster.Server)
		if err != nil {
			return nil, err
		}
		profile.Context = cfg.Current
		profile.RememberToken = false
	}
	if profile.Kind == "import" {
		profile.URL = "Local SQLite files"
		profile.RememberToken = false
	}
	if profile.Name == "" {
		profile.Name = cacheLabSourceLabel(profile.URL)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.library.Job.Running && s.library.Job.Workspace == profile.ID {
		return nil, fmt.Errorf("wait for capture before changing this connection")
	}
	var old *cacheLabWorkspace
	if profile.ID != "" {
		old, err = s.workspaceLocked(profile.ID)
		if err != nil {
			return nil, err
		}
	}
	if profile.Kind == "steve" {
		if old == nil || old.Kind != "steve" {
			return nil, fmt.Errorf("Steve workspaces are created by Steve Lab")
		}
		profile.URL = old.URL
		profile.RememberToken = false
	}
	if old != nil && old.Kind != profile.Kind {
		return nil, fmt.Errorf("create another workspace to change the connection type")
	}
	if old != nil && old.URL != profile.URL {
		for _, snapshot := range s.library.Snapshots {
			if snapshot.Workspace == old.ID {
				return nil, fmt.Errorf("this workspace already has snapshots; create a new workspace for a different source URL")
			}
		}
	}
	// Never merge by token or display name: two contexts may use the same proxy.
	if old == nil {
		if len(s.library.Workspaces) >= 100 {
			return nil, fmt.Errorf("workspace limit reached")
		}
		profile.ID = cacheLabID()
		profile.CreatedAt = time.Now().UTC()
		profile.Folders = []string{}
		profile.Queries = []cacheLabSavedQuery{}
		profile.View.Mode = "explore"
	} else {
		profile.CreatedAt = old.CreatedAt
		profile.Folders = old.Folders
		profile.Queries = old.Queries
		profile.View = old.View
	}
	token := strings.TrimSpace(req.Token)
	if token == "" && old != nil && old.URL == profile.URL && old.Kind == profile.Kind {
		token = s.tokens[profile.ID]
	}
	if strings.ContainsAny(token, "\r\n\x00") {
		return nil, fmt.Errorf("invalid token")
	}
	if profile.Kind == "rancher" && token == "" {
		return nil, fmt.Errorf("enter an API token to connect")
	}
	if profile.Kind == "rancher" && profile.RememberToken {
		if _, err = cacheLabKeychain("write", profile.ID, token); err != nil {
			return nil, err
		}
	} else if old != nil && old.RememberToken {
		if _, err = cacheLabKeychain("delete", profile.ID, ""); err != nil {
			return nil, err
		}
	}
	if profile.Kind == "rancher" {
		s.tokens[profile.ID] = token
	}
	profile.Connected = true
	if old != nil {
		*old = profile
	} else {
		s.library.Workspaces = append(s.library.Workspaces, profile)
	}
	s.library.Active = profile.ID
	if err = s.persistLocked(); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(profile)
	var response cacheLabWorkspace
	_ = json.Unmarshal(encoded, &response)
	return response, nil
}
func (s *cacheLabService) connection(id string) (cacheLabWorkspace, []string, func(), error) {
	s.mu.Lock()
	w, err := s.workspaceLocked(id)
	if err != nil {
		s.mu.Unlock()
		return cacheLabWorkspace{}, nil, func() {}, err
	}
	profile := *w
	token := s.tokens[id]
	s.mu.Unlock()
	if profile.Kind == "kubeconfig" {
		args := []string{"--kubeconfig", profile.Kubeconfig}
		if profile.Context != "" {
			args = append(args, "--context", profile.Context)
		}
		return profile, args, func() {}, nil
	}
	if profile.Kind != "rancher" {
		return profile, nil, func() {}, fmt.Errorf("this workspace accepts local snapshots; choose a Rancher connection to capture a pod")
	}
	if token == "" {
		return profile, nil, func() {}, fmt.Errorf("reconnect with an API token; the saved snapshots are available offline")
	}
	server := strings.TrimRight(profile.URL, "/") + "/k8s/clusters/" + url.PathEscape(profile.Cluster)
	cluster := map[string]any{"server": server}
	if profile.Insecure {
		cluster["insecure-skip-tls-verify"] = true
	} else if profile.CAPEM != "" {
		cluster["certificate-authority-data"] = base64.StdEncoding.EncodeToString([]byte(profile.CAPEM))
	}
	cfg := map[string]any{"apiVersion": "v1", "kind": "Config", "current-context": "capture", "clusters": []any{map[string]any{"name": "capture", "cluster": cluster}}, "users": []any{map[string]any{"name": "capture", "user": map[string]string{"token": token}}}, "contexts": []any{map[string]any{"name": "capture", "context": map[string]string{"cluster": "capture", "user": "capture"}}}}
	data, _ := json.Marshal(cfg)
	dir, err := os.MkdirTemp("", "runway-cache-auth-")
	if err != nil {
		return profile, nil, func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	path := filepath.Join(dir, "config")
	if err = os.WriteFile(path, data, 0600); err != nil {
		cleanup()
		return profile, nil, func() {}, err
	}
	return profile, []string{"--kubeconfig", path}, cleanup, nil
}

type cacheLabPod struct {
	Name      string `json:"name"`
	UID       string `json:"uid"`
	Container string `json:"container"`
	Image     string `json:"image"`
	Ready     bool   `json:"ready"`
	Restarts  int    `json:"restarts"`
}

func (s *cacheLabService) pods(ctx context.Context, profile cacheLabWorkspace, args []string) ([]cacheLabPod, error) {
	if profile.Kind == "kubeconfig" {
		output := &cacheLabBuffer{max: 2 << 20}
		if err := s.runner(ctx, append(append([]string{}, args...), "config", "view", "--minify", "-o", "json"), nil, output); err != nil {
			return nil, err
		}
		var current struct {
			Clusters []struct {
				Cluster struct {
					Server string `json:"server"`
				} `json:"cluster"`
			} `json:"clusters"`
		}
		if err := json.Unmarshal(output.Bytes(), &current); err != nil || len(current.Clusters) != 1 {
			return nil, fmt.Errorf("could not verify kubeconfig source")
		}
		source, err := cacheLabURL(current.Clusters[0].Cluster.Server)
		if err != nil || source != profile.URL {
			return nil, fmt.Errorf("this kubeconfig now points to a different server; create a workspace for the new source before capturing")
		}
	}

	out := &cacheLabBuffer{max: 4 << 20}
	args = append(append([]string{}, args...), "--request-timeout=20s", "get", "pods", "-n", profile.Namespace, "-o", "json")
	if err := s.runner(ctx, args, nil, out); err != nil {
		return nil, err
	}
	var result struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
				UID  string `json:"uid"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Name  string `json:"name"`
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					Name         string `json:"name"`
					Ready        bool   `json:"ready"`
					RestartCount int    `json:"restartCount"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("could not read Rancher pods")
	}
	pods := []cacheLabPod{}
	for _, p := range result.Items {
		if p.Status.Phase != "Running" {
			continue
		}
		for _, c := range p.Spec.Containers {
			if c.Name != profile.Container {
				continue
			}
			pod := cacheLabPod{Name: p.Metadata.Name, UID: p.Metadata.UID, Container: c.Name, Image: c.Image}
			for _, status := range p.Status.ContainerStatuses {
				if status.Name == c.Name {
					pod.Ready = status.Ready
					pod.Restarts = status.RestartCount
				}
			}
			pods = append(pods, pod)
		}
	}
	return pods, nil
}
func (s *cacheLabService) discover(ctx context.Context, id string) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	profile, args, cleanup, err := s.connection(id)
	defer cleanup()
	if err != nil {
		return nil, err
	}
	pods, err := s.pods(ctx, profile, args)
	if err != nil {
		return nil, err
	}
	return map[string]any{"pods": pods, "namespace": profile.Namespace}, nil
}
func (s *cacheLabService) helper(ctx context.Context, profile cacheLabWorkspace, arch string) (string, error) {
	if profile.HelperPath != "" {
		f, err := elf.Open(profile.HelperPath)
		if err != nil {
			return "", fmt.Errorf("helper must be a Linux vai-vacuum ELF binary")
		}
		defer f.Close()
		expected := elf.EM_X86_64
		if arch == "aarch64" || arch == "arm64" {
			expected = elf.EM_AARCH64
		}
		if f.Machine != expected {
			return "", fmt.Errorf("helper architecture does not match the Rancher pod")
		}
		return profile.HelperPath, nil
	}
	if arch != "x86_64" && arch != "amd64" {
		return "", fmt.Errorf("the published vai-vacuum release is Linux amd64; set a Linux %s build in Connection → Advanced → Helper binary", arch)
	}
	path := filepath.Join(s.root, "vai-vacuum-v1.0.0-beta")
	verify := func() bool {
		f, err := os.Open(path)
		if err != nil {
			return false
		}
		defer f.Close()
		h := sha256.New()
		_, err = io.Copy(h, f)
		return err == nil && hex.EncodeToString(h.Sum(nil)) == cacheLabHelperSHA
	}
	if verify() {
		return path, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cacheLabHelperURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 90 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not download vai-vacuum: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("vai-vacuum download returned HTTP %d", response.StatusCode)
	}
	f, err := os.CreateTemp(s.root, ".helper-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(response.Body, 32<<20))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != cacheLabHelperSHA {
		return "", fmt.Errorf("vai-vacuum checksum mismatch; helper was not executed")
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
func (s *cacheLabService) startCapture(req cacheLabRequest) (any, error) {
	s.mu.Lock()
	w, err := s.workspaceLocked(req.Workspace)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	podName := w.Pod
	if req.Profile.Pod != "" {
		podName = req.Profile.Pod
	}
	kind := w.Kind
	s.mu.Unlock()
	if kind != "rancher" && kind != "kubeconfig" {
		return nil, fmt.Errorf("choose a live connection to capture")
	}
	if podName == "" {
		return nil, fmt.Errorf("discover and choose a Rancher pod first")
	}
	ctx, id, err := s.beginJob(req.Workspace, "Connecting to Rancher")
	if err != nil {
		return nil, err
	}
	req.Profile.Pod = podName
	s.mu.Lock()
	w, _ = s.workspaceLocked(req.Workspace)
	w.Pod = podName
	err = s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		s.finishJob(cacheLabSnapshot{}, err)
		return nil, err
	}
	go func() { record, err := s.capture(ctx, req, id); s.finishJob(record, cacheLabContextError(err)) }()
	return map[string]string{"job": id}, nil
}
func (s *cacheLabService) capture(ctx context.Context, req cacheLabRequest, id string) (cacheLabSnapshot, error) {
	var record cacheLabSnapshot
	profile, args, cleanup, err := s.connection(req.Workspace)
	if req.Profile.Pod != "" {
		profile.Pod = req.Profile.Pod
	}
	defer cleanup()
	if err != nil {
		return record, err
	}
	pods, err := s.pods(ctx, profile, args)
	if err != nil {
		return record, err
	}
	var selected *cacheLabPod
	for _, pod := range pods {
		if pod.Name == profile.Pod {
			copy := pod
			selected = &copy
			break
		}
	}
	if selected == nil {
		return record, fmt.Errorf("selected pod is no longer running; discover pods again")
	}
	execArgs := append(append([]string{}, args...), "exec", "-n", profile.Namespace, selected.Name, "-c", selected.Container)
	out := &cacheLabBuffer{max: 1000}
	if err = s.runner(ctx, append(append([]string{}, execArgs...), "--", "uname", "-m"), nil, out); err != nil {
		return record, err
	}
	s.stage("Verifying vai-vacuum helper")
	helper, err := s.helper(ctx, profile, strings.TrimSpace(out.String()))
	if err != nil {
		return record, err
	}
	remote := "/tmp/runway-vai-" + id
	// shell variables come from positional arguments. No URL, pod name, token, or
	// user-supplied value is interpolated into executable shell text.
	stageArgs := append(append([]string{}, execArgs...), "-i", "--", "sh", "-c", `umask 077; set -e; test ! -e "$1"; cat > "$1"; chmod 700 "$1"`, "runway-stage", remote)
	f, err := os.Open(helper)
	if err != nil {
		return record, err
	}
	s.stage("Transferring helper to selected pod")
	err = s.runner(ctx, stageArgs, f, io.Discard)
	f.Close()
	// Cleanup is attempted even on cancellation or a partially completed upload.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.runner(cleanupCtx, append(append([]string{}, execArgs...), "--", "rm", "-f", remote), nil, io.Discard)
	}()
	if err != nil {
		return record, err
	}
	path := filepath.Join(s.root, profile.ID, id+".partial")
	defer os.Remove(path)
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return record, err
	}
	// vai-vacuum uses a fixed temporary snapshot. Serialize participating clients
	// and refuse a pre-existing snapshot instead of removing someone else's file.
	script := `set -eu
umask 077
lock=/tmp/runway-vai-vacuum.lock
if ! mkdir "$lock" 2>/dev/null; then echo 'Another capture owns the pod lock. Wait for it to finish.' >&2; exit 1; fi
child= watchdog= owns_snapshot=
cleanup() {
 trap - EXIT HUP INT TERM
 test -z "$child" || kill "$child" 2>/dev/null || true
 test -z "$watchdog" || kill "$watchdog" 2>/dev/null || true
 test -z "$owns_snapshot" || rm -f /tmp/vai-snapshot.db
 rm -f "$1"
 rmdir "$lock" 2>/dev/null || true
}
trap 'cleanup "$1"' EXIT
trap 'exit 124' HUP INT TERM
if test -e /tmp/vai-snapshot.db; then echo 'A pre-existing /tmp/vai-snapshot.db blocks capture; inspect that file before removing it.' >&2; exit 1; fi
owns_snapshot=1
parent=$$
(sleep 300; kill -TERM "$parent" 2>/dev/null) >/dev/null 2>&1 & watchdog=$!
"$1" & child=$!
wait "$child"
child=
`
	s.stage("Creating VACUUM INTO snapshot · live pod")
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	reader, writer := io.Pipe()
	commandDone := make(chan error, 1)
	go func() {
		err := s.runner(streamCtx, append(append([]string{}, execArgs...), "--", "sh", "-c", script, "runway-capture", remote), nil, writer)
		_ = writer.CloseWithError(err)
		commandDone <- err
	}()
	n, copyErr := io.Copy(output, io.LimitReader(base64.NewDecoder(base64.StdEncoding, reader), cacheLabMaxDB+1))
	_ = reader.Close()
	if copyErr != nil || n > cacheLabMaxDB {
		stopStream()
	}
	commandErr := <-commandDone
	closeErr := output.Close()
	if commandErr != nil {
		return record, commandErr
	}
	if copyErr != nil {
		return record, fmt.Errorf("snapshot transfer failed: %w", copyErr)
	}
	if closeErr != nil {
		return record, closeErr
	}
	if n > cacheLabMaxDB {
		return record, fmt.Errorf("snapshot exceeds the 2 GiB limit")
	}
	// Re-check replica identity to avoid attributing a capture to a restarted pod.
	after, err := s.pods(ctx, profile, args)
	if err != nil {
		return record, fmt.Errorf("could not verify pod identity after capture: %w", err)
	}
	stable := false
	for _, p := range after {
		if p.Name == selected.Name && p.UID == selected.UID && p.Restarts == selected.Restarts {
			stable = true
		}
	}
	if !stable {
		return record, fmt.Errorf("Rancher pod restarted during capture; capture again from the current replica")
	}
	s.stage("Validating SQLite integrity and saving snapshot")
	return s.addSnapshot(ctx, profile.ID, path, cacheLabSnapshot{Name: cacheLabText(req.Name, 140), Folder: cacheLabText(req.Folder, 80), Notes: cacheLabText(req.Notes, 8000), Source: profile.URL, Pod: selected.Name, PodUID: selected.UID, Restarts: selected.Restarts, Image: selected.Image, Method: "VACUUM INTO · vai-vacuum"})
}
