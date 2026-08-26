package test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestInteractiveResolutionFailureReturnsToEditorWithoutEndingSession(t *testing.T) {
	resultCh := make(chan interactiveResult, 1)
	server := &interactiveServer{
		phase:     phaseResolving,
		submitted: true,
		resultCh:  resultCh,
	}

	server.returnResolutionFailureToEditor(errors.New("preferred registry image pair is unavailable"))

	server.mu.Lock()
	phase := server.phase
	submitted := server.submitted
	resolveErr := server.resolveErr
	server.mu.Unlock()

	if phase != phaseEditor {
		t.Fatalf("phase = %q, want %q", phase, phaseEditor)
	}
	if submitted {
		t.Fatal("submitted remained true after resolution failure")
	}
	if resolveErr != "preferred registry image pair is unavailable" {
		t.Fatalf("resolveErr = %q", resolveErr)
	}

	select {
	case result := <-resultCh:
		t.Fatalf("resolution failure ended the interactive session: %#v", result)
	default:
	}
}

func TestDecodePreflightConfigUpdateRequestReadsDownstreamLinodePlans(t *testing.T) {
	form := url.Values{
		"versions":              {"2.15-head", "2.14-head"},
		"downstreamLinodePlans": {`[{"enabled":true,"distribution":"rke2","kubernetesVersion":"v1.36.3+rke2r1","region":"us-ord","instanceType":"g6-standard-2","image":"linode/ubuntu22.04"},{"enabled":false,"distribution":"k3s","region":"us-east","instanceType":"g6-standard-1","image":"linode/debian12"}]`},
	}
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	update, err := decodePreflightConfigUpdateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(update.DownstreamLinodePlans) != 2 || !update.DownstreamLinodePlans[0].Enabled || update.DownstreamLinodePlans[0].Distribution != "rke2" || update.DownstreamLinodePlans[0].KubernetesVersion != "v1.36.3+rke2r1" || update.DownstreamLinodePlans[1].InstanceType != "g6-standard-1" {
		t.Fatalf("unexpected downstream plans: %#v", update.DownstreamLinodePlans)
	}
}

func TestInteractiveLinodeCatalogEndpointRequiresSetupTokenAndProviderToken(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	t.Setenv("LINODE_TOKEN", "")
	t.Setenv("LINODE_ACCESS_TOKEN", "")
	server := &interactiveServer{token: "setup-token", configPath: "tool-config.yml"}
	mux := http.NewServeMux()
	server.registerHandlersAt(mux, []string{"2.15-head"}, "")

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/linode-catalog", nil))
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	missingProviderToken := httptest.NewRecorder()
	mux.ServeHTTP(missingProviderToken, httptest.NewRequest(http.MethodGet, "/api/linode-catalog?token=setup-token", nil))
	if missingProviderToken.Code != http.StatusPreconditionFailed {
		t.Fatalf("missing provider token status = %d, body=%s", missingProviderToken.Code, missingProviderToken.Body.String())
	}
	if !strings.Contains(missingProviderToken.Body.String(), "LINODE_TOKEN") {
		t.Fatalf("missing provider token response = %q", missingProviderToken.Body.String())
	}
}
