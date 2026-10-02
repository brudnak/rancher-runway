package test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	version "github.com/hashicorp/go-version"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Downstream provisioning uses the selected Rancher's own schemas and release
// catalogs, so enabled drivers and fields follow that server's capabilities.
type deployedMachineField struct {
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Create      bool     `json:"create"`
	ReadOnly    bool     `json:"readOnly,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description,omitempty"`
}

type deployedDownstreamCatalog struct {
	DefaultVersion string                          `json:"defaultVersion,omitempty"`
	NamePrefix     string                          `json:"namePrefix,omitempty"`
	Warnings       []string                        `json:"warnings,omitempty"`
	Versions       []string                        `json:"versions"`
	Fields         map[string]deployedMachineField `json:"fields"`
	Credentials    []map[string]string             `json:"credentials"`
}

func deployedRancherClient(in rancherOperationRequest) (*http.Client, error) {
	if strings.TrimSpace(in.Token) == "" || strings.ContainsAny(in.Token, "\r\n") {
		return nil, fmt.Errorf("enter a Rancher API token")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: in.Insecure}
	return &http.Client{Transport: transport, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

type deployedRancherHTTPError struct {
	method, path string
	status       int
}

func (e *deployedRancherHTTPError) Error() string {
	return fmt.Sprintf("Rancher %s %s returned HTTP %d", e.method, e.path, e.status)
}

func deployedRancherJSON(ctx context.Context, client *http.Client, base, token, method, path string, payload any, out any) error {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("downstream provisioning requires an HTTPS Rancher URL without credentials or query parameters")
	}
	var data []byte
	if payload != nil {
		data, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Rancher; check connectivity, token, and TLS settings")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &deployedRancherHTTPError{method, path, response.StatusCode}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(out)
}

func deployedDownstreamOptions(ctx context.Context, cluster clusterView, in rancherOperationRequest) (deployedDownstreamCatalog, error) {
	result := deployedDownstreamCatalog{Versions: []string{}, Credentials: []map[string]string{}}
	if in.Provider != "linode" && in.Provider != "amazonec2" {
		return result, fmt.Errorf("choose Linode or AWS")
	}
	if in.Distribution != "rke2" && in.Distribution != "k3s" {
		return result, fmt.Errorf("choose RKE2 or K3s")
	}
	client, err := deployedRancherClient(in)
	if err != nil {
		return result, err
	}
	defer client.CloseIdleConnections()
	call := func(path string, out any) error {
		return deployedRancherJSON(ctx, client, cluster.RancherURL, in.Token, http.MethodGet, path, nil, out)
	}
	var releases struct {
		Data []downstreamKubernetesRelease `json:"data"`
	}
	if err = call("/v1-"+in.Distribution+"-release/releases", &releases); err != nil {
		return result, err
	}
	hasArgs := false
	for _, release := range releases.Data {
		hasArgs = hasArgs || release.ServerArgs != nil
	}
	if !hasArgs {
		result.Warnings = append(result.Warnings, "This release catalog has no provisioning metadata; version compatibility could not be verified against the Rancher UI.")
	}
	seen := map[string]bool{}
	for _, r := range releases.Data {
		if hasArgs && r.ServerArgs == nil {
			continue
		}
		v := normalizeDownstreamKubernetesVersion(r.Version)
		if v == "" {
			v = normalizeDownstreamKubernetesVersion(r.ID)
		}
		if downstreamVersionMatchesDistribution(v, in.Distribution) && !seen[v] {
			if _, e := parseDownstreamKubernetesVersion(v); e == nil {
				seen[v] = true
				result.Versions = append(result.Versions, v)
			}
		}
	}
	sort.Slice(result.Versions, func(i, j int) bool {
		a, _ := version.NewVersion(result.Versions[i])
		b, _ := version.NewVersion(result.Versions[j])
		return a.GreaterThan(b)
	})
	if len(result.Versions) == 0 {
		return result, fmt.Errorf("Rancher returned no provisionable %s versions", in.Distribution)
	}
	result.Fields, err = deployedMachineFields(in.Provider, call)
	if err != nil {
		return result, deployedDriverSchemaError(in.Provider, err, call)
	}
	var credentials struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err = call("/v3/cloudcredentials", &credentials); err != nil {
		return result, err
	}
	for _, item := range credentials.Data {
		if _, ok := item[in.Provider+"credentialConfig"]; !ok {
			continue
		}
		var id, name, defaultRegion string
		_ = json.Unmarshal(item["id"], &id)
		_ = json.Unmarshal(item["name"], &name)
		_ = json.Unmarshal(item["defaultRegion"], &defaultRegion)
		if id != "" {
			result.Credentials = append(result.Credentials, map[string]string{"id": id, "name": name, "defaultRegion": defaultRegion})
		}
	}
	return result, nil
}

func deployedReservedField(key string) bool {
	switch key {
	case "id", "type", "links", "actions", "metadata", "status", "apiVersion", "kind":
		return true
	}
	return false
}

var deployedResourceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,38}[a-z0-9]$`)

func validateDeployedMachine(machine map[string]any, fields map[string]deployedMachineField) error {
	for key, value := range machine {
		field, ok := fields[key]
		if !ok {
			return fmt.Errorf("machine field %s is not creatable in this Rancher", key)
		}
		valid := false
		switch field.Type {
		case "string", "password":
			_, valid = value.(string)
		case "boolean":
			_, valid = value.(bool)
		case "int", "integer":
			n, ok := value.(float64)
			valid = ok && n == float64(int64(n))
		case "float", "number":
			_, valid = value.(float64)
		default:
			valid = true
		}
		if !valid {
			return fmt.Errorf("machine field %s must have type %s", key, field.Type)
		}
		if len(field.Options) > 0 {
			found := false
			for _, option := range field.Options {
				if option == fmt.Sprint(value) {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("choose a listed value for %s", key)
			}
		}
	}
	for key, field := range fields {
		if field.Required {
			value, ok := machine[key]
			if (!ok || value == nil || fmt.Sprint(value) == "") && field.Default == nil {
				return fmt.Errorf("machine field %s is required", key)
			}
		}
	}
	return nil
}

func downstreamMachinePayload(in rancherOperationRequest, name string) map[string]any {
	result := map[string]any{"type": "rke-machine-config.cattle.io." + in.Provider + "config", "metadata": map[string]any{"name": name, "namespace": "fleet-default"}}
	for key, value := range in.Machine {
		result[key] = value
	}
	return result
}

func downstreamClusterPayload(in rancherOperationRequest, credentialID, machineName string) map[string]any {
	kind := "LinodeConfig"
	if in.Provider == "amazonec2" {
		kind = "Amazonec2Config"
	}
	return map[string]any{"type": "provisioning.cattle.io.cluster", "metadata": map[string]any{"name": in.Name, "namespace": "fleet-default"}, "spec": map[string]any{"kubernetesVersion": in.KubernetesVersion, "cloudCredentialSecretName": credentialID, "rkeConfig": map[string]any{"machineGlobalConfig": map[string]any{}, "machinePools": []any{map[string]any{"name": "pool1", "quantity": in.Quantity, "controlPlaneRole": true, "etcdRole": true, "workerRole": true, "machineConfigRef": map[string]any{"kind": kind, "name": machineName}}}}}}
}
