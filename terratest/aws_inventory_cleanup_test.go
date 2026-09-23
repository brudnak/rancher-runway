package test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/smithy-go"
	"github.com/spf13/viper"
)

type fakeAWSCleanup struct {
	mu            sync.Mutex
	items         map[string]awsCleanupInspection
	inspectErrors map[string]error
	deleteErrors  map[string]error
	deleted       []string
}

func (f *fakeAWSCleanup) Inspect(_ context.Context, item awsResourceView) (awsCleanupInspection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.inspectErrors[item.ID]; err != nil {
		return awsCleanupInspection{}, err
	}
	return f.items[item.ID], nil
}
func (f *fakeAWSCleanup) Delete(_ context.Context, item awsCleanupInspection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, item.Resource.ID)
	return f.deleteErrors[item.Resource.ID]
}
func cleanupTestResource(kind, id string) awsResourceView {
	return awsResourceView{Type: kind, ID: id, Name: id, Region: "us-east-2", RunID: "old-run", Tags: map[string]string{"Owner": "Test Owner", "ManagedBy": "rancher-runway", "HA_Rancher_RKE2_Run_ID": "old-run"}}
}
func newAWSCleanupTestPanel(t *testing.T, items ...awsResourceView) (*localControlPanel, *fakeAWSCleanup) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("user.first_name", "Test")
	viper.Set("user.last_name", "Owner")
	viper.Set("tf_vars.aws_region", "us-east-2")
	p := newCleanupBatchTestPanel(t)
	p.awsCache = panelAWSInventoryState{UpdatedAt: time.Now(), Owner: "Test Owner", Region: "us-east-2", Items: items}
	client := &fakeAWSCleanup{items: map[string]awsCleanupInspection{}, inspectErrors: map[string]error{}, deleteErrors: map[string]error{}}
	for _, item := range items {
		client.items[item.ID] = awsCleanupInspection{Resource: item, Effects: []string{"Delete " + item.ID}}
	}
	p.awsCleanupClientFactory = func(context.Context, string) (awsCleanupClient, error) { return client, nil }
	return p, client
}
func cleanupTestRefs(items ...awsResourceView) []awsCleanupRef {
	refs := []awsCleanupRef{}
	for _, item := range items {
		refs = append(refs, awsCleanupRef{Type: item.Type, ID: item.ID})
	}
	return refs
}
func waitForAWSCleanup(t *testing.T, p *localControlPanel) awsCleanupSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := p.snapshotAWSCleanup()
		if !snapshot.Running && snapshot.FinishedAt != nil {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("AWS cleanup did not finish")
	return awsCleanupSnapshot{}
}
func TestAWSCleanupCandidateProtection(t *testing.T) {
	cases := []struct {
		name    string
		edit    func(*awsResourceView)
		records []panelRunRecord
		blocked bool
	}{
		{name: "owned missing local run", blocked: false},
		{name: "wrong owner", edit: func(i *awsResourceView) { i.Tags["Owner"] = "Another Person" }, blocked: true},
		{name: "untagged", edit: func(i *awsResourceView) { i.Tags = nil }, blocked: true},
		{name: "not Runway", edit: func(i *awsResourceView) { i.Tags["ManagedBy"] = "someone-else" }, blocked: true},
		{name: "missing run tag", edit: func(i *awsResourceView) { delete(i.Tags, "HA_Rancher_RKE2_Run_ID") }, blocked: true},
		{name: "wrong region", edit: func(i *awsResourceView) { i.Region = "us-west-2" }, blocked: true},
		{name: "recorded run", records: []panelRunRecord{{RunID: "old-run"}}, blocked: true},
		{name: "recorded prefix", edit: func(i *awsResourceView) { i.Name = "recorded-prefix-h1" }, records: []panelRunRecord{{RunID: "another", AWSPrefix: "recorded-prefix"}}, blocked: true},
		{name: "global IAM", edit: func(i *awsResourceView) { i.Type = "IAM role" }, blocked: true},
		{name: "DNS", edit: func(i *awsResourceView) { i.Type = "Route53 record" }, blocked: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := cleanupTestResource("EC2 instance", "i-test")
			if tc.edit != nil {
				tc.edit(&item)
			}
			reason := awsCleanupBlockedReason(item, "Test Owner", "us-east-2", tc.records)
			if (reason != "") != tc.blocked {
				t.Fatalf("reason = %q", reason)
			}
		})
	}
}
func TestAWSCleanupPreviewUsesFreshOwnershipAndExplicitSelection(t *testing.T) {
	a, b := cleanupTestResource("EBS volume", "vol-a"), cleanupTestResource("EBS volume", "vol-b")
	p, client := newAWSCleanupTestPanel(t, a, b)
	plan, err := p.prepareAWSCleanup(context.Background(), cleanupTestRefs(a))
	if err != nil || len(plan.Items) != 1 || plan.Items[0].Resource.ID != "vol-a" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if len(client.deleted) != 0 {
		t.Fatal("preview mutated AWS")
	}
	fresh := cleanupTestResource("EBS volume", "vol-a")
	fresh.Tags["Owner"] = "Someone Else"
	client.items[a.ID] = awsCleanupInspection{Resource: fresh}
	plan, err = p.prepareAWSCleanup(context.Background(), cleanupTestRefs(a))
	if err != nil || len(plan.Items) != 0 || len(plan.Blocked) != 1 {
		t.Fatalf("fresh ownership not enforced: %#v %v", plan, err)
	}
	for _, refs := range [][]awsCleanupRef{{{Type: "EBS volume", ID: "vol-injected"}}, cleanupTestRefs(a, a)} {
		if _, err := p.prepareAWSCleanup(context.Background(), refs); err == nil {
			t.Fatal("accepted unknown/duplicate IDs")
		}
	}
}
func TestAWSCleanupPreviewPrunesUnavailableDependencies(t *testing.T) {
	instance, volume := cleanupTestResource("EC2 instance", "i-a"), cleanupTestResource("EBS volume", "vol-a")
	p, client := newAWSCleanupTestPanel(t, instance, volume)
	inspected := client.items[volume.ID]
	inspected.Dependencies = cleanupTestRefs(instance)
	client.items[volume.ID] = inspected
	plan, err := p.prepareAWSCleanup(context.Background(), cleanupTestRefs(volume))
	if err != nil || len(plan.Items) != 0 || len(plan.Blocked) != 1 {
		t.Fatalf("attached volume allowed alone: %#v %v", plan, err)
	}
	plan, err = p.prepareAWSCleanup(context.Background(), cleanupTestRefs(volume, instance))
	if err != nil || len(plan.Items) != 2 || plan.Items[0].Resource.Type != "EC2 instance" {
		t.Fatalf("dependency order: %#v %v", plan, err)
	}
	client.inspectErrors[instance.ID] = errors.New("access denied")
	plan, err = p.prepareAWSCleanup(context.Background(), cleanupTestRefs(volume, instance))
	if err != nil || len(plan.Items) != 0 || len(plan.Blocked) != 2 {
		t.Fatalf("dependent survived blocked prerequisite: %#v %v", plan, err)
	}
}
func TestAWSCleanupRejectsStaleChangedAndUnconfirmedPlans(t *testing.T) {
	item := cleanupTestResource("EBS volume", "vol-a")
	p, _ := newAWSCleanupTestPanel(t, item)
	prepare := func() *awsCleanupPlan {
		t.Helper()
		plan, err := p.prepareAWSCleanup(context.Background(), cleanupTestRefs(item))
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	plan := prepare()
	if err := p.startAWSCleanup(plan.Token, "delete"); err == nil {
		t.Fatal("missing exact confirmation accepted")
	}
	if err := p.startAWSCleanup("wrong", plan.Confirmation); err == nil {
		t.Fatal("wrong token accepted")
	}
	plan.ExpiresAt = time.Now().Add(-time.Second)
	if err := p.startAWSCleanup(plan.Token, plan.Confirmation); err == nil {
		t.Fatal("expired plan accepted")
	}
	plan = prepare()
	viper.Set("tf_vars.aws_region", "us-west-2")
	if err := p.startAWSCleanup(plan.Token, plan.Confirmation); err == nil {
		t.Fatal("changed AWS config accepted")
	}
	viper.Set("tf_vars.aws_region", "us-east-2")
	plan = prepare()
	p.writeRunRecord(panelRunRecord{RunID: "old-run"})
	if err := p.startAWSCleanup(plan.Token, plan.Confirmation); err == nil {
		t.Fatal("newly recorded run accepted")
	}
}
func TestAWSCleanupRevalidatesWholePlanBeforeAnyDeletion(t *testing.T) {
	a, b := cleanupTestResource("EBS volume", "vol-a"), cleanupTestResource("EBS volume", "vol-b")
	p, client := newAWSCleanupTestPanel(t, a, b)
	plan, err := p.prepareAWSCleanup(context.Background(), cleanupTestRefs(a, b))
	if err != nil {
		t.Fatal(err)
	}
	changed := client.items[b.ID]
	changed.Effects = append(changed.Effects, "Delete newly attached data")
	client.items[b.ID] = changed
	if err = p.startAWSCleanup(plan.Token, plan.Confirmation); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForAWSCleanup(t, p)
	if len(client.deleted) != 0 || snapshot.Error == "" || snapshot.Results[0].Status != "blocked" {
		t.Fatalf("changed plan mutated AWS: %v %#v", client.deleted, snapshot)
	}
}
func TestAWSCleanupProcessesBatchAndReportsPartialFailure(t *testing.T) {
	lb, tg, instance, volume := cleanupTestResource("ALB", "lb-a"), cleanupTestResource("Target group", "tg-a"), cleanupTestResource("EC2 instance", "i-a"), cleanupTestResource("EBS volume", "vol-a")
	p, client := newAWSCleanupTestPanel(t, lb, tg, instance, volume)
	dep := client.items[volume.ID]
	dep.Dependencies = cleanupTestRefs(instance)
	client.items[volume.ID] = dep
	dep = client.items[tg.ID]
	dep.Dependencies = cleanupTestRefs(lb)
	client.items[tg.ID] = dep
	client.deleteErrors[instance.ID] = errors.New("termination protection is enabled")
	plan, err := p.prepareAWSCleanup(context.Background(), cleanupTestRefs(volume, instance, tg, lb))
	if err != nil {
		t.Fatal(err)
	}
	if err = p.startAWSCleanup(plan.Token, plan.Confirmation); err != nil {
		t.Fatal(err)
	}
	if err = p.startAWSCleanup(plan.Token, plan.Confirmation); err == nil {
		t.Fatal("token replay accepted")
	}
	snapshot := waitForAWSCleanup(t, p)
	if !reflect.DeepEqual(client.deleted, []string{"lb-a", "tg-a", "i-a"}) {
		t.Fatalf("delete order / failure isolation: %v", client.deleted)
	}
	if snapshot.Results[3].Status != "blocked" || snapshot.Error == "" {
		t.Fatalf("failed dependency not reported: %#v", snapshot)
	}
	p.awsMu.Lock()
	defer p.awsMu.Unlock()
	if p.awsCacheKey != "" {
		t.Fatal("inventory cache not invalidated")
	}
}
func TestAWSCleanupAlreadyAbsentIsSuccess(t *testing.T) {
	item := cleanupTestResource("EBS volume", "vol-a")
	p, client := newAWSCleanupTestPanel(t, item)
	plan, err := p.prepareAWSCleanup(context.Background(), cleanupTestRefs(item))
	if err != nil {
		t.Fatal(err)
	}
	client.inspectErrors[item.ID] = errAWSResourceGone
	if err = p.startAWSCleanup(plan.Token, plan.Confirmation); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForAWSCleanup(t, p)
	if snapshot.Error != "" || snapshot.Results[0].Status != "deleted" || len(client.deleted) != 0 {
		t.Fatalf("absent resource: %#v", snapshot)
	}
}
func TestAWSCleanupLocksOtherOperationsAndDoesNotResumeOnRestart(t *testing.T) {
	p, _ := newAWSCleanupTestPanel(t)
	p.operationLocked(panelOperationAWSCleanup).Running = true
	if !p.anyOperationRunning() {
		t.Fatal("native close guard missed cleanup")
	}
	for _, op := range []panelOperationName{panelOperationSetup, panelOperationLinodeSetup, panelOperationSteveLab, panelOperationK3DLab} {
		if !p.conflictingOperationRunningLocked(op) {
			t.Fatalf("%s not locked", op)
		}
	}
	if err := p.startCleanupBatch([]string{"anything"}); err == nil {
		t.Fatal("destroy batch ran concurrently")
	}
	p.persistOperationsLocked()
	restored := &localControlPanel{}
	restored.loadPersistedOperations(true)
	snapshot := restored.snapshotAWSCleanup()
	if snapshot.Running || snapshot.Error == "" {
		t.Fatalf("restart should require manual review: %#v", snapshot)
	}
}
func TestAWSCleanupHandlersRequireAuthMethodAndStrictBody(t *testing.T) {
	item := cleanupTestResource("EBS volume", "vol-a")
	p, _ := newAWSCleanupTestPanel(t, item)
	cases := []struct {
		method, token, body string
		status              int
	}{
		{"POST", "", `{"resources":[]}`, 403},
		{"GET", "token", `{}`, 405},
		{"POST", "token", `{"resources":[],"all":true}`, 400},
		{"POST", "token", `{"resources":[]} {}`, 400},
		{"POST", "token", `{"resources":[]}`, 400},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, "/api/aws/cleanup/preview", strings.NewReader(tc.body))
		req.Header.Set("X-Control-Panel-Token", tc.token)
		w := httptest.NewRecorder()
		p.handleAWSCleanupPreview(w, req)
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
		}
	}
	body, _ := json.Marshal(map[string]any{"resources": cleanupTestRefs(item)})
	req := httptest.NewRequest("POST", "/api/aws/cleanup/preview", strings.NewReader(string(body)))
	req.Header.Set("X-Control-Panel-Token", "token")
	w := httptest.NewRecorder()
	p.handleAWSCleanupPreview(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "delete 1 resource") || strings.Contains(w.Body.String(), "scope") {
		t.Fatalf("preview response: %s", w.Body.String())
	}
}

type cleanupHTTPClient func(*http.Request) (*http.Response, error)

func (f cleanupHTTPClient) Do(r *http.Request) (*http.Response, error) { return f(r) }
func TestAWSCleanupSDKDeletionOnlyUsesReviewedID(t *testing.T) {
	cases := []struct{ kind, id, action, param string }{
		{"EBS volume", "vol-reviewed", "DeleteVolume", "VolumeId"},
		{"ALB listener", "arn:listener:reviewed", "DeleteListener", "ListenerArn"},
		{"Target group", "arn:targetgroup:reviewed", "DeleteTargetGroup", "TargetGroupArn"},
		{"ACM certificate", "arn:certificate:reviewed", "DeleteCertificate", "CertificateArn"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			calls := 0
			cfg := aws.Config{Region: "us-east-2", Credentials: aws.AnonymousCredentials{}, HTTPClient: cleanupHTTPClient(func(req *http.Request) (*http.Response, error) {
				calls++
				body, _ := io.ReadAll(req.Body)
				payload := map[string]string{}
				if tc.kind == "ACM certificate" {
					if err := json.Unmarshal(body, &payload); err != nil {
						t.Fatal(err)
					}
					if !strings.HasSuffix(req.Header.Get("X-Amz-Target"), "."+tc.action) {
						t.Fatal("wrong ACM action")
					}
				} else {
					values, _ := url.ParseQuery(string(body))
					for key := range values {
						payload[key] = values.Get(key)
					}
					if payload["Action"] != tc.action {
						t.Fatalf("wrong action %v", payload)
					}
				}
				if payload[tc.param] != tc.id {
					t.Fatalf("wrong ID: %v", payload)
				}
				response := "<" + tc.action + "Response><" + tc.action + "Result/><return>true</return></" + tc.action + "Response>"
				if tc.kind == "ACM certificate" {
					response = "{}"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			client := &awsSDKCleanupClient{region: cfg.Region, ec2: ec2.NewFromConfig(cfg), elb: elasticloadbalancingv2.NewFromConfig(cfg), acm: acm.NewFromConfig(cfg)}
			item := cleanupTestResource(tc.kind, tc.id)
			item.Status = "available"
			if err := client.Delete(context.Background(), awsCleanupInspection{Resource: item}); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("expected one delete, got %d", calls)
			}
		})
	}
}
func TestAWSCleanupSDKWillNotDetachVolumesOrDeleteProtectedTypes(t *testing.T) {
	client := &awsSDKCleanupClient{}
	volume := cleanupTestResource("EBS volume", "vol-a")
	volume.Status = "in-use"
	if err := client.Delete(context.Background(), awsCleanupInspection{Resource: volume}); err == nil {
		t.Fatal("attached volume accepted")
	}
	if err := client.Delete(context.Background(), awsCleanupInspection{Resource: cleanupTestResource("IAM role", "role-a")}); err == nil {
		t.Fatal("protected resource accepted")
	}
	denied := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}
	if errors.Is(awsCleanupAPIError(denied), errAWSResourceGone) {
		t.Fatal("permission failure treated as absent")
	}
	if !errors.Is(awsCleanupAPIError(&smithy.GenericAPIError{Code: "InvalidVolume.NotFound"}), errAWSResourceGone) {
		t.Fatal("not found not recognized")
	}
}

func TestAWSCleanupSDKInspectionIncludesDestructiveVolumeEffects(t *testing.T) {
	xml := `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-reviewed</instanceId><instanceState><name>running</name><code>16</code></instanceState><tagSet><item><key>Owner</key><value>Test Owner</value></item><item><key>ManagedBy</key><value>rancher-runway</value></item><item><key>HA_Rancher_RKE2_Run_ID</key><value>old-run</value></item></tagSet><blockDeviceMapping><item><deviceName>/dev/sda1</deviceName><ebs><volumeId>vol-root</volumeId><deleteOnTermination>true</deleteOnTermination></ebs></item><item><deviceName>/dev/sdf</deviceName><ebs><volumeId>vol-data</volumeId><deleteOnTermination>false</deleteOnTermination></ebs></item></blockDeviceMapping></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`
	cfg := aws.Config{Region: "us-east-2", Credentials: aws.AnonymousCredentials{}, HTTPClient: cleanupHTTPClient(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("Action") != "DescribeInstances" || values.Get("InstanceId.1") != "i-reviewed" {
			t.Fatalf("inspection should only read selected instance: %s", body)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(xml))}, nil
	})}
	client := &awsSDKCleanupClient{region: cfg.Region, ec2: ec2.NewFromConfig(cfg)}
	inspected, err := client.Inspect(context.Background(), cleanupTestResource("EC2 instance", "i-reviewed"))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(inspected.Effects, "\n")
	if !strings.Contains(joined, "Permanently delete EBS volume vol-root") || !strings.Contains(joined, "Preserve EBS volume vol-data") {
		t.Fatalf("missing termination side effects: %#v", inspected)
	}
	if inspected.Resource.Owner != "Test Owner" || inspected.Resource.RunID != "old-run" {
		t.Fatalf("fresh tags not loaded: %#v", inspected.Resource)
	}
}

func TestAWSCleanupSDKWaitsForParentsWithoutDisablingProtection(t *testing.T) {
	for _, kind := range []string{"EC2 instance", "ALB"} {
		t.Run(kind, func(t *testing.T) {
			actions := []string{}
			cfg := aws.Config{Region: "us-east-2", Credentials: aws.AnonymousCredentials{}, HTTPClient: cleanupHTTPClient(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(body))
				action := values.Get("Action")
				actions = append(actions, action)
				status := 200
				response := ""
				switch action {
				case "TerminateInstances":
					if values.Get("InstanceId.1") != "i-reviewed" {
						t.Fatal("wrong instance")
					}
					response = `<TerminateInstancesResponse><instancesSet><item><instanceId>i-reviewed</instanceId><currentState><code>32</code><name>shutting-down</name></currentState></item></instancesSet></TerminateInstancesResponse>`
				case "DescribeInstances":
					response = `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-reviewed</instanceId><instanceState><code>48</code><name>terminated</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`
				case "DeleteLoadBalancer":
					if values.Get("LoadBalancerArn") != "arn:lb:reviewed" {
						t.Fatal("wrong load balancer")
					}
					response = `<DeleteLoadBalancerResponse><DeleteLoadBalancerResult/></DeleteLoadBalancerResponse>`
				case "DescribeLoadBalancers":
					status = 400
					response = `<ErrorResponse><Error><Code>LoadBalancerNotFound</Code><Message>not found</Message></Error></ErrorResponse>`
				default:
					t.Fatalf("unexpected mutation/read %s", action)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			client := &awsSDKCleanupClient{region: cfg.Region, ec2: ec2.NewFromConfig(cfg), elb: elasticloadbalancingv2.NewFromConfig(cfg)}
			id := "i-reviewed"
			want := []string{"TerminateInstances", "DescribeInstances"}
			if kind == "ALB" {
				id = "arn:lb:reviewed"
				want = []string{"DeleteLoadBalancer", "DescribeLoadBalancers"}
			}
			if err := client.Delete(context.Background(), awsCleanupInspection{Resource: cleanupTestResource(kind, id)}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actions, want) {
				t.Fatalf("wrong actions: %v", actions)
			}
		})
	}
}

func TestAWSCleanupRejectsUnreadableRunHistory(t *testing.T) {
	item := cleanupTestResource("EBS volume", "vol-a")
	p, client := newAWSCleanupTestPanel(t, item)
	if err := os.MkdirAll(p.runRecordsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.runRecordsDir(), "damaged-run.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.prepareAWSCleanup(context.Background(), cleanupTestRefs(item)); err == nil || !strings.Contains(err.Error(), "Repair the record") {
		t.Fatalf("bad record allowed cleanup: %v", err)
	}
	if len(client.deleted) != 0 {
		t.Fatal("cloud mutation despite corrupt run metadata")
	}
}
