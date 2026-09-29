package test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

const (
	iamTestRoleARN    = "arn:aws:iam::123456789012:role/run-ssm-role"
	iamTestProfileARN = "arn:aws:iam::123456789012:instance-profile/run-ssm-profile"
	iamTestPolicyARN  = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
	iamTestTrust      = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
)

type iamCleanupFixture struct {
	mu                                                           sync.Mutex
	calls, mutations                                             []string
	trust, profileOwner, profileRoleOwner                        string
	usedRegion, usedState, deniedRegion                          string
	inline, missingProfile, missingRole, emptyRegions, paginated bool
}

func iamTestTags(owner string) string {
	return `<Tags><member><Key>Owner</Key><Value>` + owner + `</Value></member><member><Key>ManagedBy</Key><Value>rancher-runway</Value></member><member><Key>HA_Rancher_RKE2_Run_ID</Key><Value>old-run</Value></member></Tags>`
}

func iamTestResource(kind string) awsResourceView {
	id := iamTestRoleARN
	if kind == "IAM instance profile" {
		id = iamTestProfileARN
	}
	if kind == "IAM policy attachment" {
		id += ":" + iamTestPolicyARN
	}
	item := cleanupTestResource(kind, id)
	item.Region = "global"
	return item
}

func newIAMCleanupSDKFixture(t *testing.T, fixture *iamCleanupFixture) *awsSDKCleanupClient {
	t.Helper()
	if fixture.trust == "" {
		fixture.trust = iamTestTrust
	}
	if fixture.profileOwner == "" {
		fixture.profileOwner = "Test Owner"
	}
	if fixture.usedState == "" {
		fixture.usedState = "stopped"
	}
	if fixture.profileRoleOwner == "" {
		fixture.profileRoleOwner = "Test Owner"
	}
	roleXML := func(owner string) string {
		return `<Role><Path>/</Path><RoleName>run-ssm-role</RoleName><RoleId>ARO-original</RoleId><Arn>` + iamTestRoleARN + `</Arn><AssumeRolePolicyDocument>` + url.QueryEscape(fixture.trust) + `</AssumeRolePolicyDocument>` + iamTestTags(owner) + `</Role>`
	}
	profileXML := func() string {
		member := strings.TrimSuffix(strings.TrimPrefix(roleXML("Test Owner"), "<Role>"), "</Role>")
		return `<Path>/</Path><InstanceProfileName>run-ssm-profile</InstanceProfileName><InstanceProfileId>AIP-original</InstanceProfileId><Arn>` + iamTestProfileARN + `</Arn>` + iamTestTags(fixture.profileOwner) + `<Roles><member>` + member + `</member></Roles>`
	}
	cfg := aws.Config{Region: "us-east-2", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1, HTTPClient: cleanupHTTPClient(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		action := values.Get("Action")
		region := "us-east-2"
		if strings.Contains(req.URL.Host, "us-west-2") {
			region = "us-west-2"
		}
		fixture.mu.Lock()
		fixture.calls = append(fixture.calls, action+":"+region)
		fixture.mu.Unlock()
		status, result := 200, ""
		if fixture.deniedRegion == region && action == "DescribeInstances" {
			status, result = 403, `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>denied</Message></Error></Errors></Response>`
		} else {
			switch action {
			case "GetRole":
				if values.Get("RoleName") != "run-ssm-role" {
					return nil, fmt.Errorf("wrong role: %v", values)
				}
				if fixture.missingRole {
					status, result = 404, `<ErrorResponse><Error><Code>NoSuchEntity</Code><Message>gone</Message></Error></ErrorResponse>`
				} else {
					result = roleXML(fixture.profileRoleOwner)
				}
			case "GetInstanceProfile":
				if values.Get("InstanceProfileName") != "run-ssm-profile" {
					return nil, fmt.Errorf("wrong profile: %v", values)
				}
				if fixture.missingProfile {
					status, result = 404, `<ErrorResponse><Error><Code>NoSuchEntity</Code><Message>gone</Message></Error></ErrorResponse>`
				} else {
					result = `<InstanceProfile>` + profileXML() + `</InstanceProfile>`
				}
			case "ListInstanceProfilesForRole":
				if fixture.paginated && values.Get("Marker") == "" {
					result = `<InstanceProfiles/><IsTruncated>true</IsTruncated><Marker>profiles-2</Marker>`
				} else {
					result = `<InstanceProfiles><member>` + profileXML() + `</member></InstanceProfiles><IsTruncated>false</IsTruncated>`
				}
			case "ListAttachedRolePolicies":
				if fixture.paginated && values.Get("Marker") == "" {
					result = `<AttachedPolicies/><IsTruncated>true</IsTruncated><Marker>policies-2</Marker>`
				} else {
					result = `<AttachedPolicies><member><PolicyName>AmazonSSMManagedInstanceCore</PolicyName><PolicyArn>` + iamTestPolicyARN + `</PolicyArn></member></AttachedPolicies><IsTruncated>false</IsTruncated>`
				}
			case "ListRolePolicies":
				result = `<PolicyNames/><IsTruncated>false</IsTruncated>`
				if fixture.inline {
					result = `<PolicyNames><member>custom</member></PolicyNames><IsTruncated>false</IsTruncated>`
				}
			case "DescribeRegions":
				result = `<regionInfo><item><regionName>us-east-2</regionName></item><item><regionName>us-west-2</regionName></item></regionInfo>`
				if fixture.emptyRegions {
					result = `<regionInfo/>`
				}
			case "DescribeInstances":
				if values.Get("Filter.1.Name") != "iam-instance-profile.arn" || values.Get("Filter.1.Value.1") != iamTestProfileARN {
					return nil, fmt.Errorf("unscoped instance read: %v", values)
				}
				result = `<reservationSet/>`
				if fixture.paginated && values.Get("NextToken") == "" {
					result += `<nextToken>instances-2</nextToken>`
				} else if fixture.usedRegion == region {
					result = `<reservationSet><item><instancesSet><item><instanceId>i-stopped</instanceId><instanceState><name>` + fixture.usedState + `</name></instanceState></item></instancesSet></item></reservationSet>`
				}
			case "RemoveRoleFromInstanceProfile", "DeleteInstanceProfile", "DetachRolePolicy", "DeleteRole":
				if action != "DeleteInstanceProfile" && values.Get("RoleName") != "run-ssm-role" {
					return nil, fmt.Errorf("unexpected role mutation: %v", values)
				}
				if strings.Contains(action, "InstanceProfile") && values.Get("InstanceProfileName") != "run-ssm-profile" {
					return nil, fmt.Errorf("unexpected profile mutation: %v", values)
				}
				if action == "DetachRolePolicy" && values.Get("PolicyArn") != iamTestPolicyARN {
					return nil, fmt.Errorf("unexpected policy mutation: %v", values)
				}
				fixture.mu.Lock()
				fixture.mutations = append(fixture.mutations, action)
				fixture.mu.Unlock()
			case "ListInstanceProfiles":
				result = `<InstanceProfiles><member>` + profileXML() + `</member></InstanceProfiles><IsTruncated>false</IsTruncated>`
			case "ListInstanceProfileTags":
				result = iamTestTags(fixture.profileOwner)
			default:
				return nil, fmt.Errorf("unexpected AWS action: %s", action)
			}
		}
		if status == 200 {
			if strings.HasPrefix(action, "Describe") {
				result = "<" + action + "Response>" + result + "</" + action + "Response>"
			} else {
				result = "<" + action + "Response><" + action + "Result>" + result + "</" + action + "Result></" + action + "Response>"
			}
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(result))}, nil
	})}
	return &awsSDKCleanupClient{region: cfg.Region, iam: iam.NewFromConfig(cfg), ec2: ec2.NewFromConfig(cfg), ec2ForRegion: func(region string) *ec2.Client {
		return ec2.NewFromConfig(cfg, func(o *ec2.Options) { o.Region = region })
	}}
}

func TestAWSCleanupIAMTrustRestriction(t *testing.T) {
	for _, policy := range []string{iamTestTrust, url.QueryEscape(iamTestTrust), strings.ReplaceAll(iamTestTrust, `"ec2.amazonaws.com"`, `["ec2.amazonaws.com"]`)} {
		if !iamEC2OnlyTrust(policy) {
			t.Fatalf("valid EC2 trust rejected: %s", policy)
		}
	}
	for _, policy := range []string{`{}`, `broken`, strings.ReplaceAll(iamTestTrust, "ec2.amazonaws.com", "lambda.amazonaws.com"), strings.ReplaceAll(iamTestTrust, `"Service":"ec2.amazonaws.com"`, `"Service":"ec2.amazonaws.com","AWS":"*"`), strings.ReplaceAll(iamTestTrust, "sts:AssumeRole", "sts:*"), strings.ReplaceAll(iamTestTrust, "Principal", "NotPrincipal")} {
		if iamEC2OnlyTrust(policy) {
			t.Fatalf("unsafe trust accepted: %s", policy)
		}
	}
}

func TestAWSCleanupIAMReviewAndExactMutations(t *testing.T) {
	for _, kind := range []string{"IAM role", "IAM instance profile", "IAM policy attachment"} {
		t.Run(kind, func(t *testing.T) {
			fixture := &iamCleanupFixture{paginated: true}
			client := newIAMCleanupSDKFixture(t, fixture)
			inspection, err := client.Inspect(context.Background(), iamTestResource(kind))
			if err != nil {
				t.Fatal(err)
			}
			if len(fixture.mutations) != 0 {
				t.Fatal("review mutated AWS")
			}
			if !strings.Contains(strings.Join(inspection.Checks, " "), "all 2 enabled") {
				t.Fatalf("missing regional verification: %#v", inspection)
			}
			if inspection.Resource.Region != "global" || inspection.Resource.Owner != "Test Owner" || inspection.Resource.RunID != "old-run" {
				t.Fatalf("missing fresh ownership: %#v", inspection.Resource)
			}
			want := []string{"RemoveRoleFromInstanceProfile", "DeleteInstanceProfile", "DetachRolePolicy", "DeleteRole"}
			if kind == "IAM instance profile" {
				want = want[:2]
			}
			if kind == "IAM policy attachment" {
				want = []string{"DetachRolePolicy"}
			}
			if kind != "IAM instance profile" && !strings.Contains(strings.Join(inspection.Effects, " "), "managed policy itself is retained") {
				t.Fatal("managed policy retention not explained")
			}
			if err := client.Delete(context.Background(), inspection); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fixture.mutations, want) {
				t.Fatalf("mutations = %v, want %v", fixture.mutations, want)
			}
		})
	}
}

func TestAWSCleanupIAMRejectsUnverifiedOrUsedResources(t *testing.T) {
	cases := []struct {
		name, kind, want string
		fixture          iamCleanupFixture
	}{
		{"stopped instance in another region", "IAM role", "i-stopped", iamCleanupFixture{usedRegion: "us-west-2", paginated: true}},
		{"pending instance in another region", "IAM instance profile", "i-stopped", iamCleanupFixture{usedRegion: "us-west-2", usedState: "pending", paginated: true}},
		{"cannot verify all regions", "IAM policy attachment", "Cannot verify", iamCleanupFixture{deniedRegion: "us-west-2"}},
		{"no enabled region response", "IAM role", "enabled AWS regions", iamCleanupFixture{emptyRegions: true}},
		{"inline policies", "IAM role", "inline policies", iamCleanupFixture{inline: true}},
		{"profile owned by someone else", "IAM role", "ownership tags", iamCleanupFixture{profileOwner: "Someone Else"}},
		{"policy role profile owned by someone else", "IAM policy attachment", "ownership tags", iamCleanupFixture{profileOwner: "Someone Else"}},
		{"profile role owned by someone else", "IAM instance profile", "ownership tags", iamCleanupFixture{profileRoleOwner: "Someone Else"}},
		{"non EC2 trust", "IAM role", "exclusively by EC2", iamCleanupFixture{trust: strings.ReplaceAll(iamTestTrust, "ec2.amazonaws.com", "lambda.amazonaws.com")}},
		{"associated profile disappeared", "IAM role", "associated instance profile", iamCleanupFixture{missingProfile: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newIAMCleanupSDKFixture(t, &tc.fixture)
			_, err := client.Inspect(context.Background(), iamTestResource(tc.kind))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %s", err, tc.want)
			}
			if errors.Is(err, errAWSResourceGone) {
				t.Fatal("a dependency or verification error must not mark the selected resource deleted")
			}
			if len(tc.fixture.mutations) != 0 {
				t.Fatal("failed review mutated AWS")
			}
		})
	}
}

func TestAWSCleanupIAMAbsentRootAndRecreatedIdentity(t *testing.T) {
	client := newIAMCleanupSDKFixture(t, &iamCleanupFixture{missingRole: true})
	if _, err := client.Inspect(context.Background(), iamTestResource("IAM role")); !errors.Is(err, errAWSResourceGone) {
		t.Fatalf("expected absent root, got %v", err)
	}
	item := iamTestResource("IAM role")
	p, fake := newAWSCleanupTestPanel(t, item)
	inspected := fake.items[item.ID]
	inspected.Identities = map[string]string{item.ID: "original"}
	fake.items[item.ID] = inspected
	plan, err := p.prepareAWSCleanup(context.Background(), cleanupTestRefs(item))
	if err != nil {
		t.Fatal(err)
	}
	inspected.Identities = map[string]string{item.ID: "recreated"}
	fake.items[item.ID] = inspected
	if err := p.startAWSCleanup(plan.Token, plan.Confirmation); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForAWSCleanup(t, p)
	if len(fake.deleted) != 0 || snapshot.Results[0].Status != "blocked" {
		t.Fatalf("recreated resource was deleted: %#v", snapshot)
	}
}

func TestAWSInventoryIAMProfilesScanIndependently(t *testing.T) {
	fixture := &iamCleanupFixture{}
	client := newIAMCleanupSDKFixture(t, fixture)
	collector := &awsInventoryCollector{state: &panelAWSInventoryState{}, owner: "Test Owner", seen: map[string]bool{}}
	collector.collectIAMProfiles(context.Background(), client.iam)
	if len(collector.errors) != 0 || len(collector.state.Items) != 1 || collector.state.Items[0].Type != "IAM instance profile" {
		t.Fatalf("profile scan failed: %#v %v", collector.state.Items, collector.errors)
	}
	for _, call := range fixture.calls {
		if strings.HasPrefix(call, "ListRoles:") {
			t.Fatal("profile scan must not wait behind role scan")
		}
	}
}
