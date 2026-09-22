package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestConfigImportValidatesWithoutExposingSecrets(t *testing.T) {
	valid := []byte("rancher:\n  bootstrap_password: secret-preview-value\n  versions: [2.15-head]\ntf_vars:\n  aws_prefix: ab\n")
	preview, err := previewToolConfig(valid)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(preview)
	if strings.Contains(string(encoded), "secret-preview-value") || len(preview.Sections) != 2 {
		t.Fatal("preview should contain section names only")
	}
	for _, content := range []string{
		"", "[]", "rancher: [secret-preview-value]", "rancher: {mode: unknown}",
		"rancher: {}\n---\nrancher: {}", "rancher: {password: first, password: secret-preview-value}",
		"apiVersion: v1\nkind: Config\nclusters: []", "some_other_file: true",
		"rancher: {}\ntotal_has: 99999999", "rancher: {}\ndeployment: {type: other}",
		strings.Repeat("x", maxImportedConfigBytes+1),
	} {
		if _, err := previewToolConfig([]byte(content)); err == nil {
			t.Fatal("invalid input accepted")
		} else if strings.Contains(err.Error(), "secret-preview-value") {
			t.Fatal("validation error exposed a secret")
		}
	}
}

func TestConfigImportBacksUpAndRefreshesLiveSettings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := filepath.Join(t.TempDir(), "tool-config.yml")
	before := []byte(starterToolConfigYAML)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	viper.Set("rancher.bootstrap_password", "old-editor-override")
	viper.Set("old.setting", "old-value")
	after := []byte("rancher:\n  bootstrap_password: imported-password\n  versions: [2.15-head]\n  mode: manual\n  helm_commands: [helm install rancher rancher/rancher]\nrke2:\n  ingress_controller: ingress-nginx\ntf_vars:\n  aws_prefix: ab\n")
	backup, err := importToolConfig(path, after, configRevision(before))
	if err != nil {
		t.Fatal(err)
	}
	for filename, want := range map[string][]byte{path: after, backup: before} {
		got, err := os.ReadFile(filename)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("incorrect content for %s", filepath.Base(filename))
		}
		info, err := os.Stat(filename)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("config and backup must be private")
		}
	}
	if viper.GetString("rancher.bootstrap_password") != "imported-password" || viper.IsSet("old.setting") || viper.GetString("rke2.ingress_controller") != "ingress-nginx" {
		t.Fatal("live settings were not replaced, including editor overrides")
	}
	if _, err := importToolConfig(path, before, configRevision(before)); err == nil {
		t.Fatal("stale preview overwrote configuration")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, after) {
		t.Fatal("failed import changed the config")
	}
}

func TestConfigImportEndpointPreviewApplyAndGuards(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := filepath.Join(t.TempDir(), "tool-config.yml")
	before := []byte(starterToolConfigYAML)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	panel := &localControlPanel{token: "test-token", configPath: path, operations: newPanelOperations()}
	server := panel.newSetupEditor()
	mux := http.NewServeMux()
	server.registerHandlersAt(mux, []string{""}, "/setup-editor")
	call := func(method, token string, body any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(body)
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/setup-editor/api/import-config?token="+token, bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(recorder, req)
		return recorder
	}
	content := "rancher:\n  versions: [2.15-head]\n  bootstrap_password: imported-secret\n"
	request := map[string]any{"yaml": content}
	if got := call(http.MethodPost, "", request); got.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d", got.Code)
	}
	if got := call(http.MethodGet, "test-token", request); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status = %d", got.Code)
	}
	preview := call(http.MethodPost, "test-token", request)
	if preview.Code != http.StatusOK || strings.Contains(preview.Body.String(), "imported-secret") {
		t.Fatalf("invalid preview status: %d", preview.Code)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatal("preview mutated the config")
	}
	request["apply"] = true
	request["revision"] = configRevision(before)
	server.submitted = true
	if got := call(http.MethodPost, "test-token", request); got.Code != http.StatusConflict {
		t.Fatal("import allowed during resolution")
	}
	server.submitted = false
	panel.operations[panelOperationSetup].Running = true
	if got := call(http.MethodPost, "test-token", request); got.Code != http.StatusConflict {
		t.Fatal("import allowed during lifecycle operation")
	}
	panel.operations[panelOperationSetup].Running = false
	if got := call(http.MethodPost, "test-token", request); got.Code != http.StatusOK {
		t.Fatalf("apply failed: %d %s", got.Code, got.Body.String())
	}
	// The standalone page must also render fresh imported data after reload.
	page := httptest.NewRecorder()
	mux.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/setup-editor/?token=test-token", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "2.15-head") {
		t.Fatal("page did not refresh after import")
	}
}
