package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

const compatibilityMachineSetting = "/v3/settings/machine-provision-image"

type compatibilitySettingFixture struct {
	t            *testing.T
	value        string
	defaultValue string
	registry     string
	putValues    []string
	inspected    []string
	putErr       error
	inspectErr   error
	missingImage bool
	staleRead    bool
	settingReads int
}

func (f *compatibilitySettingFixture) call(method, path string, payload, out any) error {
	f.t.Helper()
	switch {
	case method == http.MethodGet && path == compatibilityMachineSetting:
		f.settingReads++
		return compatibilityDecode(out, map[string]any{"value": f.value, "default": f.defaultValue})
	case method == http.MethodGet && path == "/v3/settings/system-default-registry":
		return compatibilityDecode(out, map[string]any{"value": f.registry, "default": ""})
	case method == http.MethodPut && path == compatibilityMachineSetting:
		var update struct {
			Value string `json:"value"`
		}
		if err := compatibilityDecode(&update, payload); err != nil {
			f.t.Fatal(err)
		}
		if len(f.inspected) == 0 {
			f.t.Fatal("setting changed before replacement image was checked")
		}
		f.putValues = append(f.putValues, update.Value)
		if f.putErr != nil {
			return f.putErr
		}
		if !f.staleRead {
			f.value = update.Value
		}
		if out != nil {
			return compatibilityDecode(out, map[string]any{"value": f.value, "default": f.defaultValue})
		}
		return nil
	default:
		f.t.Fatalf("unexpected request: %s %s", method, path)
		return nil
	}
}

func (f *compatibilitySettingFixture) inspect(_ context.Context, reference string) (imagelookup.Provenance, bool, error) {
	f.inspected = append(f.inspected, reference)
	return imagelookup.Provenance{Digest: "sha256:fixture"}, !f.missingImage, f.inspectErr
}

func compatibilityDecode(out, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func TestDownstreamProvisionerCompatibilityRepairsKnownImages(t *testing.T) {
	cases := []struct {
		name, image, defaultImage, registry, replacement string
	}{
		{"rancher-215", "rancher/machine:v0.15.0-rancher145.4-rc.1", "", "", "rancher/machine:v0.15.0-rancher145.3"},
		{"rancher-214", "rancher/machine:v0.15.0-rancher142.8-rc.1", "", "", "rancher/machine:v0.15.0-rancher142.7"},
		{"empty-value-uses-default", "", "rancher/machine:v0.15.0-rancher145.4-rc.1", "", "rancher/machine:v0.15.0-rancher145.3"},
		{"staging-system-registry", "rancher/machine:v0.15.0-rancher145.4-rc.1", "", "stgregistry.suse.com", "rancher/machine:v0.15.0-rancher145.3"},
		{"private-system-registry", "rancher/machine:v0.15.0-rancher142.8-rc.1", "", "mirror.example.test", "rancher/machine:v0.15.0-rancher142.7"},
		{"docker-qualified", "docker.io/rancher/machine:v0.15.0-rancher145.4-rc.1", "", "", "docker.io/rancher/machine:v0.15.0-rancher145.3"},
		{"prime-qualified", "registry.rancher.com/rancher/machine:v0.15.0-rancher142.8-rc.1", "", "", "registry.rancher.com/rancher/machine:v0.15.0-rancher142.7"},
		{"staging-qualified", "stgregistry.suse.com/rancher/machine:v0.15.0-rancher145.4-rc.1", "", "", "stgregistry.suse.com/rancher/machine:v0.15.0-rancher145.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &compatibilitySettingFixture{t: t, value: tc.image, defaultValue: tc.defaultImage, registry: tc.registry}
			message, err := ensureDownstreamProvisionerCompatibility(context.Background(), fixture.call, fixture.inspect)
			if err != nil {
				t.Fatal(err)
			}
			if len(fixture.putValues) != 1 || fixture.putValues[0] != tc.replacement {
				t.Fatalf("setting updates = %v; want only %q", fixture.putValues, tc.replacement)
			}
			wantedReference := tc.replacement
			if tc.registry != "" {
				wantedReference = tc.registry + "/" + tc.replacement
			}
			if len(fixture.inspected) != 1 || fixture.inspected[0] != wantedReference {
				t.Fatalf("inspected = %v; want effective image %q", fixture.inspected, wantedReference)
			}
			if fixture.settingReads < 2 {
				t.Fatal("setting update was not read back")
			}
			oldImage := tc.image
			if oldImage == "" {
				oldImage = tc.defaultImage
			}
			if !strings.Contains(message, oldImage) || !strings.Contains(message, tc.replacement) {
				t.Fatalf("message should identify old and new images: %q", message)
			}
			if !strings.Contains(strings.ToLower(message), "server-wide") || !strings.Contains(strings.ToLower(message), "future") {
				t.Fatalf("message should disclose server-wide effect on future jobs: %q", message)
			}
			message, err = ensureDownstreamProvisionerCompatibility(context.Background(), fixture.call, fixture.inspect)
			if err != nil || message != "" || len(fixture.putValues) != 1 || len(fixture.inspected) != 1 {
				t.Fatalf("retry should leave repaired setting alone: message=%q err=%v writes=%v inspected=%v", message, err, fixture.putValues, fixture.inspected)
			}
		})
	}
}

func TestDownstreamProvisionerCompatibilityPreservesOtherImages(t *testing.T) {
	const brokenDefault = "rancher/machine:v0.15.0-rancher145.4-rc.1"
	for _, value := range []string{
		"rancher/machine:v0.15.0-rancher146-rc.5",
		"rancher/machine:v0.15.0-rancher145.4-rc.2",
		"rancher/machine:v0.15.0-rancher142.8",
		"rancher/machine:v0.15.0-rancher145.3",
		"custom.example.test/rancher/machine:v0.15.0-rancher145.4-rc.1",
		"docker.io/custom/machine:v0.15.0-rancher145.4-rc.1",
		"rancher/machine:v0.15.0-rancher145.4-rc.1@sha256:" + strings.Repeat("a", 64),
		"rancher/machine@sha256:" + strings.Repeat("b", 64),
	} {
		t.Run(value, func(t *testing.T) {
			fixture := &compatibilitySettingFixture{t: t, value: value, defaultValue: brokenDefault}
			message, err := ensureDownstreamProvisionerCompatibility(context.Background(), fixture.call, fixture.inspect)
			if err != nil || message != "" || len(fixture.putValues) != 0 || len(fixture.inspected) != 0 {
				t.Fatalf("unaffected override changed: message=%q err=%v writes=%v inspected=%v", message, err, fixture.putValues, fixture.inspected)
			}
		})
	}
}

func TestDownstreamProvisionerCompatibilityStopsWhenReplacementCannotBeVerified(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			fixture := &compatibilitySettingFixture{t: t, value: "rancher/machine:v0.15.0-rancher145.4-rc.1", missingImage: missing}
			if !missing {
				fixture.inspectErr = errors.New("registry unavailable")
			}
			_, err := ensureDownstreamProvisionerCompatibility(context.Background(), fixture.call, fixture.inspect)
			if err == nil || len(fixture.putValues) != 0 {
				t.Fatalf("unverified replacement must prevent writes: err=%v writes=%v", err, fixture.putValues)
			}
		})
	}
}

func TestDownstreamProvisionerCompatibilityStopsOnWriteFailure(t *testing.T) {
	fixture := &compatibilitySettingFixture{t: t, value: "rancher/machine:v0.15.0-rancher142.8-rc.1", putErr: errors.New("setting update forbidden")}
	_, err := ensureDownstreamProvisionerCompatibility(context.Background(), fixture.call, fixture.inspect)
	if err == nil || len(fixture.putValues) != 1 {
		t.Fatalf("failed setting update must fail provisioning: err=%v writes=%v", err, fixture.putValues)
	}
}

func TestDownstreamProvisionerCompatibilityRequiresReadBack(t *testing.T) {
	fixture := &compatibilitySettingFixture{t: t, value: "rancher/machine:v0.15.0-rancher142.8-rc.1", staleRead: true}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := ensureDownstreamProvisionerCompatibility(ctx, fixture.call, fixture.inspect)
	if err == nil || len(fixture.putValues) != 1 || fixture.settingReads < 2 {
		t.Fatalf("stale read-back must fail without repeated writes: err=%v writes=%v reads=%d", err, fixture.putValues, fixture.settingReads)
	}
}

func TestDownstreamProvisionerCompatibilityRunsBeforeUIResourceCreation(t *testing.T) {
	t.Setenv(machineProvisionerWorkaroundEnv, "true")
	for _, scenario := range []string{"verified", "missing-image", "setting-forbidden"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
			const broken = "rancher/machine:v0.15.0-rancher145.4-rc.1"
			const replacement = "rancher/machine:v0.15.0-rancher145.3"
			var mu sync.Mutex
			setting := broken
			confirmed := false
			settingWrites, providerWrites, inspections := 0, 0, 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("missing Rancher authorization")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch {
				case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "provisioning.cattle.io.cluster/"):
					w.WriteHeader(http.StatusNotFound)
				case r.Method == http.MethodGet && r.URL.Path == compatibilityMachineSetting:
					confirmed = setting == replacement
					_ = json.NewEncoder(w).Encode(map[string]string{"value": setting})
				case r.Method == http.MethodGet && r.URL.Path == "/v3/settings/system-default-registry":
					fmt.Fprint(w, `{"value":"stgregistry.suse.com"}`)
				case r.Method == http.MethodPut && r.URL.Path == compatibilityMachineSetting:
					settingWrites++
					if inspections == 0 {
						t.Error("setting PUT preceded replacement verification")
					}
					if scenario == "setting-forbidden" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					var update struct{ Value string }
					if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
						t.Error(err)
					}
					if update.Value != replacement {
						t.Errorf("replacement = %q", update.Value)
					}
					setting = update.Value
					fmt.Fprint(w, `{}`)
				case r.Method == http.MethodPost:
					providerWrites++
					if r.URL.Path != "/v3/cloudcredentials" || setting != replacement || !confirmed {
						t.Errorf("provider creation preceded confirmed repair: path=%s setting=%s confirmed=%t", r.URL.Path, setting, confirmed)
					}
					// Stop at the first provider write; this test must never provision a cluster.
					w.WriteHeader(http.StatusTeapot)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			panel := &localControlPanel{}
			err := panel.runDeployedDownstream(clusterView{RancherURL: server.URL}, rancherOperationRequest{Name: "qa", Provider: "linode", Insecure: true, Token: "fixture-token"}, &rancherOperationRecord{ID: operationID()}, downstreamProvisioningRuntime{inspect: func(_ context.Context, reference string) (imagelookup.Provenance, bool, error) {
				mu.Lock()
				defer mu.Unlock()
				inspections++
				if reference != "stgregistry.suse.com/"+replacement {
					t.Errorf("unexpected image inspection: %q", reference)
				}
				return imagelookup.Provenance{Digest: "sha256:fixture"}, scenario != "missing-image", nil
			}})
			if err == nil {
				t.Fatal("fixture should stop before successful resource creation")
			}
			mu.Lock()
			defer mu.Unlock()
			switch scenario {
			case "verified":
				if settingWrites != 1 || providerWrites != 1 || !strings.Contains(err.Error(), "418") {
					t.Fatalf("verified repair did not reach the provider write: err=%v setting writes=%d provider writes=%d", err, settingWrites, providerWrites)
				}
			case "missing-image":
				if settingWrites != 0 || providerWrites != 0 {
					t.Fatalf("missing image allowed writes: settings=%d provider=%d", settingWrites, providerWrites)
				}
			case "setting-forbidden":
				if settingWrites != 1 || providerWrites != 0 {
					t.Fatalf("rejected setting allowed provider creation: settings=%d provider=%d", settingWrites, providerWrites)
				}
			}
		})
	}
}

func TestDownstreamProvisionerCompatibilityLifecyclePreservesHealthyImage(t *testing.T) {
	t.Setenv(machineProvisionerWorkaroundEnv, "true")
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != compatibilityMachineSetting || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("unexpected lifecycle request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"value":"rancher/machine:v0.15.0-rancher146-rc.5"}`)
	}))
	defer server.Close()
	message, err := prepareLinodeDownstreamProvisioner(context.Background(), server.URL, "fixture-token")
	if err != nil || message != "" || requests.Load() != 1 {
		t.Fatalf("healthy lifecycle image should require only one read: message=%q err=%v requests=%d", message, err, requests.Load())
	}
}

func TestDownstreamProvisionerCompatibilityIsOptIn(t *testing.T) {
	for _, value := range []string{"", "false", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(machineProvisionerWorkaroundEnv, value)
			fixture := &compatibilitySettingFixture{t: t, value: "rancher/machine:v0.15.0-rancher145.4-rc.1"}
			message, err := prepareDownstreamProvisionerCompatibility(context.Background(), fixture.call, fixture.inspect)
			if err != nil || message != "" || fixture.settingReads != 0 || len(fixture.putValues) != 0 || len(fixture.inspected) != 0 {
				t.Fatalf("default run must preserve Rancher's provisioner: message=%q err=%v fixture=%+v", message, err, fixture)
			}
		})
	}
}
