package test

import (
	"context"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/spf13/viper"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var rancherHelmRepoURLs = map[string]string{
	"rancher-latest":         "https://releases.rancher.com/server-charts/latest",
	"rancher-stable":         "https://releases.rancher.com/server-charts/stable",
	"rancher-alpha":          "https://releases.rancher.com/server-charts/alpha",
	"rancher-prime":          "https://charts.rancher.com/server-charts/prime",
	"optimus-rancher-latest": "https://charts.optimus.rancher.io/server-charts/latest",
	"optimus-rancher-alpha":  "https://charts.optimus.rancher.io/server-charts/alpha",
	"rancher-optimus-alpha":  "https://s3.amazonaws.com/charts.optimus.rancher.io/server-charts/bin/chart/alpha",
	"optimus-s3":             "http://charts.optimus.rancher.io.s3.amazonaws.com/server-charts/latest",
}

func ensureRancherHelmRepos(repoAliases []string, required bool) error {
	for _, repoAlias := range repoAliases {
		repoURL, ok := rancherHelmRepoURLs[repoAlias]
		if !ok {
			continue
		}

		log.Printf("[preflight] Ensuring Helm repo %s -> %s", repoAlias, repoURL)
		output, err := exec.Command("helm", "repo", "add", repoAlias, repoURL, "--force-update").CombinedOutput()
		if err != nil {
			message := fmt.Sprintf("failed to add or update Helm repo %s (%s): %v (%s)", repoAlias, repoURL, err, strings.TrimSpace(string(output)))
			if required {
				return fmt.Errorf("%s", message)
			}
			log.Printf("[preflight] Optional Helm repo unavailable, resolver will try remaining repos: %s", message)
		}
	}
	return nil
}

func refreshHelmRepoIndexes() error {
	log.Printf("[preflight] Running 'helm repo update'...")
	helmRepoUpdateOutput, err := exec.Command("helm", "repo", "update").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run 'helm repo update': %w", err)
	}
	log.Printf("[preflight] Helm repo update completed (%d bytes)", len(strings.TrimSpace(string(helmRepoUpdateOutput))))
	return nil
}

func validateSecretEnvironment() error {
	loadSecretEnvironmentFromZProfile()

	requiredEnvVars := []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
	for _, envVar := range requiredEnvVars {
		if strings.TrimSpace(os.Getenv(envVar)) == "" {
			return fmt.Errorf("%s must be set in the environment", envVar)
		}
	}

	dockerhubUsername := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	dockerhubPassword := strings.TrimSpace(os.Getenv("DOCKERHUB_PASSWORD"))
	if (dockerhubUsername == "") != (dockerhubPassword == "") {
		return fmt.Errorf("set both DOCKERHUB_USERNAME and DOCKERHUB_PASSWORD, or leave both unset")
	}

	log.Printf("[preflight] Secret environment validated successfully")
	return nil
}

const dockerHubPullTokenURL = "https://auth.docker.io/token?service=registry.docker.io&scope=repository:rancher/rancher:pull"

func prepareDockerHubCredentialsForProvisioning() error {
	return prepareDockerHubCredentialsForProvisioningWithClient(http.DefaultClient, dockerHubPullTokenURL)
}

func prepareDockerHubCredentialsForProvisioningWithClient(client *http.Client, tokenURL string) error {
	username := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	password := strings.TrimSpace(os.Getenv("DOCKERHUB_PASSWORD"))
	if username == "" && password == "" {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("set both DOCKERHUB_USERNAME and DOCKERHUB_PASSWORD, or leave both unset")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	accepted, err := dockerHubCredentialsAccepted(ctx, client, tokenURL, username, password)
	if err != nil {
		return fmt.Errorf("validate Docker Hub credentials before provisioning: %w", err)
	}
	if accepted {
		log.Printf("[preflight] Docker Hub credentials validated successfully")
		return nil
	}

	log.Printf("[preflight] WARNING: Docker Hub rejected the configured credentials; falling back to anonymous pulls instead of installing invalid RKE2 registry authentication")
	if err := os.Unsetenv("DOCKERHUB_USERNAME"); err != nil {
		return fmt.Errorf("clear rejected DOCKERHUB_USERNAME: %w", err)
	}
	if err := os.Unsetenv("DOCKERHUB_PASSWORD"); err != nil {
		return fmt.Errorf("clear rejected DOCKERHUB_PASSWORD: %w", err)
	}
	return nil
}

func dockerHubCredentialsAccepted(ctx context.Context, client *http.Client, tokenURL, username, password string) (bool, error) {
	if client == nil {
		return false, fmt.Errorf("Docker Hub HTTP client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32*1024))

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, nil
	default:
		return false, fmt.Errorf("Docker Hub credential check returned HTTP %d", resp.StatusCode)
	}
}

func configuredRancherWebhookImage() string {
	if webhookImage := strings.TrimSpace(os.Getenv("RANCHER_WEBHOOK_IMAGE")); webhookImage != "" {
		return webhookImage
	}
	return configuredRancherInstallWebhookImage()
}

func configuredRancherInstallWebhookImage() string {
	return strings.TrimSpace(viper.GetString("rancher.webhook_image"))
}

func validateRancherWebhookImage(webhookImage string) error {
	webhookImage = strings.TrimSpace(webhookImage)
	if webhookImage == "" {
		return nil
	}
	registry, repository, tag, err := imagelookup.ParseRegistryImage(webhookImage)
	if err != nil {
		return err
	}
	found, err := registryImageTagExists(registry, repository, tag)
	if err != nil {
		return fmt.Errorf("validate webhook image %s: %w", webhookImage, err)
	}
	if !found {
		return fmt.Errorf("webhook image %s was not found in registry", webhookImage)
	}
	return nil
}

func validateWebhookImagePreflight() error {
	webhookImage := configuredRancherWebhookImage()
	if webhookImage == "" {
		log.Printf("[preflight] No Rancher webhook image override configured; skipping explicit webhook image manifest check")
		return nil
	}

	log.Printf("[preflight] Validating webhook image manifest before provisioning: %s", webhookImage)
	if err := validateRancherWebhookImage(webhookImage); err != nil {
		return err
	}

	log.Printf("[preflight] Webhook image manifest validated successfully")
	return nil
}

func loadSecretEnvironmentFromZProfile() {
	desiredVars := []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"LINODE_TOKEN",
		"LINODE_ACCESS_TOKEN",
		"DOCKERHUB_USERNAME",
		"DOCKERHUB_PASSWORD",
	}

	missingVars := 0
	for _, envVar := range desiredVars {
		if strings.TrimSpace(os.Getenv(envVar)) == "" {
			missingVars++
		}
	}
	if missingVars == 0 {
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	zprofilePath := filepath.Join(homeDir, ".zprofile")
	content, err := os.ReadFile(zprofilePath)
	if err != nil {
		return
	}

	loadedVars := 0
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "export ") {
			continue
		}

		parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "export ")), "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if !slices.Contains(desiredVars, key) {
			continue
		}
		if strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}

		value = strings.Trim(value, `"'`)
		if value == "" {
			continue
		}

		if os.Setenv(key, value) == nil {
			loadedVars++
		}
	}

	if loadedVars > 0 {
		log.Printf("[preflight] Loaded %d secret environment value(s) from ~/.zprofile", loadedVars)
	}
}

func findMissingHelmRepos(helmRepoListOutput string, helmCommands []string) []string {
	knownRepos := map[string]bool{}
	for _, line := range strings.Split(helmRepoListOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.EqualFold(fields[0], "NAME") {
			continue
		}
		knownRepos[fields[0]] = true
	}

	missingRepos := map[string]bool{}
	for _, helmCommand := range helmCommands {
		fields := strings.Fields(helmCommand)
		for _, field := range fields {
			if !strings.Contains(field, "/") {
				continue
			}
			if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
				continue
			}
			if strings.HasPrefix(field, "--") {
				continue
			}

			repoName := strings.SplitN(field, "/", 2)[0]
			if repoName == "" || repoName == "." {
				continue
			}
			if !knownRepos[repoName] {
				missingRepos[repoName] = true
			}
			break
		}
	}

	var missing []string
	for repoName := range missingRepos {
		missing = append(missing, repoName)
	}
	slices.Sort(missing)
	return missing
}

func helmRepoAliasesFromCommands(helmCommands []string) []string {
	aliases := map[string]bool{}
	for _, helmCommand := range helmCommands {
		if repoName := helmRepoAliasFromCommand(helmCommand); repoName != "" {
			aliases[repoName] = true
		}
	}

	var result []string
	for repoName := range aliases {
		result = append(result, repoName)
	}
	slices.Sort(result)
	return result
}

func helmRepoAliasFromCommand(helmCommand string) string {
	fields := strings.Fields(helmCommand)
	for _, field := range fields {
		if !strings.Contains(field, "/") {
			continue
		}
		if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
			continue
		}
		if strings.HasPrefix(field, "--") {
			continue
		}

		repoName := strings.SplitN(field, "/", 2)[0]
		if repoName == "" || repoName == "." {
			continue
		}
		return repoName
	}
	return ""
}
