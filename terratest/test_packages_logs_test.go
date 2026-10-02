package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const packageLogTestPods = `{"items":[
{"metadata":{"name":"rancher-abc-123","namespace":"cattle-system","uid":"rancher-uid","labels":{"app":"rancher"}},"spec":{"containers":[{"name":"rancher","image":"rancher/rancher:v2.15.2"},{"name":"sidecar","image":"unrelated:1"}]},"status":{"containerStatuses":[{"name":"rancher","imageID":"containerd://sha256:abc","ready":true,"restartCount":1}]}},
{"metadata":{"name":"rancher-webhook-xyz-456","namespace":"cattle-system","uid":"webhook-uid","labels":{"app.kubernetes.io/name":"rancher-webhook"}},"spec":{"containers":[{"name":"rancher-webhook","image":"rancher/rancher-webhook:v0.9.2"}]},"status":{"containerStatuses":[{"name":"rancher-webhook","imageID":"containerd://sha256:def","ready":false,"restartCount":0}]}},
{"metadata":{"name":"unrelated-rancher","namespace":"cattle-system","uid":"wrong-name","labels":{"app":"rancher"}},"spec":{"containers":[{"name":"rancher","image":"rancher/rancher:v2.15.2"}]}},
{"metadata":{"name":"rancher-fake","namespace":"other","uid":"wrong-namespace","labels":{"app":"rancher"}},"spec":{"containers":[{"name":"rancher","image":"rancher/rancher:v2.15.2"}]}},
{"metadata":{"name":"rancher-no-label","namespace":"cattle-system","uid":"wrong-label"},"spec":{"containers":[{"name":"rancher","image":"rancher/rancher:v2.15.2"}]}}
]}`

func packageLogFixture(t *testing.T) (*localControlPanel, *testPackageService, testPackage, testPackageRequest, string) {
	t.Helper()
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	raw := "apiVersion: v1\nkind: Config\ncurrent-context: default\ncontexts:\n- name: pinned\n  context:\n    cluster: management\n    user: admin\nclusters:\n- name: management\n  cluster:\n    server: https://kubernetes.example.test\nusers:\n- name: admin\n  user:\n    token: kubecredential123456\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	p.rememberClusterSnapshot([]clusterView{{ID: "cluster-a", Name: "Rancher", Role: "local", RancherURL: "https://rancher.test", KubeconfigPath: path}})
	p.clusterWorkspacesMu.Lock()
	r := p.clusterWorkspaces["cluster-a"]
	r.Context = "pinned"
	p.clusterWorkspaces["cluster-a"] = r
	p.clusterWorkspacesMu.Unlock()
	pkg := packageTestPlan(t, s)
	env := testPackageEnvironment{ClusterID: "cluster-a", ClusterName: "Rancher", URL: "https://rancher.test", Source: "recorded", RecordedAt: time.Now().UTC()}
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Observe logs", Purpose: "reproduction"}, &env, nil, nil)
	req := testPackageRequest{Action: "capture-logs", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID, Pod: "rancher-abc-123", PodUID: "rancher-uid", Container: "rancher", CaseID: pkg.Sessions[0].Cases[0].ID, StepID: pkg.Sessions[0].Cases[0].Steps[0].ID}
	return p, s, pkg, req, path
}

func packageLogRunner(t *testing.T, kubePath string, calls *[][]string) cachelab.CommandRunner {
	t.Helper()
	return func(ctx context.Context, args []string, stdin io.Reader, out io.Writer) error {
		*calls = append(*calls, append([]string{}, args...))
		if stdin != nil || len(args) < 7 || args[0] != "--kubeconfig" || args[1] != kubePath || args[2] != "--context" || args[3] != "pinned" || args[4] != "--request-timeout=20s" {
			t.Fatalf("unbound cluster command: %v", args)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("live reads have no deadline")
		}
		switch args[5] {
		case "get":
			if strings.Join(args[5:], " ") != "get pods --namespace=cattle-system -o json" {
				t.Fatalf("unexpected Kubernetes read: %v", args)
			}
			_, err := io.WriteString(out, packageLogTestPods)
			return err
		case "logs":
			_, err := io.WriteString(out, "2026-09-30T12:00:00Z ready\nAuthorization: Bearer bearer-secret\npassword=pass123 token-abc:rawsecret\nkubecredential123456\n")
			return err
		default:
			t.Fatalf("unexpected Kubernetes operation: %v", args)
			return nil
		}
	}
}

func TestTestPackageLogTargetsAndPreservedSnapshot(t *testing.T) {
	p, s, pkg, req, path := packageLogFixture(t)
	var calls [][]string
	runner := packageLogRunner(t, path, &calls)
	listReq := req
	listReq.Action = "log-targets"
	listed, err := p.testPackageLogsWithRunner(context.Background(), s, listReq, runner)
	if err != nil {
		t.Fatal(err)
	}
	response := listed.(map[string]any)
	targets := response["targets"].([]testPackageLogTarget)
	if response["available"] != true || len(targets) != 2 || targets[0].Component != "rancher" || targets[1].Component != "webhook" || targets[0].Container != "rancher" || targets[0].Restarts != 1 {
		t.Fatalf("incorrect allowlisted targets: %+v", response)
	}
	calls = nil
	result, err := p.testPackageLogsWithRunner(context.Background(), s, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	updated := result.(map[string]any)["package"].(testPackage)
	if len(calls) != 3 || updated.Revision == pkg.Revision || len(updated.Sessions[0].Evidence) != 1 {
		t.Fatalf("capture not persisted after verification: %+v %v", calls, updated)
	}
	args := strings.Join(calls[1], " ")
	for _, flag := range []string{"logs rancher-abc-123", "--container=rancher", "--timestamps=true", "--follow=false", "--tail=1000", "--since-time=", "--limit-bytes=4194304", "--previous=false"} {
		if !strings.Contains(args, flag) {
			t.Fatalf("missing bounded snapshot flag %s: %s", flag, args)
		}
	}
	e := updated.Sessions[0].Evidence[0]
	if e.Kind != "pod-logs" || e.ClusterID != "cluster-a" || e.CaseID != req.CaseID || e.StepID != req.StepID || e.SourceID != req.PodUID {
		t.Fatalf("incorrect evidence scope: %+v", e)
	}
	var m testPackageLogMetadata
	if err := json.Unmarshal(e.Metadata, &m); err != nil || !m.Redacted || m.Truncated || m.SinceSeconds != 900 || m.SinceTime.After(m.StartedAt) || m.CompletedAt.Before(m.StartedAt) || m.PodUID != req.PodUID || m.ImageID == "" {
		t.Fatalf("incomplete provenance: %+v %v", m, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	read, err := s.readEvidence(testPackageRequest{ID: updated.ID, SessionID: req.SessionID, SourceID: e.ID})
	if err != nil {
		t.Fatal(err)
	}
	text := read.(map[string]any)["text"].(string)
	for _, secret := range []string{"bearer-secret", "pass123", "rawsecret", "kubecredential123456"} {
		if strings.Contains(text, secret) {
			t.Fatalf("credential retained: %s", secret)
		}
	}
	if !strings.Contains(text, "ready") || !strings.Contains(text, "[redacted]") {
		t.Fatalf("snapshot content disappeared: %q", text)
	}
	restored, err := newTestPackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.get(updated.ID); err != nil {
		t.Fatal(err)
	}
	bundle, err := s.buildBundle(updated.ID, false, []string{e.Artifact.Name})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateTestPackageBundle(&bundle); err != nil {
		t.Fatalf("log bundle no longer validates: %v", err)
	}
}

func TestTestPackageLogsRejectUnsafeSessionBeforeReads(t *testing.T) {
	for _, scenario := range []string{"stale", "finished", "imported", "manual", "different-url", "missing-kubeconfig", "downstream", "missing-context", "bad-case", "bad-step", "bad-window", "bad-pod"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, pkg, req, path := packageLogFixture(t)
			switch scenario {
			case "stale":
				req.Revision = "stale"
			case "finished":
				pkg = packageTestMutation(t, s, testPackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: req.SessionID, Finding: "inconclusive"}, nil, nil, nil)
				req.Revision = pkg.Revision
			case "imported", "manual", "different-url":
				if scenario == "different-url" {
					pkg.Sessions[0].Environment.URL = "https://wrong.test"
				} else {
					pkg.Sessions[0].Environment.Source = scenario
				}
				s.packages[pkg.ID] = pkg
			case "missing-kubeconfig":
				_ = os.Remove(path)
			case "missing-context":
				_ = os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0600)
			case "downstream":
				p.clusterSnapshot["cluster-a"] = clusterView{ID: "cluster-a", Role: "downstream", RancherURL: "https://rancher.test", KubeconfigPath: path}
			case "bad-case":
				req.CaseID = "missing"
			case "bad-step":
				req.StepID = "missing"
			case "bad-window":
				req.SinceSeconds = testPackageLogMaxSince + 1
			case "bad-pod":
				req.Pod = "--all-containers"
			}
			calls := 0
			_, err := p.testPackageLogsWithRunner(context.Background(), s, req, func(context.Context, []string, io.Reader, io.Writer) error { calls++; return nil })
			if err == nil || calls != 0 {
				t.Fatalf("unsafe session used Kubernetes: error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestTestPackageLogsUnavailableAndSelectionChecks(t *testing.T) {
	p, s, pkg, req, path := packageLogFixture(t)
	var calls [][]string
	run := packageLogRunner(t, path, &calls)
	for _, scenario := range []string{"unknown-pod", "wrong-uid", "wrong-container", "previous-unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			input := req
			switch scenario {
			case "unknown-pod":
				input.Pod = "rancher-missing"
			case "wrong-uid":
				input.PodUID = "old-uid"
			case "wrong-container":
				input.Container = "rancher-webhook"
			case "previous-unavailable":
				input.Pod, input.PodUID, input.Container, input.Previous = "rancher-webhook-xyz-456", "webhook-uid", "rancher-webhook", true
			}
			calls = nil
			_, err := p.testPackageLogsWithRunner(context.Background(), s, input, run)
			if err == nil || len(calls) != 1 || calls[0][5] != "get" {
				t.Fatalf("unverified selection read log data: %v %+v", err, calls)
			}
		})
	}
	pkg.Sessions[0].Environment.Source = "imported"
	s.packages[pkg.ID] = pkg
	req.Action = "log-targets"
	response, err := p.testPackageLogsWithRunner(context.Background(), s, req, run)
	if err != nil || response.(map[string]any)["available"] != false || response.(map[string]any)["reason"] == "" {
		t.Fatalf("missing useful unavailable message: %v %v", response, err)
	}
}

func TestTestPackageLogsRevalidateAndDiscardPartialFailures(t *testing.T) {
	for _, scenario := range []string{"revision", "pod-replaced", "restarted", "connection-changed", "partial-error", "context-cancel"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, pkg, req, path := packageLogFixture(t)
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := p.testPackageLogsWithRunner(ctx, s, req, func(_ context.Context, args []string, _ io.Reader, out io.Writer) error {
				calls++
				if args[5] == "get" {
					pods := packageLogTestPods
					if calls > 1 && scenario == "pod-replaced" {
						pods = strings.ReplaceAll(pods, "rancher-uid", "replacement-uid")
					}
					if calls > 1 && scenario == "restarted" {
						pods = strings.ReplaceAll(pods, `"restartCount":1`, `"restartCount":2`)
					}
					_, _ = io.WriteString(out, pods)
					return nil
				}
				_, _ = io.WriteString(out, "partial private content")
				switch scenario {
				case "revision":
					packageTestMutation(t, s, testPackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: req.SessionID, CaseID: req.CaseID, Outcome: "passed"}, nil, nil, nil)
				case "connection-changed":
					_ = os.WriteFile(path, []byte("different connection"), 0600)
				case "partial-error":
					return fmt.Errorf("arbitrary stderr includes token-fake:privatepassword")
				case "context-cancel":
					cancel()
					return context.Canceled
				}
				return nil
			})
			if err == nil || strings.Contains(err.Error(), "privatepassword") {
				t.Fatalf("failure not handled safely: %v", err)
			}
			if scenario == "revision" && !errors.Is(err, errTestPackageConflict) {
				t.Fatalf("expected conflict after network read: %v", err)
			}
			current, _ := s.get(pkg.ID)
			if len(current.Sessions[0].Evidence) != 0 {
				t.Fatal("failed read persisted evidence")
			}
			entries, _ := os.ReadDir(filepath.Join(s.root, pkg.ID, "artifacts"))
			if len(entries) != 0 {
				t.Fatal("failed read leaked an orphan artifact")
			}
		})
	}
}

func TestTestPackageLogBoundsAndCredentialRedaction(t *testing.T) {
	buffer := &testPackageLogBuffer{}
	large := []byte(strings.Repeat("a", testPackageLogMaxBytes+2048))
	n, err := buffer.Write(large)
	if err != nil || n != len(large) || buffer.Len() != testPackageLogMaxBytes || !buffer.truncated {
		t.Fatal("unbounded log buffer")
	}
	if n, err = buffer.Write([]byte("more")); err != nil || n != 4 || buffer.Len() != testPackageLogMaxBytes {
		t.Fatal("full log buffer failed to drain")
	}
	raw := []byte("ordinary message\n\"bootstrapPassword\":\"sensitive password\"\nsecret: s3cret\napi_key='keyvalue'\nhttps://user:cred@host.test\nAuthorization: Bearer abc123\n-----BEGIN PRIVATE KEY-----\nprivate material\n-----END PRIVATE KEY-----\neyJheader.eyJpayload.signature\nexactknown\ntoken-x:ab123\nghp_abcdefg\n")
	redacted, changed := redactTestPackageLog(raw, []string{"exactknown"})
	if !changed || !strings.Contains(string(redacted), "ordinary message") {
		t.Fatal("redaction removed normal log message")
	}
	for _, secret := range []string{"sensitive password", "s3cret", "keyvalue", "user:cred", "abc123", "private material", "eyJheader", "exactknown", "ab123", "ghp_abcdefg"} {
		if strings.Contains(string(redacted), secret) {
			t.Errorf("secret survived redaction: %s", secret)
		}
	}
}

func TestTestPackageLogPreviousInstanceAndBoundedArtifact(t *testing.T) {
	for _, scenario := range []string{"previous", "response-cap", "redaction-expansion"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, _, req, path := packageLogFixture(t)
			req.Previous = scenario == "previous"
			req.TailLines, req.SinceSeconds = 2000, 3600
			if scenario == "redaction-expansion" {
				config, _ := os.ReadFile(path)
				_ = os.WriteFile(path, []byte(strings.ReplaceAll(string(config), "kubecredential123456", "a")), 0600)
			}
			result, err := p.testPackageLogsWithRunner(context.Background(), s, req, func(_ context.Context, args []string, _ io.Reader, out io.Writer) error {
				if args[5] == "get" {
					_, err := io.WriteString(out, packageLogTestPods)
					return err
				}
				if scenario == "previous" {
					if !strings.Contains(strings.Join(args, " "), "--previous=true") {
						t.Fatal("previous logs not explicitly selected")
					}
					_, err := io.WriteString(out, "previous instance logs")
					return err
				}
				count := testPackageLogMaxBytes + 100
				if scenario == "redaction-expansion" {
					count = testPackageLogMaxBytes / 2
				}
				_, err := io.WriteString(out, strings.Repeat("a", count))
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			e := result.(map[string]any)["package"].(testPackage).Sessions[0].Evidence[0]
			var m testPackageLogMetadata
			_ = json.Unmarshal(e.Metadata, &m)
			if e.Artifact.Bytes > testPackageLogMaxBytes || m.TailLines != 2000 || m.SinceSeconds != 3600 || m.ImageObservation != "current-container" {
				t.Fatalf("incorrect capture bounds or provenance: %+v %+v", e.Artifact, m)
			}
			if scenario == "previous" && (!m.Previous || !strings.Contains(e.Name, "previous instance")) {
				t.Fatal("previous evidence not clearly labeled")
			}
			if scenario != "previous" && !m.Truncated {
				t.Fatal("bounded evidence missing truncation marker")
			}
		})
	}
}

func TestTestPackageLogMetadataValidation(t *testing.T) {
	now := time.Now().UTC()
	base := testPackageLogMetadata{Component: "rancher", Namespace: "cattle-system", Pod: "rancher-abc", PodUID: "pod-uid", Container: "rancher", Image: "rancher/rancher:v2", ImageObservation: "current-container", StartedAt: now, CompletedAt: now, SinceTime: now.Add(-time.Minute), SinceSeconds: 60, TailLines: 1000, Timestamps: true, LimitBytes: testPackageLogMaxBytes, ReceivedBytes: 50, Method: "kubernetes-pod-logs"}
	for _, scenario := range []string{"valid", "namespace", "container", "method", "window", "timestamp", "bytes"} {
		value := base
		switch scenario {
		case "namespace":
			value.Namespace = "other"
		case "container":
			value.Container = "sidecar"
		case "method":
			value.Method = "arbitrary-command"
		case "window":
			value.SinceSeconds = -1
		case "timestamp":
			value.CompletedAt = now.Add(-time.Second)
		case "bytes":
			value.ReceivedBytes = testPackageLogMaxBytes + 1
		}
		raw, _ := json.Marshal(value)
		if err := validateTestPackageLogMetadata(raw); (err == nil) != (scenario == "valid") {
			t.Errorf("metadata %s: %v", scenario, err)
		}
	}
}
