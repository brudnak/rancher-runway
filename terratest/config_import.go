package test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const maxImportedConfigBytes = 1 << 20

type configImportPreview struct {
	Sections []string `json:"sections"`
	Revision string   `json:"revision"`
}

// Validate independently of the live Viper instance. Partial configs are allowed:
// Setup guides the user through missing values after import.
func previewToolConfig(content []byte) (configImportPreview, error) {
	preview := configImportPreview{}
	if len(content) == 0 || len(content) > maxImportedConfigBytes {
		return preview, fmt.Errorf("choose a non-empty YAML config file smaller than 1 MB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return preview, fmt.Errorf("the file is not valid YAML; check its indentation and syntax")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return preview, fmt.Errorf("choose a single YAML configuration, not multiple documents")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return preview, fmt.Errorf("the config must contain named sections such as rancher and tf_vars")
	}
	var values map[string]any
	if err := document.Decode(&values); err != nil {
		// Parser errors can contain passwords or tokens, so never return them.
		return preview, fmt.Errorf("the config contains invalid values or duplicate keys")
	}
	root := document.Content[0]
	recognized := false
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Tag != "!!str" {
			return preview, fmt.Errorf("config section names must be text")
		}
		switch key.Value {
		case "rancher", "deployment", "rke2", "k8s", "gpu_worker", "user", "tf_vars", "linode", "hosted_tenant", "downstream":
			if value.Kind != yaml.MappingNode {
				return preview, fmt.Errorf("%s must contain named settings", key.Value)
			}
			preview.Sections = append(preview.Sections, key.Value)
			if key.Value == "rancher" || key.Value == "tf_vars" {
				recognized = true
			}
		}
	}
	if !recognized || values["apiVersion"] != nil || values["kind"] != nil {
		return preview, fmt.Errorf("choose a Runway tool-config.yml file, not a kubeconfig or Kubernetes manifest")
	}
	candidate := viper.New()
	candidate.SetConfigType("yaml")
	if err := candidate.ReadConfig(bytes.NewReader(content)); err != nil {
		return preview, fmt.Errorf("the configuration could not be read")
	}
	switch strings.TrimSpace(candidate.GetString("deployment.type")) {
	case "", deploymentTypeHARKE2, deploymentTypeHostedTenantK3S, deploymentTypeLinodeDocker:
	default:
		return preview, fmt.Errorf("deployment.type must be ha-rke2, hosted-tenant-k3s, or linode-docker-cattle")
	}
	switch strings.TrimSpace(candidate.GetString("rancher.mode")) {
	case "", "auto", "manual":
	default:
		return preview, fmt.Errorf("rancher.mode must be auto or manual")
	}
	for _, key := range []string{"total_has", "total_rancher_instances", "hosted_tenant.total_rancher_instances"} {
		if candidate.IsSet(key) && (candidate.GetInt(key) < 0 || candidate.GetInt(key) > 100) {
			return preview, fmt.Errorf("%s must be between 0 and 100", key)
		}
	}
	sort.Strings(preview.Sections)
	return preview, nil
}

func configRevision(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func (s *interactiveServer) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.authorized(r) {
		http.Error(w, "invalid interactive setup token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportedConfigBytes*6+1024)
	var request struct {
		YAML     string `json:"yaml"`
		Apply    bool   `json:"apply"`
		Revision string `json:"revision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "could not read the config upload; choose a YAML file smaller than 1 MB", http.StatusBadRequest)
		return
	}
	content := []byte(request.YAML)
	preview, err := previewToolConfig(content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !request.Apply {
		current, err := os.ReadFile(s.configPath)
		if err != nil {
			http.Error(w, "could not read the current configuration", http.StatusInternalServerError)
			return
		}
		preview.Revision = configRevision(current)
		writeJSON(w, preview)
		return
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.submitted || (s.phase != "" && s.phase != phaseEditor) {
		http.Error(w, "return to Setup and finish the current plan before importing configuration", http.StatusConflict)
		return
	}
	importer := s.configImporter
	if importer == nil {
		importer = func(content []byte, revision string) (string, error) {
			return importToolConfig(s.configPath, content, revision)
		}
	}
	backup, err := importer(content, request.Revision)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"backup": filepath.Base(backup), "status": "imported"})
}

func importToolConfig(path string, content []byte, revision string) (string, error) {
	if _, err := previewToolConfig(content); err != nil {
		return "", err
	}
	viperConfigMu.Lock()
	defer viperConfigMu.Unlock()
	current, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("could not read the current configuration; nothing was imported")
	}
	if revision == "" || configRevision(current) != revision {
		return "", fmt.Errorf("configuration changed since preview; choose the file again to review the import")
	}
	backup, err := os.CreateTemp(filepath.Dir(path), ".tool-config-before-import-*.yml")
	if err != nil {
		return "", fmt.Errorf("could not create a config backup; nothing was imported")
	}
	backupPath := backup.Name()
	_, writeErr := backup.Write(current)
	syncErr := backup.Sync()
	closeErr := backup.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("could not save the config backup; nothing was imported")
	}
	if err := writePrivateConfigAtomically(path, content); err != nil {
		return "", fmt.Errorf("could not save the imported configuration; the existing config is unchanged")
	}
	// Clear old editor overrides as well as file-backed values.
	viper.Reset()
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(bytes.NewReader(content)); err != nil {
		_ = writePrivateConfigAtomically(path, current)
		_ = viper.ReadConfig(bytes.NewReader(current))
		return "", fmt.Errorf("could not load the imported configuration; the backup is available beside tool-config.yml")
	}
	return backupPath, nil
}
