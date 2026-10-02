package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults are advisory; they never replace an explicit user selection.
func (p *localControlPanel) downstreamCatalogDefaults(ctx context.Context, cluster clusterView, in rancherOperationRequest, result *deployedDownstreamCatalog) {
	var config struct {
		TFVars struct {
			Prefix string `yaml:"aws_prefix"`
		} `yaml:"tf_vars"`
	}
	if data, err := os.ReadFile(p.configPath); err == nil && yaml.Unmarshal(data, &config) == nil {
		result.NamePrefix = config.TFVars.Prefix
	}
	client, err := deployedRancherClient(in)
	if err != nil {
		return
	}
	defer client.CloseIdleConnections()
	call := func(path string, out any) error {
		return deployedRancherJSON(ctx, client, cluster.RancherURL, in.Token, "GET", path, nil, out)
	}
	var setting struct {
		Value   string `json:"value"`
		Default string `json:"default"`
	}
	_ = call("/v3/settings/"+in.Distribution+"-default-version", &setting)
	selected := firstNonEmptyCatalogValue(setting.Value, setting.Default)
	if selected == "" {
		var channels struct {
			Data []struct {
				ID     string `json:"id"`
				Latest string `json:"latest"`
			} `json:"data"`
		}
		if call("/v1-"+in.Distribution+"-release/channels", &channels) == nil {
			for _, channel := range channels.Data {
				if channel.ID == "default" {
					selected = channel.Latest
				}
			}
		}
	}
	selected = normalizeDownstreamKubernetesVersion(selected)
	for _, value := range result.Versions {
		if value == selected {
			result.DefaultVersion = value
		}
	}
	if result.DefaultVersion == "" {
		result.Warnings = append(result.Warnings, "Rancher's default Kubernetes version could not be matched to its release list. Choose a version explicitly.")
	}
	// Norman can retain driver defaults which Steve's CRD definitions omit.
	var schema struct {
		Fields map[string]deployedMachineField `json:"resourceFields"`
	}
	if call("/v3/schemas/"+in.Provider+"Config", &schema) == nil {
		for key, field := range result.Fields {
			if field.Default == nil {
				field.Default = schema.Fields[key].Default
				result.Fields[key] = field
			}
		}
	}
}

type deployedCatalogTransport struct {
	base              *url.URL
	token, credential string
	transport         http.RoundTripper
}

func (t deployedCatalogTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// The collector supplies only fixed Linode endpoints and numeric pagination.
	if request.URL.Host != "api.linode.com" || request.URL.Scheme != "https" {
		return nil, fmt.Errorf("unexpected provider catalog destination")
	}
	out := request.Clone(request.Context())
	target := *t.base
	target.Path = strings.TrimRight(target.Path, "/") + "/meta/proxy/api.linode.com" + request.URL.Path
	target.RawQuery = request.URL.RawQuery
	out.URL = &target
	out.Header.Set("Authorization", "Bearer "+t.token)
	out.Header.Set("x-api-cattleauth-header", "bearer credID="+t.credential+" passwordField=token")
	return t.transport.RoundTrip(out)
}

func deployedLinodeChoices(ctx context.Context, cluster clusterView, in rancherOperationRequest) (linodeCatalogResponse, error) {
	if in.Provider != "linode" {
		return linodeCatalogResponse{}, fmt.Errorf("provider catalog lookup currently supports Linode; use Rancher's schema defaults or enter AWS values")
	}
	client := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	token := in.Credentials["token"]
	if in.CredentialID != "" {
		if strings.ContainsAny(in.CredentialID, " \t\r\n") {
			return linodeCatalogResponse{}, fmt.Errorf("invalid cloud credential ID")
		}
		base, err := url.Parse(cluster.RancherURL)
		if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
			return linodeCatalogResponse{}, fmt.Errorf("invalid Rancher URL")
		}
		rancherClient, err := deployedRancherClient(in)
		if err != nil {
			return linodeCatalogResponse{}, err
		}
		defer rancherClient.CloseIdleConnections()
		client.Transport = deployedCatalogTransport{base, in.Token, in.CredentialID, rancherClient.Transport}
		token = "provider-proxy" // Collector requires a nonempty token; transport replaces it.
	} else if in.UseEnvironment {
		credentials, err := deployedEnvironmentCredentials("linode")
		if err != nil {
			return linodeCatalogResponse{}, err
		}
		token = credentials["token"]
	}
	if strings.TrimSpace(token) == "" {
		return linodeCatalogResponse{}, fmt.Errorf("choose a saved cloud credential, use Runway credentials, or enter a Linode token to load regions, sizes and images")
	}
	catalog, err := collectLinodeCatalog(ctx, client, linodeCatalogDefaultAPIBaseURL, token)
	if err != nil {
		return linodeCatalogResponse{}, fmt.Errorf("could not load Linode choices; check the selected credential, provider permissions and connectivity, then retry")
	}
	return catalog, nil
}
