package test

// Live evidence is always an explicit, bounded read against the active
// session's registered target. The package owns the resulting sanitized copy.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const issuePackageLogMaxBytes = 4 << 20
const issuePackageLogMaxTail = 10000
const issuePackageLogMaxSince = 24 * 60 * 60

type issuePackageLogTarget struct {
	Component string `json:"component"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	PodUID    string `json:"podUid"`
	Container string `json:"container"`
	Image     string `json:"image"`
	ImageID   string `json:"imageId,omitempty"`
	Ready     bool   `json:"ready"`
	Restarts  int    `json:"restarts"`
}

type issuePackageLogMetadata struct {
	Component        string    `json:"component"`
	Namespace        string    `json:"namespace"`
	Pod              string    `json:"pod"`
	PodUID           string    `json:"podUid"`
	Container        string    `json:"container"`
	Image            string    `json:"image"`
	ImageID          string    `json:"imageId,omitempty"`
	ImageObservation string    `json:"imageObservation"`
	Restarts         int       `json:"restarts"`
	StartedAt        time.Time `json:"startedAt"`
	CompletedAt      time.Time `json:"completedAt"`
	SinceTime        time.Time `json:"sinceTime"`
	SinceSeconds     int64     `json:"sinceSeconds"`
	TailLines        int64     `json:"tailLines"`
	Previous         bool      `json:"previous"`
	Timestamps       bool      `json:"timestamps"`
	LimitBytes       int64     `json:"limitBytes"`
	ReceivedBytes    int64     `json:"receivedBytes"`
	Truncated        bool      `json:"truncated"`
	Redacted         bool      `json:"redacted"`
	Method           string    `json:"method"`
}

var issuePackageKubeName = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$`)
var issuePackagePodUID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

func validIssuePackageLogTarget(t issuePackageLogTarget) bool {
	return packageEnum(t.Component, "rancher", "webhook") && t.Namespace == "cattle-system" &&
		len(t.Pod) <= 253 && issuePackageKubeName.MatchString(t.Pod) && issuePackagePodUID.MatchString(t.PodUID) &&
		packageEnum(t.Container, "rancher", "rancher-webhook") && packageText(t.Image, 2000, true) &&
		packageText(t.ImageID, 2000, false) && t.Restarts >= 0 &&
		(t.Component == "rancher" && t.Container == "rancher" || t.Component == "webhook" && t.Container == "rancher-webhook")
}

func validateIssuePackageLogMetadata(raw json.RawMessage) error {
	var m issuePackageLogMetadata
	if err := decodeIssuePackage(raw, &m); err != nil {
		return err
	}
	target := issuePackageLogTarget{Component: m.Component, Namespace: m.Namespace, Pod: m.Pod, PodUID: m.PodUID, Container: m.Container, Image: m.Image, ImageID: m.ImageID, Restarts: m.Restarts}
	if !validIssuePackageLogTarget(target) || m.StartedAt.IsZero() || m.CompletedAt.Before(m.StartedAt) ||
		m.SinceTime.IsZero() || m.SinceTime.After(m.StartedAt) || m.SinceSeconds < 1 || m.SinceSeconds > issuePackageLogMaxSince ||
		m.TailLines < 1 || m.TailLines > issuePackageLogMaxTail || !m.Timestamps || m.LimitBytes != issuePackageLogMaxBytes ||
		m.ReceivedBytes < 0 || m.ReceivedBytes > issuePackageLogMaxBytes || m.Method != "kubernetes-pod-logs" || m.ImageObservation != "current-container" {
		return fmt.Errorf("invalid pod log snapshot metadata")
	}
	return nil
}

func (s *issuePackageService) logSession(req issuePackageRequest) (issuePackageSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg, ok := s.packages[req.ID]
	if !ok {
		return issuePackageSession{}, fmt.Errorf("issue package not found")
	}
	if req.Revision == "" || pkg.Revision != req.Revision {
		return issuePackageSession{}, errIssuePackageConflict
	}
	for _, session := range cloneIssuePackage(pkg).Sessions {
		if session.ID != req.SessionID {
			continue
		}
		if session.Status != "active" {
			return session, fmt.Errorf("completed sessions are preserved; start a new session to capture live logs")
		}
		if req.CaseID != "" {
			for _, c := range session.Cases {
				if c.ID != req.CaseID {
					continue
				}
				if req.StepID == "" {
					return session, nil
				}
				for _, step := range c.Steps {
					if step.ID == req.StepID {
						return session, nil
					}
				}
				return session, fmt.Errorf("step does not belong to this session's case")
			}
			return session, fmt.Errorf("case does not belong to this session")
		}
		if req.StepID != "" {
			return session, fmt.Errorf("step evidence needs a case")
		}
		return session, nil
	}
	return issuePackageSession{}, fmt.Errorf("test session not found")
}

// resolveIssuePackageLogCluster rejects imported/manual identities, archived
// targets, and changed URLs. No client-provided path becomes a command argument.
func (p *localControlPanel) resolveIssuePackageLogCluster(session issuePackageSession) (clusterWorkspaceRecord, error) {
	e := session.Environment
	if e.Source != "recorded" || e.ClusterID == "" || e.URL == "" {
		return clusterWorkspaceRecord{}, fmt.Errorf("live logs need a session started with a registered Rancher; start a new session and choose its local cluster")
	}
	records, err := p.labClusterCandidates()
	if err != nil {
		return clusterWorkspaceRecord{}, fmt.Errorf("registered cluster details are unavailable; refresh Clusters and try again")
	}
	wantURL, err := clusterWorkspaceURL(e.URL)
	if err != nil {
		return clusterWorkspaceRecord{}, fmt.Errorf("this session has no verified local Rancher identity")
	}
	for _, record := range records {
		if record.ID != e.ClusterID {
			continue
		}
		actualURL, err := clusterWorkspaceURL(record.URL)
		if err != nil || actualURL == "" || actualURL != wantURL {
			return record, fmt.Errorf("this cluster's Rancher URL differs from the preserved session; start a session with the current target")
		}
		if record.Archived {
			return record, fmt.Errorf("this cluster is archived; live logs need an available Rancher cluster")
		}
		if packageEnum(record.Role, "downstream", "steve", "k3d", "docker") {
			return record, fmt.Errorf("live pod logs are available for kubeconfig-backed Rancher management clusters")
		}
		if strings.TrimSpace(record.Kubeconfig) == "" {
			return record, fmt.Errorf("no management kubeconfig is registered for this Rancher; connect its local cluster in Runway first")
		}
		record.Context = clusterWorkspaceKubeconfigContext(record.Kubeconfig, record.Context)
		if record.Context == "" {
			return record, fmt.Errorf("the registered kubeconfig has no usable context; reconnect the cluster first")
		}
		return record, nil
	}
	return clusterWorkspaceRecord{}, fmt.Errorf("this session's cluster is not registered on this computer; start a new session with a local Rancher")
}

type issuePackageLogConnection struct {
	record      clusterWorkspaceRecord
	fingerprint string
	secrets     []string
}

func issuePackageReadLogConnection(record clusterWorkspaceRecord) (issuePackageLogConnection, error) {
	value := issuePackageLogConnection{record: record}
	raw, err := issuePackageReadBounded(record.Kubeconfig, 4<<20)
	if err != nil {
		return value, fmt.Errorf("the registered kubeconfig is unavailable or not a regular local file; reconnect the cluster first")
	}
	var config struct {
		Contexts []struct {
			Name    string `yaml:"name"`
			Context struct {
				Cluster string `yaml:"cluster"`
				User    string `yaml:"user"`
			} `yaml:"context"`
		} `yaml:"contexts"`
		Clusters []struct {
			Name    string `yaml:"name"`
			Cluster struct {
				Server string `yaml:"server"`
			} `yaml:"cluster"`
		} `yaml:"clusters"`
		Users []struct {
			Name string `yaml:"name"`
			User struct {
				Token         string `yaml:"token"`
				Password      string `yaml:"password"`
				ClientKeyData string `yaml:"client-key-data"`
			} `yaml:"user"`
		} `yaml:"users"`
	}
	if yaml.Unmarshal(raw, &config) != nil {
		return value, fmt.Errorf("the registered kubeconfig cannot be read; reconnect the cluster first")
	}
	cluster, user := "", ""
	for _, c := range config.Contexts {
		if c.Name == record.Context {
			if cluster != "" {
				return value, fmt.Errorf("the registered kubeconfig contains an ambiguous context")
			}
			cluster, user = c.Context.Cluster, c.Context.User
		}
	}
	server := ""
	for _, c := range config.Clusters {
		if c.Name == cluster && cluster != "" {
			if server != "" {
				return value, fmt.Errorf("the registered kubeconfig contains an ambiguous cluster")
			}
			server = c.Cluster.Server
		}
	}
	if cluster == "" || server == "" {
		return value, fmt.Errorf("the registered kubeconfig context no longer identifies a cluster")
	}
	for _, u := range config.Users {
		if u.Name == user && user != "" {
			for _, secret := range []string{u.User.Token, u.User.Password, u.User.ClientKeyData} {
				if secret != "" {
					value.secrets = append(value.secrets, secret)
				}
			}
		}
	}
	sum := sha256.Sum256(raw)
	value.fingerprint = hex.EncodeToString(sum[:])
	return value, nil
}

func (c issuePackageLogConnection) args() []string {
	return []string{"--kubeconfig", c.record.Kubeconfig, "--context", c.record.Context, "--request-timeout=20s"}
}

func issuePackageLiveLogError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("log snapshot request stopped: %w; no evidence was saved", ctx.Err())
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "forbidden"):
		return fmt.Errorf("the registered kubeconfig cannot read Rancher pods or their logs; no evidence was saved")
	case strings.Contains(message, "unauthorized"), strings.Contains(message, "credentials"):
		return fmt.Errorf("the registered kubeconfig needs fresh credentials; reconnect the cluster before capturing logs")
	case strings.Contains(message, "previous terminated container"):
		return fmt.Errorf("this container has no available previous-instance logs; turn off Previous instance and retry")
	case strings.Contains(message, "kubectl is required"):
		return fmt.Errorf("kubectl is required for live log snapshots; install it and reopen Runway")
	default:
		// Raw kubectl/plugin stderr may include credentials, paths, or log text.
		return fmt.Errorf("Kubernetes could not finish reading the selected pod; check cluster access and retry. No evidence was saved")
	}
}

func issuePackageLogTargets(ctx context.Context, connection issuePackageLogConnection, run cachelab.CommandRunner) ([]issuePackageLogTarget, error) {
	out := &issuePackageLogBuffer{}
	args := append(connection.args(), "get", "pods", "--namespace=cattle-system", "-o", "json")
	if err := run(ctx, args, nil, out); err != nil {
		return nil, issuePackageLiveLogError(ctx, err)
	}
	if out.truncated {
		return nil, fmt.Errorf("pod discovery exceeded its response size limit; no evidence was saved")
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name      string            `json:"name"`
				Namespace string            `json:"namespace"`
				UID       string            `json:"uid"`
				Labels    map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Name  string `json:"name"`
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
			Status struct {
				ContainerStatuses []struct {
					Name         string `json:"name"`
					ImageID      string `json:"imageID"`
					Ready        bool   `json:"ready"`
					RestartCount int    `json:"restartCount"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal(out.Bytes(), &pods) != nil {
		return nil, fmt.Errorf("Kubernetes returned unreadable pod details; no evidence was saved")
	}
	result := []issuePackageLogTarget{}
	for _, pod := range pods.Items {
		if pod.Metadata.Namespace != "cattle-system" {
			continue
		}
		for _, component := range []struct{ label, component string }{{"rancher", "rancher"}, {"rancher-webhook", "webhook"}} {
			if !strings.HasPrefix(pod.Metadata.Name, component.label+"-") ||
				(pod.Metadata.Labels["app"] != component.label && pod.Metadata.Labels["app.kubernetes.io/name"] != component.label) {
				continue
			}
			for _, container := range pod.Spec.Containers {
				if container.Name != component.label {
					continue
				}
				target := issuePackageLogTarget{Component: component.component, Namespace: "cattle-system", Pod: pod.Metadata.Name, PodUID: pod.Metadata.UID, Container: container.Name, Image: container.Image}
				for _, status := range pod.Status.ContainerStatuses {
					if status.Name == container.Name {
						target.ImageID, target.Ready, target.Restarts = status.ImageID, status.Ready, status.RestartCount
					}
				}
				if validIssuePackageLogTarget(target) {
					result = append(result, target)
				}
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Component != result[j].Component {
			return result[i].Component < result[j].Component
		}
		return result[i].Pod < result[j].Pod
	})
	return result, nil
}

// A non-failing bounded writer drains any excess response while retaining only
// the limit; the process and request timeout also bound a misbehaving server.
type issuePackageLogBuffer struct {
	buffer    bytes.Buffer
	max       int
	truncated bool
}

func (b *issuePackageLogBuffer) Write(raw []byte) (int, error) {
	n := len(raw)
	limit := b.max
	if limit == 0 {
		limit = issuePackageLogMaxBytes
	}
	remaining := limit - b.buffer.Len()
	if len(raw) > remaining {
		raw = raw[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(raw)
	return n, nil
}

func (b *issuePackageLogBuffer) Bytes() []byte { return b.buffer.Bytes() }
func (b *issuePackageLogBuffer) Len() int      { return b.buffer.Len() }

// Keep the bounded buffer's methods explicit: embedding bytes.Buffer would
// promote WriteString/ReadFrom and let io.Copy bypass the Write limit.
func runIssuePackageLogCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	bin, err := resolveLocalToolPath("kubectl")
	if err != nil {
		return fmt.Errorf("kubectl is required for live log snapshots")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = localToolEnv(nil)
	cmd.Stdin, cmd.Stdout = stdin, stdout
	stderr := &issuePackageLogBuffer{max: 12000}
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("Kubernetes request failed: %s", string(stderr.Bytes()))
	}
	return nil
}

var issuePackageLogPrivateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)
var issuePackageLogAuthHeader = regexp.MustCompile(`(?im)((?:authorization|proxy-authorization)["']?\s*[:=]\s*)[^\r\n]+`)
var issuePackageLogSecretValue = regexp.MustCompile(`(?i)(["']?(?:bootstrapPassword|password|adminToken|apiToken|api[_-]?key|access[_-]?key|secret[_-]?key|client[_-]?secret|token|secret)["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)
var issuePackageLogJWT = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
var issuePackageLogURLAuth = regexp.MustCompile(`(?i)(https?://)[^\s/@:]+:[^\s/@]+@`)

func redactIssuePackageLog(raw []byte, secrets []string) ([]byte, bool) {
	text := strings.ToValidUTF8(string(raw), "�")
	// Process larger configured secrets before any shorter overlapping value.
	secrets = append([]string{}, secrets...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	replacements := []string{}
	for _, secret := range secrets {
		if secret != "" {
			replacements = append(replacements, secret, "[redacted]")
		}
	}
	// A single pass also prevents short overlapping credentials from expanding
	// the replacement marker repeatedly.
	if len(replacements) > 0 {
		text = strings.NewReplacer(replacements...).Replace(text)
	}
	text = issuePackageLogPrivateKey.ReplaceAllString(text, "[redacted private key]")
	text = issuePackageLogAuthHeader.ReplaceAllString(text, "${1}[redacted]")
	text = issuePackageLogSecretValue.ReplaceAllString(text, "${1}[redacted]")
	text = issuePackageLogJWT.ReplaceAllString(text, "[redacted]")
	text = issuePackageLogURLAuth.ReplaceAllString(text, "${1}[redacted]@")
	text = testLabTokenPattern.ReplaceAllString(text, "[redacted]")
	return []byte(text), text != string(raw)
}

func (p *localControlPanel) handleIssuePackageLogs(ctx context.Context, s *issuePackageService, req issuePackageRequest) (any, error) {
	return p.issuePackageLogsWithRunner(ctx, s, req, runIssuePackageLogCommand)
}

func (p *localControlPanel) issuePackageLogsWithRunner(ctx context.Context, s *issuePackageService, req issuePackageRequest, run cachelab.CommandRunner) (any, error) {
	if !packageEnum(req.Action, "log-targets", "capture-logs") {
		return nil, fmt.Errorf("unknown log snapshot action")
	}
	session, err := s.logSession(req)
	if err != nil {
		return nil, err
	}
	unavailable := func(err error) (any, error) {
		if req.Action == "log-targets" {
			return map[string]any{"available": false, "reason": err.Error(), "targets": []issuePackageLogTarget{}}, nil
		}
		return nil, err
	}
	record, err := p.resolveIssuePackageLogCluster(session)
	if err != nil {
		return unavailable(err)
	}
	connection, err := issuePackageReadLogConnection(record)
	if err != nil {
		return unavailable(err)
	}
	if req.Action == "capture-logs" {
		if req.TailLines == 0 {
			req.TailLines = 1000
		}
		if req.SinceSeconds == 0 {
			req.SinceSeconds = 15 * 60
		}
		if req.TailLines < 1 || req.TailLines > issuePackageLogMaxTail || req.SinceSeconds < 1 || req.SinceSeconds > issuePackageLogMaxSince ||
			!issuePackageKubeName.MatchString(req.Pod) || !issuePackagePodUID.MatchString(req.PodUID) ||
			!packageEnum(req.Container, "rancher", "rancher-webhook") || !packageText(req.Description, 32000, false) {
			return nil, fmt.Errorf("select a Rancher or webhook container and a log window of 1–10000 lines within the last 24 hours")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	targets, err := issuePackageLogTargets(ctx, connection, run)
	if err != nil {
		return unavailable(err)
	}
	if req.Action == "log-targets" {
		if _, err := s.logSession(req); err != nil {
			return nil, err
		}
		response := map[string]any{"available": len(targets) > 0, "targets": targets, "limits": map[string]int{"maxTailLines": issuePackageLogMaxTail, "maxSinceSeconds": issuePackageLogMaxSince, "maxBytes": issuePackageLogMaxBytes}}
		if len(targets) == 0 {
			response["reason"] = "No Rancher or rancher-webhook containers were found in cattle-system. Refresh the cluster and try again."
		}
		return response, nil
	}
	var target issuePackageLogTarget
	for _, candidate := range targets {
		if candidate.Pod == req.Pod && candidate.PodUID == req.PodUID && candidate.Container == req.Container {
			target = candidate
			break
		}
	}
	if target.Pod == "" {
		return nil, fmt.Errorf("the selected pod changed or is no longer a Rancher/webhook container; refresh the pod list before capturing")
	}
	if req.Previous && target.Restarts == 0 {
		return nil, fmt.Errorf("this container has no recorded restarts; choose its current logs")
	}
	// Recheck the package immediately before collecting any actual log content.
	if _, err := s.logSession(req); err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	since := started.Add(-time.Duration(req.SinceSeconds) * time.Second)
	args := append(connection.args(), "logs", req.Pod, "--namespace=cattle-system", "--container="+req.Container, "--timestamps=true", "--follow=false", "--tail="+strconv.FormatInt(req.TailLines, 10), "--since-time="+since.Format(time.RFC3339Nano), "--limit-bytes="+strconv.Itoa(issuePackageLogMaxBytes), "--previous="+strconv.FormatBool(req.Previous))
	out := &issuePackageLogBuffer{}
	if err := run(ctx, args, nil, out); err != nil {
		return nil, issuePackageLiveLogError(ctx, err)
	}
	completed := time.Now().UTC()
	// Name reuse and restarts must not silently change the selected evidence.
	after, err := issuePackageLogTargets(ctx, connection, run)
	if err != nil {
		return nil, err
	}
	stable := false
	for _, current := range after {
		if current.Pod == target.Pod && current.PodUID == target.PodUID && current.Container == target.Container && current.Image == target.Image && current.ImageID == target.ImageID && current.Restarts == target.Restarts {
			stable = true
			break
		}
	}
	if !stable {
		return nil, fmt.Errorf("the selected container changed or restarted during capture; no evidence was saved. Refresh the pod list and retry")
	}
	latestRecord, err := p.resolveIssuePackageLogCluster(session)
	if err != nil {
		return nil, err
	}
	latest, err := issuePackageReadLogConnection(latestRecord)
	if err != nil || latest.fingerprint != connection.fingerprint || latestRecord.Kubeconfig != record.Kubeconfig || latestRecord.Context != record.Context {
		return nil, fmt.Errorf("the registered cluster connection changed during capture; no evidence was saved")
	}
	if err := ctx.Err(); err != nil {
		return nil, issuePackageLiveLogError(ctx, err)
	}
	raw, redacted := redactIssuePackageLog(out.Bytes(), connection.secrets)
	truncated := out.truncated || out.Len() >= issuePackageLogMaxBytes
	if len(raw) > issuePackageLogMaxBytes {
		// Redaction markers can be longer than the value they replace. The saved
		// artifact keeps the same byte budget as the Kubernetes response.
		raw = []byte(strings.ToValidUTF8(string(raw[:issuePackageLogMaxBytes]), ""))
		truncated = true
	}
	metadata := issuePackageLogMetadata{Component: target.Component, Namespace: target.Namespace, Pod: target.Pod, PodUID: target.PodUID, Container: target.Container, Image: target.Image, ImageID: target.ImageID, ImageObservation: "current-container", Restarts: target.Restarts, StartedAt: started, CompletedAt: completed, SinceTime: since, SinceSeconds: req.SinceSeconds, TailLines: req.TailLines, Previous: req.Previous, Timestamps: true, LimitBytes: issuePackageLogMaxBytes, ReceivedBytes: int64(out.Len()), Truncated: truncated, Redacted: redacted, Method: "kubernetes-pod-logs"}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	evidence := issuePackageEvidence{ID: cachelab.ID(), Kind: "pod-logs", Name: map[string]string{"rancher": "Rancher", "webhook": "Webhook"}[target.Component] + " logs · " + target.Pod, Description: req.Description, SourceID: target.PodUID, ClusterID: record.ID, CapturedAt: completed, AttachedAt: time.Now().UTC(), CaseID: req.CaseID, StepID: req.StepID, Metadata: encoded}
	if req.Previous {
		evidence.Name += " · previous instance"
	}
	evidence.Artifact = packageArtifact(evidence.ID+".log", "text/plain", raw)
	req.Action = "attach-evidence"
	// mutate checks revision, active session and evidence scope again under lock.
	return s.mutate(req, nil, &evidence, raw)
}

var _ io.Writer = (*issuePackageLogBuffer)(nil)
