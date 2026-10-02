package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDeployedDownstreamDefaults(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/settings/k3s-default-version":
			fmt.Fprint(w, `{"value":"v1.36.5+k3s1"}`)
		case "/v3/schemas/linodeConfig":
			fmt.Fprint(w, `{"resourceFields":{"sshUser":{"default":"root"},"createPrivateIp":{"default":true},"unrecognized":{"default":"ignore"}}}`)
		default:
			t.Errorf("unexpected default read %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("tf_vars:\n  aws_prefix: atb\nrancher:\n  bootstrap_password: SECRET\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &localControlPanel{configPath: path}
	result := deployedDownstreamCatalog{Versions: []string{"v1.37.1+k3s1", "v1.36.5+k3s1"}, Fields: map[string]deployedMachineField{"sshUser": {Type: "string"}, "createPrivateIp": {Type: "boolean", Default: false}}}
	p.downstreamCatalogDefaults(context.Background(), clusterView{RancherURL: server.URL}, rancherOperationRequest{Token: "rancher-token", Insecure: true, Provider: "linode", Distribution: "k3s"}, &result)
	if result.DefaultVersion != "v1.36.5+k3s1" || result.NamePrefix != "atb" || result.Fields["sshUser"].Default != "root" || result.Fields["createPrivateIp"].Default != false {
		t.Fatalf("wrong defaults: %+v", result)
	}
	if _, ok := result.Fields["unrecognized"]; ok {
		t.Fatal("added uncreatable field")
	}
}

func TestDeployedLinodeCatalogProxy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer rancher-token" || r.Header.Get("x-api-cattleauth-header") != "bearer credID=cattle-global-data:cc-test passwordField=token" {
			t.Error("wrong proxy authentication")
		}
		var data any
		switch r.URL.Path {
		case "/meta/proxy/api.linode.com/v4/regions":
			data = []map[string]any{{"id": "us-west", "label": "Fremont", "status": "ok"}}
		case "/meta/proxy/api.linode.com/v4/linode/types":
			data = []map[string]any{{"id": "g6-standard-2", "label": "Linode 4 GB", "memory": 4096, "vcpus": 2}}
		case "/meta/proxy/api.linode.com/v4/images":
			data = []map[string]any{{"id": "linode/ubuntu24.04", "label": "Ubuntu 24.04", "status": "available"}}
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data, "pages": 1})
	}))
	defer server.Close()
	catalog, err := deployedLinodeChoices(context.Background(), clusterView{RancherURL: server.URL}, rancherOperationRequest{Token: "rancher-token", Insecure: true, Provider: "linode", CredentialID: "cattle-global-data:cc-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Regions) != 1 || len(catalog.Types) != 1 || catalog.Images[0].ID != "linode/ubuntu24.04" {
		t.Fatalf("wrong catalog: %+v", catalog)
	}
}

func TestDeployedDownstreamProvisionableVersions(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1-k3s-release/releases":
			fmt.Fprint(w, `{"data":[{"id":"v1.37.1+k3s1"},{"id":"v1.36.5+k3s1","serverArgs":{}},{"id":"v1.36.4+k3s1","serverArgs":{}}]}`)
		case "/v1/schemas/rke-machine-config.cattle.io.linodeconfig":
			fmt.Fprint(w, `{"resourceFields":{"region":{"type":"string","create":true}}}`)
		case "/v3/cloudcredentials":
			fmt.Fprint(w, `{"data":[]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	in := rancherOperationRequest{Provider: "linode", Distribution: "k3s", Token: "test", Insecure: true}
	cluster := clusterView{RancherURL: server.URL}
	catalog, err := deployedDownstreamOptions(context.Background(), cluster, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Versions) != 2 || catalog.Versions[0] != "v1.36.5+k3s1" {
		t.Fatalf("unprovisionable release included: %+v", catalog.Versions)
	}
	p := &localControlPanel{}
	p.downstreamCatalogDefaults(context.Background(), cluster, in, &catalog)
	if catalog.DefaultVersion != "" || len(catalog.Warnings) == 0 {
		t.Fatal("missing default silently chose a version")
	}
}
