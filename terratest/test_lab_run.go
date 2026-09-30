package test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

func testLabConfig(raw string) (string, []string, error) {
	if len(raw) > 128<<10 {
		return "", nil, fmt.Errorf("configuration exceeds 128 KiB")
	}
	var config map[string]any
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&config); err != nil {
		return "", nil, fmt.Errorf("configuration must be a valid YAML mapping")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return "", nil, fmt.Errorf("use a single YAML document")
	}
	rancher, ok := config["rancher"].(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("add a rancher section to cattle-config.yml")
	}
	host, _ := rancher["host"].(string)
	token, _ := rancher["adminToken"].(string)
	if host == "" {
		return "", nil, fmt.Errorf("enter the Rancher hostname")
	}
	if strings.Contains(host, "://") {
		return "", nil, fmt.Errorf("rancher.host expects a hostname, without https:// or a path")
	}
	u, err := url.Parse("https://" + host)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(host, " \n\r\t") {
		return "", nil, fmt.Errorf("enter a valid Rancher hostname, optionally with a port")
	}
	if len(token) < 8 || strings.ContainsAny(token, "\n\r\x00") {
		return "", nil, fmt.Errorf("enter a Rancher API token in rancher.adminToken")
	}
	// Redact all nontrivial configuration strings, including provider credentials
	// in advanced sections whose field names Runway does not know.
	secrets := []string{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, y := range x {
				walk(y)
			}
		case []any:
			for _, y := range x {
				walk(y)
			}
		case string:
			if len(x) >= 4 {
				secrets = append(secrets, x, url.QueryEscape(x), base64.StdEncoding.EncodeToString([]byte(x)))
				for _, line := range strings.Split(x, "\n") {
					if len(strings.TrimSpace(line)) >= 8 {
						secrets = append(secrets, strings.TrimSpace(line))
					}
				}
				b, _ := json.Marshal(x)
				if len(b) > 2 {
					secrets = append(secrets, string(b[1:len(b)-1]))
				}
			}
		}
	}
	walk(config)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return host, secrets, nil
}
func (s *testLabService) review(req testLabRequest) (any, error) {
	host, _, err := testLabConfig(req.Config)
	if err != nil {
		return nil, err
	}
	clusterID, err := s.resolveClusterID(req.ClusterID, host)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	commands, err := s.commandsLocked(req)
	if err != nil {
		return nil, err
	}
	goPath, goErr := resolveLocalToolPath("go")
	if goErr != nil {
		goPath = ""
	}
	return map[string]any{"host": host, "clusterId": clusterID, "sha": req.SHA, "commands": commands, "goPath": goPath, "goVersion": s.library.Catalog.GoVersion, "confirmation": typedConfirmationPhrase, "warnings": []string{"These are integration tests. They may create, change, or delete resources and incur cloud costs.", "Individual tests still run suite setup and cleanup. Review provider-specific requirements in the linked source.", "External test code runs as your local user. Runway isolates its working files and environment, but this is not an OS sandbox.", "Canceling stops execution; it cannot guarantee that a test's cleanup completes."}}, nil
}
func (s *testLabService) startRun(req testLabRequest) (any, error) {
	host, secrets, err := testLabConfig(req.Config)
	if err != nil {
		return nil, err
	}
	if req.Confirm != typedConfirmationPhrase {
		return nil, fmt.Errorf("review this run and type confirm to start it")
	}
	clusterID, err := s.resolveClusterID(req.ClusterID, host)
	if err != nil {
		return nil, err
	}
	goPath, err := resolveLocalToolPath("go")
	if err != nil {
		return nil, fmt.Errorf("install Go and reopen Runway before starting local tests")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil, fmt.Errorf("a local test run is already active")
	}
	if len(s.library.Runs) >= 200 {
		return nil, fmt.Errorf("remove old local results before starting another run")
	}
	commands, err := s.commandsLocked(req)
	if err != nil {
		return nil, err
	}
	id := cacheLabID()
	run := testLabRun{ClusterID: clusterID, ID: id, Name: cacheLabText(req.Name, 100), Host: host, SHA: req.SHA, Ref: s.library.Catalog.Ref, Selection: append([]string{}, req.Selection...), Tags: req.Tags, Timeout: req.Timeout, Status: "running", Stage: "Preparing isolated source", StartedAt: time.Now(), Results: []testLabResult{}}
	if run.Name == "" {
		run.Name = fmt.Sprintf("%s · %d suite(s)", host, len(commands))
	}
	s.library.Runs = append([]testLabRun{run}, s.library.Runs...)
	if err = s.persistLocked(); err != nil {
		s.library.Runs = s.library.Runs[1:]
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.execute(ctx, run, commands, req.Config, secrets, goPath)
	return run, nil
}
func (s *testLabService) runLocked(id string) *testLabRun {
	for i := range s.library.Runs {
		if s.library.Runs[i].ID == id {
			return &s.library.Runs[i]
		}
	}
	return nil
}
func (s *testLabService) execute(ctx context.Context, run testLabRun, commands []testLabCommand, config string, secrets []string, goPath string) {
	work := filepath.Join(s.root, "work-"+run.ID)
	var failure error
	status := "passed"
	output := &testLabOutput{s: s, id: run.ID, secrets: secrets, seen: map[string]bool{}}
	checkpointDone := make(chan struct{})
	checkpointStopped := make(chan struct{})
	go func() {
		defer close(checkpointStopped)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-checkpointDone:
				return
			case <-ticker.C:
				s.mu.Lock()
				_ = s.persistLocked()
				_ = os.WriteFile(filepath.Join(s.root, run.ID+".log"), []byte(s.logs[run.ID]), 0600)
				s.mu.Unlock()
			}
		}
	}()
	defer func() {
		close(checkpointDone)
		<-checkpointStopped
		output.flush()
		_ = os.RemoveAll(work)
		if ctx.Err() != nil {
			status = "canceled"
			failure = fmt.Errorf("stopped by user or app shutdown; inspect the target for resources left by the tests")
		} else if failure != nil {
			status = "failed"
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		r := s.runLocked(run.ID)
		r.Status = status
		r.Stage = "Finished"
		r.FinishedAt = time.Now()
		if failure != nil {
			r.Error = output.redact(failure.Error())
		}
		if s.cancel != nil {
			s.cancel()
		}
		s.cancel = nil
		if err := os.WriteFile(filepath.Join(s.root, run.ID+".log"), []byte(s.logs[run.ID]), 0600); err != nil {
			r.Error += " Result log could not be saved."
		}
		if err := s.persistLocked(); err != nil {
			r.Error += " Run history could not be saved."
		}
	}()
	if failure = os.MkdirAll(work, 0700); failure != nil {
		return
	}
	source := filepath.Join(work, "source")
	if failure = os.MkdirAll(source, 0700); failure != nil {
		return
	}
	if failure = testLabExtract(filepath.Join(s.root, run.SHA+".tar.gz"), source); failure != nil {
		return
	}
	configPath := filepath.Join(work, "cattle-config.yml")
	if failure = os.WriteFile(configPath, []byte(config), 0600); failure != nil {
		return
	}
	// Isolate Go configuration, caches, HOME, and TMPDIR. External tests do not
	// inherit Runway's AWS/GitHub/provider credentials from its environment.
	home := filepath.Join(work, "home")
	tmp := filepath.Join(work, "tmp")
	cache := filepath.Join(s.root, "go-cache")
	mod := filepath.Join(s.root, "go-modules")
	for _, dir := range []string{home, tmp, cache, mod} {
		if failure = os.MkdirAll(dir, 0700); failure != nil {
			return
		}
	}
	env := []string{"PATH=" + localToolPATH(), "HOME=" + home, "TMPDIR=" + tmp, "GOCACHE=" + cache, "GOPATH=" + filepath.Join(work, "go"), "GOMODCACHE=" + mod, "GOTOOLCHAIN=auto", "GOENV=off", "GOTELEMETRY=off", "GOFLAGS=", "GOWORK=off", "GOPROXY=https://proxy.golang.org,direct", "GOSUMDB=sum.golang.org", "GOVCS=*:off", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "CI=true", "CATTLE_TEST_CONFIG=" + configPath}
	if runtime.GOOS == "darwin" {
		s.mu.Lock()
		s.runLocked(run.ID).Stage = "Checking local toolchain"
		s.mu.Unlock()
		output.line("[runway] Checking the macOS SDK and C compiler…\n")
		env, failure = testLabDarwinToolchainEnv(ctx, env)
		if failure != nil {
			if ctx.Err() == nil {
				failure = fmt.Errorf("local toolchain check failed: %w. Check the selected Xcode/Command Line Tools installation (xcode-select -p); install missing Command Line Tools with xcode-select --install, then retry. No tests ran", failure)
				output.line("[runway] " + failure.Error() + "\n")
			}
			return
		}
		output.line("[runway] macOS SDK and C compiler ready.\n")
	}
	for index, command := range commands {
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		s.runLocked(run.ID).Stage = fmt.Sprintf("Suite %d of %d · %s", index+1, len(commands), command.Package)
		s.mu.Unlock()
		output.line(fmt.Sprintf("[runway] %s · %s\n", command.Package, command.Pattern))
		// The outer deadline also bounds compilation/module downloads, which Go's
		// -timeout flag does not include.
		stepCtx, cancel := context.WithTimeout(ctx, time.Duration(run.Timeout+10)*time.Minute)
		cmd := exec.CommandContext(stepCtx, goPath, command.Args...)
		cmd.Dir = source
		cmd.Env = env
		cmd.SysProcAttr = panelCommandSysProcAttr()
		cmd.Stdout = output
		cmd.Stderr = output
		cmd.WaitDelay = 10 * time.Second
		done := make(chan struct{})
		cmd.Cancel = func() error {
			err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
			go func() {
				select {
				case <-done:
					return
				case <-time.After(8 * time.Second):
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			}()
			return err
		}
		err := cmd.Run()
		close(done)
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		timedOut := stepCtx.Err() == context.DeadlineExceeded
		cancel()
		output.flush()
		if timedOut {
			failure = fmt.Errorf("suite exceeded its execution and compilation deadline")
			return
		}
		if err != nil {
			failure = fmt.Errorf("test command failed; inspect the activity log and test results")
			return
		}
		for _, expected := range command.Expected {
			if !output.hasSeen(command.Package + "::" + expected) {
				failure = fmt.Errorf("selection did not execute %s; no tests is not a pass. Check build tags and the suite source", expected)
				return
			}
		}
	}
}

type testLabOutput struct {
	mu      sync.Mutex
	s       *testLabService
	id      string
	secrets []string
	pending string
	seen    map[string]bool
}

var testLabTokenPattern = regexp.MustCompile(`(?i)(?:token-[a-z0-9]+:[a-z0-9]+|gh[pousr]_[A-Za-z0-9_]+)`)

func (o *testLabOutput) redact(text string) string {
	for _, secret := range o.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return testLabTokenPattern.ReplaceAllString(text, "[redacted]")
}
func (o *testLabOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pending += string(p)
	for {
		index := strings.IndexByte(o.pending, '\n')
		if index < 0 {
			break
		}
		line := o.pending[:index+1]
		o.pending = o.pending[index+1:]
		o.line(line)
	}
	if len(o.pending) > 1<<20 {
		o.pending = ""
		o.line("[runway] Oversized output line omitted.\n")
	}
	return len(p), nil
}
func (o *testLabOutput) flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pending != "" {
		o.line(o.pending + "\n")
		o.pending = ""
	}
}
func (o *testLabOutput) hasSeen(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.seen[key]
}
func (o *testLabOutput) line(line string) {
	var event struct {
		Action, Package, Test, Output string
		Elapsed                       float64
	}
	parsed := json.Unmarshal([]byte(line), &event) == nil && event.Action != ""
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	r := o.s.runLocked(o.id)
	if r == nil {
		return
	}
	if parsed {
		pkg := strings.TrimPrefix(event.Package, "github.com/rancher/tests/")
		if event.Test != "" {
			if event.Action == "run" {
				o.seen[pkg+"::"+event.Test] = true
			}
			if event.Action == "run" || event.Action == "pass" || event.Action == "fail" || event.Action == "skip" {
				name := o.redact(event.Test)
				found := false
				for i := range r.Results {
					if r.Results[i].Package == pkg && r.Results[i].Name == name {
						r.Results[i].Status = event.Action
						r.Results[i].Elapsed = event.Elapsed
						found = true
						break
					}
				}
				if !found && len(r.Results) < 5000 {
					r.Results = append(r.Results, testLabResult{Package: pkg, Name: name, Status: event.Action, Elapsed: event.Elapsed})
				}
			}
		}
		line = event.Output
	}
	if line == "" {
		return
	}
	line = o.redact(line)
	line = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, line)
	log := o.s.logs[o.id] + line
	if len(log) > 2<<20 {
		start := len(log) - (2 << 20)
		if i := strings.IndexByte(log[start:], '\n'); i >= 0 {
			start += i + 1
		}
		log = "[runway] Earlier output omitted; keeping the latest 2 MiB.\n" + log[start:]
	}
	o.s.logs[o.id] = log
}
