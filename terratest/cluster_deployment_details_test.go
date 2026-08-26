package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClusterDeploymentInspectReferenceNormalizesRuntimeSchemes(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	digestC := "sha256:" + strings.Repeat("c", 64)
	digestD := "sha256:" + strings.Repeat("d", 64)
	tests := []struct {
		name     string
		declared string
		imageID  string
		wantRef  string
		wantHash string
		wantTag  string
	}{
		{
			name:     "docker pullable reference",
			declared: "stgregistry.suse.com/rancher/rancher:v2.15.1-rc1",
			imageID:  "docker-pullable://stgregistry.suse.com/rancher/rancher@" + digestA,
			wantRef:  "stgregistry.suse.com/rancher/rancher@" + digestA,
			wantHash: digestA,
			wantTag:  "v2.15.1-rc1",
		},
		{
			name:     "containerd digest uses declared Docker Hub repository",
			declared: "rancher/rancher:v2.15.2",
			imageID:  "containerd://" + digestB,
			wantRef:  "docker.io/rancher/rancher@" + digestB,
			wantHash: digestB,
			wantTag:  "v2.15.2",
		},
		{
			name:     "registry port is not confused with tag",
			declared: "registry.example.test:5000/team/rancher:v2.16.0-rc3",
			imageID:  "containerd://" + digestC,
			wantRef:  "registry.example.test:5000/team/rancher@" + digestC,
			wantHash: digestC,
			wantTag:  "v2.16.0-rc3",
		},
		{
			name:     "runtime repository is preserved across a registry rewrite",
			declared: "stgregistry.suse.com/rancher/rancher:v2.15.1-rc1",
			imageID:  "docker-pullable://registry.internal.example/rancher/rancher@" + digestD,
			wantRef:  "registry.internal.example/rancher/rancher@" + digestD,
			wantHash: digestD,
			wantTag:  "v2.15.1-rc1",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			gotRef, gotHash := clusterDeploymentInspectReference(testCase.declared, testCase.imageID)
			if gotRef != testCase.wantRef || gotHash != testCase.wantHash {
				t.Fatalf("clusterDeploymentInspectReference(%q, %q) = (%q, %q), want (%q, %q)", testCase.declared, testCase.imageID, gotRef, gotHash, testCase.wantRef, testCase.wantHash)
			}
			if gotTag := clusterDeploymentImageVersion(testCase.declared); gotTag != testCase.wantTag {
				t.Fatalf("clusterDeploymentImageVersion(%q) = %q, want %q", testCase.declared, gotTag, testCase.wantTag)
			}
		})
	}
}

func TestClusterDeploymentImagesFromPodsDeduplicatesReplicasButPreservesRolloutDigests(t *testing.T) {
	oldDigest := "sha256:" + strings.Repeat("1", 64)
	newDigest := "sha256:" + strings.Repeat("2", 64)
	declared := "stgregistry.suse.com/rancher/rancher:v2.15.1-rc1"
	runtimeID := func(digest string) string {
		return "docker-pullable://stgregistry.suse.com/rancher/rancher@" + digest
	}
	pods := []podView{
		{
			Namespace: "cattle-system",
			Name:      "rancher-old-not-ready",
			Images: []containerImageView{{
				Name: "rancher", Image: declared, ImageID: runtimeID(oldDigest), Ready: false,
			}},
		},
		{
			Namespace: "cattle-system",
			Name:      "rancher-old-ready",
			Images: []containerImageView{{
				Name: "rancher", Image: declared, ImageID: runtimeID(oldDigest), Ready: true,
			}},
		},
		{
			Namespace: "cattle-system",
			Name:      "rancher-new",
			Images: []containerImageView{{
				Name: "rancher", Image: declared, ImageID: runtimeID(newDigest), Ready: true,
			}},
		},
	}

	images := clusterDeploymentImagesFromPods(pods)
	if len(images) != 2 {
		t.Fatalf("expected one old and one new rollout image, got %#v", images)
	}
	byDigest := make(map[string]clusterDeploymentImage, len(images))
	for _, image := range images {
		byDigest[image.Digest] = image
	}
	oldImage, ok := byDigest[oldDigest]
	if !ok || !oldImage.Ready || oldImage.Pod != "rancher-old-ready" {
		t.Fatalf("expected ready old-digest representative, got %#v", oldImage)
	}
	if newImage, ok := byDigest[newDigest]; !ok || newImage.Pod != "rancher-new" {
		t.Fatalf("expected new rollout digest to remain visible, got %#v", newImage)
	}
}

func TestClusterDeploymentVersionParsers(t *testing.T) {
	t.Run("Rancher Setting value", func(t *testing.T) {
		got, err := parseRancherServerVersionSettingJSON([]byte(`{"value":" v2.15.1-rc1+up0.11.1 ","default":"v2.15.0"}`))
		if err != nil || got != "v2.15.1-rc1+up0.11.1" {
			t.Fatalf("got version %q, error %v", got, err)
		}
	})
	t.Run("Rancher Setting default fallback", func(t *testing.T) {
		got, err := parseRancherServerVersionSettingJSON([]byte(`{"value":"","default":"v2.15.0"}`))
		if err != nil || got != "v2.15.0" {
			t.Fatalf("got version %q, error %v", got, err)
		}
	})
	t.Run("Kubernetes server GitVersion", func(t *testing.T) {
		got, err := parseKubernetesServerVersionJSON([]byte(`{"clientVersion":{"gitVersion":"v1.35.0"},"serverVersion":{"gitVersion":"v1.36.1+k3s1"}}`))
		if err != nil || got != "v1.36.1+k3s1" {
			t.Fatalf("got version %q, error %v", got, err)
		}
	})
	t.Run("rancher-webhook App chart", func(t *testing.T) {
		got, err := parseRancherWebhookChartVersionJSON([]byte(`{"spec":{"chart":{"metadata":{"version":"110.0.2+up0.11.1"}}}}`))
		if err != nil || got != "110.0.2+up0.11.1" {
			t.Fatalf("got version %q, error %v", got, err)
		}
	})
	t.Run("empty values fail", func(t *testing.T) {
		if _, err := parseRancherServerVersionSettingJSON([]byte(`{}`)); err == nil {
			t.Fatal("expected empty Rancher Setting to fail")
		}
		if _, err := parseKubernetesServerVersionJSON([]byte(`{}`)); err == nil {
			t.Fatal("expected missing Kubernetes server version to fail")
		}
		if _, err := parseRancherWebhookChartVersionJSON([]byte(`{}`)); err == nil {
			t.Fatal("expected empty webhook App version to fail")
		}
	})
}

func TestCollectClusterDeploymentDetailsForLinodeDoesNotProbeSSH(t *testing.T) {
	details := (&localControlPanel{}).collectClusterDeploymentDetails(context.Background(), clusterView{
		ID:             "linode-docker-1",
		Type:           "linode",
		DeploymentType: deploymentTypeLinodeDocker,
		Version:        "2.15.1-rc1",
		LoadBalancer:   "192.0.2.10",
		Reachable:      true,
	})

	if details.ConfiguredVersion != "2.15.1-rc1" {
		t.Fatalf("configured version = %q, want %q", details.ConfiguredVersion, "2.15.1-rc1")
	}
	if len(details.Images) != 0 {
		t.Fatalf("expected no unverified runtime image metadata, got %#v", details.Images)
	}
	if len(details.Warnings) != 1 || !strings.Contains(details.Warnings[0], "cannot verify the SSH host identity") {
		t.Fatalf("expected host identity warning, got %#v", details.Warnings)
	}
}

func TestKubeconfigBackedManagementClusterExcludesDownstream(t *testing.T) {
	if isKubeconfigBackedManagementCluster(clusterView{Type: "downstream", KubeconfigPath: "/tmp/downstream.yaml"}) {
		t.Fatal("downstream clusters must not run Rancher server or webhook probes")
	}
	if !isKubeconfigBackedManagementCluster(clusterView{Type: "local", KubeconfigPath: "/tmp/local.yaml"}) {
		t.Fatal("local management cluster should run Rancher server and webhook probes")
	}
}

func TestHandleClusterDeploymentDetailsGuardsRequest(t *testing.T) {
	panel := &localControlPanel{token: "secret"}

	t.Run("authorization", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/clusters/details?cluster=local-ha-1", nil)
		request.RemoteAddr = "198.51.100.10:1234"
		response := httptest.NewRecorder()
		panel.handleClusterDeploymentDetails(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
		}
	})

	t.Run("GET only", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/clusters/details?cluster=local-ha-1", nil)
		request.RemoteAddr = "198.51.100.10:1234"
		request.Header.Set("X-Control-Panel-Token", "secret")
		response := httptest.NewRecorder()
		panel.handleClusterDeploymentDetails(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
		}
		if response.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("Allow = %q, want GET", response.Header().Get("Allow"))
		}
	})

	t.Run("cluster is required", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/clusters/details", nil)
		request.RemoteAddr = "198.51.100.10:1234"
		request.Header.Set("X-Control-Panel-Token", "secret")
		response := httptest.NewRecorder()
		panel.handleClusterDeploymentDetails(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
		}
	})

	t.Run("uses the state snapshot without rediscovery", func(t *testing.T) {
		panel.rememberClusterSnapshot([]clusterView{{
			ID:             "linode-docker-1",
			Type:           "linode",
			DeploymentType: deploymentTypeLinodeDocker,
			Version:        "2.15.1-rc1",
		}})
		request := httptest.NewRequest(http.MethodGet, "/api/clusters/details?cluster=linode-docker-1", nil)
		request.RemoteAddr = "198.51.100.10:1234"
		request.Header.Set("X-Control-Panel-Token", "secret")
		response := httptest.NewRecorder()
		panel.handleClusterDeploymentDetails(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var details clusterDeploymentDetailsResponse
		if err := json.Unmarshal(response.Body.Bytes(), &details); err != nil {
			t.Fatalf("failed to decode deployment details: %v", err)
		}
		if details.ClusterID != "linode-docker-1" || details.ConfiguredVersion != "2.15.1-rc1" {
			t.Fatalf("unexpected cached deployment details: %#v", details)
		}
	})
}
