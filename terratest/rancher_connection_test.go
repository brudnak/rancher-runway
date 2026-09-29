package test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func connectionInput(base string) rancherTokenRequest {
	return rancherTokenRequest{URL: base, Username: "admin", Password: " padded-password ", TTLMinutes: 60, Purpose: "cache"}
}

func TestRancherConnectionTokenLifecycle(t *testing.T) {
	for _, scenario := range []string{"success", "token-fails", "cleanup-fails", "cancel", "no-expiry"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			calls := []string{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls = append(calls, r.Method+" "+r.URL.RequestURI())
				mu.Unlock()
				switch r.URL.RequestURI() {
				case "/prefix/v3-public/localProviders/local?action=login":
					var payload map[string]any
					json.NewDecoder(r.Body).Decode(&payload)
					if r.Method != "POST" || r.URL.Query().Get("action") != "login" || payload["password"] != " padded-password " || payload["ttl"] != float64(300000) || payload["username"] != "admin" {
						t.Error("incorrect login payload")
					}
					if scenario == "cancel" {
						cancel()
					}
					writeJSON(w, map[string]any{"token": "session-1:secret", "id": "session-1"})
				case "/prefix/v3/tokens":
					if r.Header.Get("Authorization") != "Bearer session-1:secret" {
						t.Error("derived creation missing login credential")
					}
					var payload map[string]any
					json.NewDecoder(r.Body).Decode(&payload)
					if payload["ttl"] != float64(3600000) || !strings.Contains(payload["description"].(string), "Cache Lab") {
						t.Error("incorrect token expiry/purpose")
					}
					if scenario == "token-fails" {
						http.Error(w, "SECRET PASSWORD leaked response", 500)
						return
					}
					expires := "2026-09-30T00:00:00Z"
					if scenario == "no-expiry" {
						expires = ""
					}
					writeJSON(w, map[string]any{"token": "token-2:derived-secret", "id": "token-2", "expiresAt": expires})
				case "/prefix/v3/tokens?action=logout":
					if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer session-1:secret" {
						t.Error("wrong session cleanup")
					}
					if scenario == "cleanup-fails" {
						http.Error(w, "SECRET cleanup body", 500)
						return
					}
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected credential request: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			result, err := generateRancherConnectionToken(ctx, connectionInput(server.URL+"/prefix"))
			mu.Lock()
			defer mu.Unlock()
			if scenario == "cancel" {
				if err == nil {
					t.Fatal("cancellation succeeded")
				}
				return
			}
			if len(calls) != 3 || calls[2] != "POST /prefix/v3/tokens?action=logout" {
				t.Fatalf("temporary session not cleaned up: %v", calls)
			}
			if scenario == "token-fails" {
				if err == nil || strings.Contains(err.Error(), "SECRET") {
					t.Fatal("token failure missing or exposed response")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Token != "token-2:derived-secret" || result.ID != "token-2" {
				t.Fatal("derived token not returned")
			}
			if (scenario == "cleanup-fails") != (result.Warning != "") {
				t.Fatal("cleanup failure not communicated")
			}
			if scenario == "no-expiry" && result.ExpiresAt != "" {
				t.Fatal("invented expiry")
			}
		})
	}
}
func TestRancherConnectionTLSRedirectsAndRedaction(t *testing.T) {
	for _, scenario := range []string{"untrusted", "custom-ca", "insecure", "redirect", "bad-login", "bad-ca"} {
		t.Run(scenario, func(t *testing.T) {
			redirected := 0
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++; w.WriteHeader(200) }))
			defer receiver.Close()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario == "redirect" {
					http.Redirect(w, r, receiver.URL, 307)
					return
				}
				if scenario == "bad-login" {
					http.Error(w, "padded-password private error", 401)
					return
				}
				if r.URL.Query().Get("action") == "logout" {
					w.WriteHeader(204)
					return
				}
				id := "session-1"
				if r.URL.Path == "/v3/tokens" {
					id = "token-2"
				}
				writeJSON(w, map[string]any{"token": id + ":secret", "id": id})
			}))
			defer server.Close()
			input := connectionInput(server.URL)
			if scenario == "custom-ca" {
				input.CAPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
			} else if scenario == "bad-ca" {
				input.CAPEM = "not a certificate"
			} else if scenario != "untrusted" {
				input.Insecure = true
			}
			_, err := generateRancherConnectionToken(context.Background(), input)
			success := scenario == "custom-ca" || scenario == "insecure"
			if success != (err == nil) {
				t.Fatalf("unexpected result: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "padded-password") {
				t.Fatal("response exposed password")
			}
			if redirected != 0 {
				t.Fatal("forwarded credential request to redirect")
			}
		})
	}
}
func TestRancherConnectionValidation(t *testing.T) {
	for _, raw := range []string{"", "http://example.com", "https://admin:password@example.com", "https://example.com?secret=value", "https://example.com/#token", "https://example.com/%2e%2e/admin", "https://example.com/../admin", "file:///tmp/rancher"} {
		in := connectionInput(raw)
		if in.validate() == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
	for _, ttl := range []int64{0, -1, 43201} {
		in := connectionInput("https://example.com")
		in.TTLMinutes = ttl
		if in.validate() == nil {
			t.Error("invalid ttl accepted")
		}
	}
	for _, response := range []rancherTokenResponse{{Token: "session-1:secret", ID: "another-token"}, {Token: "../some-token:secret"}, {Token: "opaque-unknown"}} {
		if rancherResponseTokenID(response) != "" {
			t.Fatal("accepted ambiguous credential identity")
		}
	}
}
func TestRancherConnectionDiscoveryAndAuthorization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	os.WriteFile(path, []byte("fixture"), 0600)
	panel := &localControlPanel{token: "panel-secret", clusterSnapshot: map[string]clusterView{
		"a": {ID: "a", Name: "Rancher", RancherURL: "https://RANCHER.example.test/", Type: "local", RunID: "run-1", KubeconfigPath: path, Pods: []podView{{Name: "private-pod"}}},
		"b": {ID: "b", Name: "Downstream", RancherURL: "https://rancher.example.test", Type: "downstream", KubeconfigPath: "/wrong-kubeconfig"},
		"c": {ID: "c", Name: "Earlier duplicate", RancherURL: "https://rancher.example.test", Type: "local", Provisioning: true},
		"d": {ID: "d", Name: "Provisioning", RancherURL: "https://pending.example.test", Provisioning: true},
		"e": {ID: "e", Name: "Docker", RancherURL: "https://docker.example.test", Type: "linode", Role: "docker", KubeconfigPath: path},
		"f": {ID: "f", Name: "Bad", RancherURL: "https://user:password@example.test"},
	}}
	targets := panel.rancherConnectionTargets()
	if len(targets) != 3 {
		t.Fatalf("dedup/filter failed: %v", targets)
	}
	for _, target := range targets {
		if target.Name == "Rancher" && (target.Kubeconfig != path || target.Provisioning) {
			t.Fatal("lost management config")
		}
		if target.Name == "Docker" && (target.CacheSupported || target.Kubeconfig != "") {
			t.Fatal("docker offered pod capture")
		}
	}
	for _, endpoint := range []string{"targets", "token"} {
		handler := panel.handleRancherTargets
		if endpoint == "token" {
			handler = panel.handleRancherToken
		}
		req := httptest.NewRequest("POST", "/api/rancher/"+endpoint, strings.NewReader(`{}`))
		out := httptest.NewRecorder()
		handler(out, req)
		if out.Code != 403 {
			t.Fatal("unauthenticated connection API allowed")
		}
		req = httptest.NewRequest("DELETE", "/api/rancher/"+endpoint, nil)
		req.Header.Set("X-Control-Panel-Token", "panel-secret")
		out = httptest.NewRecorder()
		handler(out, req)
		if out.Code != 405 {
			t.Fatal("unsupported method accepted")
		}
	}
	req := httptest.NewRequest("GET", "/api/rancher/targets", nil)
	req.Header.Set("X-Control-Panel-Token", "panel-secret")
	out := httptest.NewRecorder()
	panel.handleRancherTargets(out, req)
	if out.Code != 200 || out.Header().Get("Cache-Control") != "no-store" || strings.Contains(out.Body.String(), "private-pod") {
		t.Fatal("target response exposed excess metadata")
	}
	for _, body := range []string{`{"password":"private","unexpected":true}`, `{} {}`, `{"url":"http://example.com","password":"private"}`} {
		req := httptest.NewRequest("POST", "/api/rancher/token", strings.NewReader(body))
		req.Header.Set("X-Control-Panel-Token", "panel-secret")
		out := httptest.NewRecorder()
		panel.handleRancherToken(out, req)
		if out.Code != 400 || strings.Contains(out.Body.String(), "private") {
			t.Fatal("invalid request not safely rejected")
		}
	}
}

func TestRancherConnectionExpiryUsesServerValues(t *testing.T) {
	result := rancherCredentialExpiry(rancherTokenResponse{Created: "2026-09-29T12:00:00Z", TTL: 3600000})
	if result != "2026-09-29T13:00:00Z" {
		t.Fatal(result)
	}
	if rancherCredentialExpiry(rancherTokenResponse{Created: "2026-09-29T12:00:00Z"}) != "" {
		t.Fatal("invented lifetime")
	}
}

func TestRancherConnectionCancellationStillLogsOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loggedOut := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("action") == "login":
			writeJSON(w, map[string]any{"token": "session-fixture:secret"})
		case r.URL.Query().Get("action") == "logout":
			loggedOut = true
			w.WriteHeader(204)
		default:
			cancel()
			<-ctx.Done()
			w.WriteHeader(http.StatusRequestTimeout)
		}
	}))
	defer server.Close()
	_, err := generateRancherConnectionToken(ctx, connectionInput(server.URL))
	if err == nil || !loggedOut {
		t.Fatal("cancelled token creation did not log out its temporary session")
	}
}
func TestRancherConnectionTokenEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") == "logout" {
			w.WriteHeader(204)
			return
		}
		if r.URL.Query().Get("action") == "login" {
			writeJSON(w, map[string]any{"token": "session-fixture:secret"})
			return
		}
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if !strings.Contains(payload["description"].(string), "Test Lab") {
			t.Error("incorrect token purpose")
		}
		writeJSON(w, map[string]any{"token": "token-fixture:new-secret", "created": "2026-09-29T12:00:00Z", "ttl": 60000})
	}))
	defer server.Close()
	in := connectionInput(server.URL)
	in.Purpose = "test"
	raw, _ := json.Marshal(in)
	panel := &localControlPanel{token: "panel-secret"}
	req := httptest.NewRequest("POST", "/api/rancher/token", strings.NewReader(string(raw)))
	req.Header.Set("X-Control-Panel-Token", "panel-secret")
	out := httptest.NewRecorder()
	panel.handleRancherToken(out, req)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var result rancherTokenResult
	if json.Unmarshal(out.Body.Bytes(), &result) != nil || result.Token != "token-fixture:new-secret" || result.ExpiresAt != "2026-09-29T12:01:00Z" {
		t.Fatal("invalid token response")
	}
	if strings.Contains(out.Body.String(), in.Password) || strings.Contains(out.Body.String(), "session-fixture") || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential response exposed sign-in or allowed caching")
	}
}
