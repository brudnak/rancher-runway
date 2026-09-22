package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func helmLabSaveRequest(method, body string) *http.Request {
	request := httptest.NewRequest(method, "/api/helm-lab/save", strings.NewReader(body))
	request.Header.Set("X-Control-Panel-Token", "test-token")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestHelmLabSaveExports(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	panel := &localControlPanel{token: "test-token"}
	// Exercise the registered route, not just the handler in isolation.
	handler := panel.handler()
	exports := []struct{ filename, content, savedName string }{
		{"values.yaml", "bootstrapPassword: 'p@ss'\nextraEnv:\n- name: GREETING\n  value: '你好'\n", "values.yaml"},
		{"values.yaml", "annotations:\n  example.com/key: value\n", "values (1).yaml"},
		{"setup.sh", "#!/usr/bin/env sh\nset -eu\ntouch '" + filepath.Join(home, "must-not-execute") + "'\n", "setup.sh"},
		{"setup.sh", "#!/usr/bin/env sh\n# second export\n", "setup (1).sh"},
	}
	for _, export := range exports {
		t.Run(export.savedName, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"filename": export.filename, "content": export.content})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, helmLabSaveRequest(http.MethodPost, string(body)))
			if recorder.Code != http.StatusOK {
				t.Fatalf("save returned %d: %s", recorder.Code, recorder.Body.String())
			}
			var saved struct{ Filename, Path string }
			if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil {
				t.Fatal(err)
			}
			wantPath := filepath.Join(home, "Downloads", export.savedName)
			if saved.Filename != export.savedName || saved.Path != wantPath {
				t.Fatalf("unexpected save result: %+v", saved)
			}
			content, err := os.ReadFile(saved.Path)
			if err != nil || string(content) != export.content {
				t.Fatalf("saved content changed: %q, %v", content, err)
			}
			info, err := os.Stat(saved.Path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("export must be private and non-executable: %v", info.Mode())
			}
		})
	}
	for _, export := range exports {
		content, err := os.ReadFile(filepath.Join(home, "Downloads", export.savedName))
		if err != nil || string(content) != export.content {
			t.Fatalf("a later export replaced %s: %v", export.savedName, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "must-not-execute")); !os.IsNotExist(err) {
		t.Fatal("saving a setup script must not execute it")
	}
}

func TestHelmLabSaveRejectsInvalidRequests(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	panel := &localControlPanel{token: "test-token"}
	valid := `{"filename":"values.yaml","content":"replicas: 1\n"}`
	for _, tc := range []struct {
		name, method, body string
		unauthorized       bool
		status             int
	}{
		{name: "missing token", method: "POST", body: valid, unauthorized: true, status: http.StatusForbidden},
		{name: "wrong method", method: "GET", body: valid, status: http.StatusMethodNotAllowed},
		{name: "invalid JSON", method: "POST", body: "{", status: http.StatusBadRequest},
		{name: "trailing JSON", method: "POST", body: valid + "{}", status: http.StatusBadRequest},
		{name: "trailing garbage", method: "POST", body: valid + "oops", status: http.StatusBadRequest},
		{name: "unknown field", method: "POST", body: `{"filename":"values.yaml","content":"a","path":"/tmp/export"}`, status: http.StatusBadRequest},
		{name: "path traversal", method: "POST", body: `{"filename":"../values.yaml","content":"a"}`, status: http.StatusBadRequest},
		{name: "unsupported filename", method: "POST", body: `{"filename":"other.sh","content":"a"}`, status: http.StatusBadRequest},
		{name: "empty content", method: "POST", body: `{"filename":"setup.sh","content":" \n "}`, status: http.StatusBadRequest},
		{name: "missing content", method: "POST", body: `{"filename":"values.yaml"}`, status: http.StatusBadRequest},
		{name: "oversized content", method: "POST", body: `{"filename":"values.yaml","content":"` + strings.Repeat("x", 8<<20) + `"}`, status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := helmLabSaveRequest(tc.method, tc.body)
			if tc.unauthorized {
				request.Header.Del("X-Control-Panel-Token")
			}
			recorder := httptest.NewRecorder()
			panel.handleHelmLabSave(recorder, request)
			if recorder.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", recorder.Code, tc.status, recorder.Body.String())
			}
		})
	}
	if _, err := os.Stat(filepath.Join(home, "Downloads")); !os.IsNotExist(err) {
		t.Fatal("invalid requests must not create Downloads or write files")
	}
}

func TestHelmLabSaveReportsFilesystemFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "Downloads"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	panel := &localControlPanel{token: "test-token"}
	recorder := httptest.NewRecorder()
	panel.handleHelmLabSave(recorder, helmLabSaveRequest(http.MethodPost, `{"filename":"values.yaml","content":"replicas: 1\n"}`))
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "Downloads") {
		t.Fatalf("expected actionable save failure, got %d: %s", recorder.Code, recorder.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(home, "Downloads"))
	if err != nil || string(content) != "keep" {
		t.Fatalf("failed save modified existing path: %q, %v", content, err)
	}
}
