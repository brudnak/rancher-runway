package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/viper"
)

func TestRancherTokenUsesMatchingRunConfigurationAndURL(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	viper.Reset()
	t.Cleanup(viper.Reset)
	p := &localControlPanel{repoRoot: root, testDir: root}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/v3-public/localProviders/local" {
			var input map[string]string
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if input["password"] != "prime-password" && input["password"] != "community-password" {
				t.Error("authentication used another run's password")
				w.WriteHeader(401)
				return
			}
			fmt.Fprintf(w, `{"token":%q}`, input["password"])
			return
		}
		fmt.Fprintf(w, `{"token":%q}`, r.Header.Get("Authorization"))
	}))
	defer server.Close()
	for _, run := range []string{"prime", "community"} {
		viper.Set("rancher.bootstrap_password", run+"-password")
		if err := p.freezeRunConfig(run); err != nil {
			t.Fatal(err)
		}
		p.writeRunRecord(panelRunRecord{RunID: run, ConfigSnapshot: true})
	}
	viper.Set("rancher.bootstrap_password", "latest-editor-password")
	// Reusing a hostname across runs still requires the matching run's token.
	var wg sync.WaitGroup
	for _, run := range []string{"prime", "community"} {
		wg.Add(1)
		go func(run string) {
			defer wg.Done()
			token, err := p.rancherToken(run, server.URL)
			if err != nil || token != "Bearer "+run+"-password" {
				t.Errorf("wrong token for run %s: %v", run, err)
			}
		}(run)
	}
	wg.Wait()
	before := requests.Load()
	for _, run := range []string{"prime", "community"} {
		token, err := p.rancherToken(run, server.URL+"/")
		if err != nil || token != "Bearer "+run+"-password" {
			t.Fatalf("cached token for %s: %v", run, err)
		}
	}
	if requests.Load() != before {
		t.Fatal("normalized URL did not reuse the matching token")
	}
	second := httptest.NewServer(server.Config.Handler)
	defer second.Close()
	if _, err := p.rancherToken("prime", second.URL); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != before+2 {
		t.Fatal("a different Rancher URL reused the first server's token")
	}
}

func TestRunBootstrapPasswordFailsForMissingSnapshotAndSupportsLegacy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rancher.bootstrap_password", "legacy-password")
	p := &localControlPanel{repoRoot: root, testDir: root}
	p.writeRunRecord(panelRunRecord{RunID: "new", ConfigSnapshot: true})
	if _, err := p.runBootstrapPassword("new"); err == nil {
		t.Fatal("missing snapshot reused another run's password")
	}
	if password, err := p.runBootstrapPassword("legacy"); err != nil || password != "legacy-password" {
		t.Fatalf("legacy password fallback: %v", err)
	}
	if err := p.freezeRunConfig("invalid"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.runConfigPath("invalid"), []byte("rancher: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.runBootstrapPassword("invalid"); err == nil {
		t.Fatal("invalid snapshot reused another run's password")
	}
}
