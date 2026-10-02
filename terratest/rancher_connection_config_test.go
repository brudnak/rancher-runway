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

func TestRancherConnectionConfiguredPassword(t *testing.T) {
	for _, purpose := range []string{"cache", "test", "downstream"} {
		t.Run(purpose, func(t *testing.T) {
			logins, logouts := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				switch r.URL.RequestURI() {
				case "/v3-public/localProviders/local?action=login":
					logins++
					if body["password"] != " configured password " || body["username"] != "admin" {
						t.Error("configured password was not preserved for admin")
					}
					writeJSON(w, map[string]any{"token": "session-1:secret"})
				case "/v3/tokens":
					if purpose == "downstream" && !strings.Contains(body["description"].(string), "Downstream creation") {
						t.Error("downstream token lacks purpose")
					}
					writeJSON(w, map[string]any{"token": "token-1:derived", "expiresAt": "2026-10-03T00:00:00Z"})
				case "/v3/tokens?action=logout":
					logouts++
					w.WriteHeader(204)
				default:
					t.Error("unexpected request")
				}
			}))
			defer server.Close()
			panel := &localControlPanel{token: "panel-secret", configPath: filepath.Join(t.TempDir(), "tool-config.yml")}
			os.WriteFile(panel.configPath, []byte("rancher:\n  bootstrap_password: ' configured password '\n"), 0600)
			input := connectionInput(server.URL)
			input.Password, input.Purpose, input.UseBootstrapPassword = "", purpose, true
			raw, _ := json.Marshal(input)
			req := httptest.NewRequest("POST", "/api/rancher/token", strings.NewReader(string(raw)))
			req.Header.Set("X-Control-Panel-Token", panel.token)
			out := httptest.NewRecorder()
			panel.handleRancherToken(out, req)
			if out.Code != 200 || !strings.Contains(out.Body.String(), "token-1:derived") || strings.Contains(out.Body.String(), "configured password") || logins != 1 || logouts != 1 || out.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected token lifecycle: status %d, login %d, logout %d", out.Code, logins, logouts)
			}
		})
	}
}

func TestRancherConnectionConfigFailures(t *testing.T) {
	for _, scenario := range []struct{ name, config, username, password string }{
		{"missing password", "rancher: {}", "admin", ""},
		{"invalid YAML", "rancher: [private-secret", "admin", ""},
		{"wrong account", "rancher: {bootstrap_password: private-secret}", "someone", ""},
		{"ambiguous source", "rancher: {bootstrap_password: private-secret}", "admin", "manual-secret"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			panel := &localControlPanel{configPath: filepath.Join(t.TempDir(), "tool-config.yml")}
			os.WriteFile(panel.configPath, []byte(scenario.config), 0600)
			input := rancherTokenRequest{UseBootstrapPassword: true, Username: scenario.username, Password: scenario.password}
			err := panel.useConfiguredRancherPassword(&input)
			if err == nil || strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "manual-secret") {
				t.Fatal("config failure was accepted or exposed a secret")
			}
		})
	}
	panel := &localControlPanel{configPath: filepath.Join(t.TempDir(), "missing.yml")}
	manual := connectionInput("https://rancher.example.test")
	if err := panel.useConfiguredRancherPassword(&manual); err != nil || manual.Password != " padded-password " {
		t.Fatal("manual sign-in depended on a config file")
	}
	manual.Password, manual.UseBootstrapPassword = "", true
	if panel.useConfiguredRancherPassword(&manual) == nil {
		t.Fatal("missing config accepted")
	}
}
