package test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/viper"
)

func validateLocalToolingPreflight(helmCommands []string) error {
	log.Printf("[preflight] Validating local tooling before provisioning...")

	requiredCommands := []string{"kubectl", "helm", "terraform"}
	for _, commandName := range requiredCommands {
		if _, err := exec.LookPath(commandName); err != nil {
			return fmt.Errorf("%s is required locally but was not found in PATH", commandName)
		}
	}
	if err := validateInstalledRancherHelmVersion(); err != nil {
		return err
	}

	helmRepoAliases := helmRepoAliasesFromCommands(helmCommands)
	if err := ensureRancherHelmRepos(helmRepoAliases, true); err != nil {
		return err
	}

	if err := refreshHelmRepoIndexes(); err != nil {
		return err
	}

	helmRepoOutput, err := exec.Command("helm", "repo", "list").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run 'helm repo list': %w", err)
	}

	missingHelmRepos := findMissingHelmRepos(string(helmRepoOutput), helmCommands)
	if len(missingHelmRepos) > 0 {
		return fmt.Errorf("missing required Helm repos locally: %s", strings.Join(missingHelmRepos, ", "))
	}

	log.Printf("[preflight] Local tooling validated successfully")
	return nil
}

func validateInstalledRancherHelmVersion() error {
	output, err := exec.Command("helm", "version", "--template", "{{.Version}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to determine the installed Helm version: %w", err)
	}
	return validateRancherHelmVersion(string(output))
}

func validateRancherHelmVersion(raw string) error {
	raw = strings.TrimSpace(raw)
	parsed, err := goversion.NewVersion(strings.TrimPrefix(raw, "v"))
	if err != nil || raw == "" {
		return fmt.Errorf("Rancher installs and upgrades require Helm 3; could not parse installed version %q", raw)
	}
	if parsed.Segments()[0] != 3 {
		return fmt.Errorf("Rancher installs and upgrades require Helm 3; found %s", raw)
	}
	return nil
}

func validateRancherHelmCommandsUseExternalTLS(helmCommands []string) error {
	for i, helmCommand := range helmCommands {
		if rancherHelmCommandUsesExternalTLS(helmCommand) {
			continue
		}
		return fmt.Errorf("rancher.helm_commands[%d] must include --set tls=external because this AWS setup terminates public TLS at the ALB and forwards HTTP/80 to Rancher", i)
	}
	return nil
}

type manualHelmValidationResult struct {
	Index   int    `json:"index"`
	OK      bool   `json:"ok"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

func validateManualHelmCommandsForPlanning(helmCommands []string, k8sVersions []string) []manualHelmValidationResult {
	results := make([]manualHelmValidationResult, 0, len(helmCommands))
	if _, err := exec.LookPath("helm"); err != nil {
		for i := range helmCommands {
			results = append(results, manualHelmValidationResult{
				Index:   i,
				OK:      false,
				Summary: "helm was not found in PATH",
				Detail:  err.Error(),
			})
		}
		return results
	}

	repoAliases := helmRepoAliasesFromCommands(helmCommands)
	if err := ensureRancherHelmRepos(repoAliases, true); err != nil {
		for i := range helmCommands {
			results = append(results, manualHelmValidationResult{
				Index:   i,
				OK:      false,
				Summary: "Helm repo setup failed",
				Detail:  err.Error(),
			})
		}
		return results
	}
	if err := refreshHelmRepoIndexes(); err != nil {
		for i := range helmCommands {
			results = append(results, manualHelmValidationResult{
				Index:   i,
				OK:      false,
				Summary: "Helm repo update failed",
				Detail:  err.Error(),
			})
		}
		return results
	}

	for i, helmCommand := range helmCommands {
		result := manualHelmValidationResult{Index: i}
		if strings.TrimSpace(helmCommand) == "" {
			result.Summary = "Helm command is empty"
			results = append(results, result)
			continue
		}
		if err := validateManualHelmCommandStructure(helmCommand); err != nil {
			result.Summary = "Helm command could not be parsed"
			result.Detail = err.Error()
			results = append(results, result)
			continue
		}
		if !rancherHelmCommandUsesExternalTLS(helmCommand) {
			result.Summary = "Missing external TLS setting"
			result.Detail = "Add --set tls=external so the AWS load balancer can terminate public TLS."
			results = append(results, result)
			continue
		}
		if strings.TrimSpace(manualHelmKubeVersionForIndex(k8sVersions, i)) == "" {
			result.Summary = "Invalid RKE2 version"
			if i >= len(k8sVersions) {
				result.Detail = fmt.Sprintf("Add an RKE2 version for HA %d before validating Helm compatibility.", i+1)
			} else if _, err := normalizeRKE2VersionInput(k8sVersions[i]); err != nil {
				result.Detail = err.Error()
			} else {
				result.Detail = "Add a valid RKE2 version before validating Helm compatibility."
			}
			results = append(results, result)
			continue
		}
		output, err := renderManualHelmCommandTemplate(helmCommand, manualHelmKubeVersionForIndex(k8sVersions, i))
		if err != nil {
			result.Summary = "Helm template render failed"
			result.Detail = output
			if result.Detail == "" {
				result.Detail = err.Error()
			}
			results = append(results, result)
			continue
		}
		result.OK = true
		result.Summary = "Helm command rendered successfully"
		result.Detail = output
		results = append(results, result)
	}
	return results
}

func manualHelmKubeVersionForIndex(k8sVersions []string, index int) string {
	if index < 0 || index >= len(k8sVersions) {
		return ""
	}
	version, err := normalizeRKE2VersionInput(k8sVersions[index])
	if err != nil {
		return ""
	}
	return helmKubeVersionFromRKE2Version(version)
}

func validateManualHelmCommandStructure(helmCommand string) error {
	fields, err := parseHelmCommandFields(helmCommand)
	if err != nil {
		return err
	}
	invocation, err := manualHelmInvocationFromFields(fields)
	if err != nil {
		return err
	}
	if invocation.releaseName == "" {
		return fmt.Errorf("release name is required")
	}
	if invocation.chartRef == "" {
		return fmt.Errorf("chart reference is required")
	}
	if !strings.HasSuffix(invocation.chartRef, "/rancher") {
		return fmt.Errorf("chart reference must point to a Rancher chart such as rancher-latest/rancher")
	}
	return nil
}

type manualHelmInvocation struct {
	operation    string
	releaseName  string
	chartRef     string
	trailingArgs []string
}

func manualHelmInvocationFromFields(fields []string) (manualHelmInvocation, error) {
	if len(fields) < 2 {
		return manualHelmInvocation{}, fmt.Errorf("command must start with helm install or helm upgrade")
	}
	if fields[0] != "helm" {
		return manualHelmInvocation{}, fmt.Errorf("command must start with helm")
	}
	operation := fields[1]
	if operation != "install" && operation != "upgrade" {
		return manualHelmInvocation{}, fmt.Errorf("command must use helm install or helm upgrade")
	}

	positionals := make([]string, 0, 2)
	trailingArgs := make([]string, 0, len(fields))
	for i := 2; i < len(fields); i++ {
		field := fields[i]
		if isShellControlField(field) {
			return manualHelmInvocation{}, fmt.Errorf("shell control operator %q is not supported in manual Helm commands", field)
		}
		if strings.HasPrefix(field, "-") {
			trailingArgs = append(trailingArgs, field)
			if helmFlagConsumesValue(field) && i+1 < len(fields) {
				i++
				trailingArgs = append(trailingArgs, fields[i])
			}
			continue
		}
		if len(positionals) < 2 {
			positionals = append(positionals, field)
			continue
		}
		trailingArgs = append(trailingArgs, field)
	}
	if len(positionals) < 2 {
		return manualHelmInvocation{}, fmt.Errorf("command must include a release name and chart reference")
	}
	return manualHelmInvocation{
		operation:    operation,
		releaseName:  positionals[0],
		chartRef:     positionals[1],
		trailingArgs: trailingArgs,
	}, nil
}

func helmFlagValue(fields []string, flagName string) string {
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if field == flagName && i+1 < len(fields) {
			return strings.TrimSpace(fields[i+1])
		}
		if strings.HasPrefix(field, flagName+"=") {
			return strings.TrimSpace(strings.TrimPrefix(field, flagName+"="))
		}
	}
	return ""
}

func renderManualHelmCommandTemplate(helmCommand, kubeVersion string) (string, error) {
	fields, err := parseHelmCommandFields(helmCommand)
	if err != nil {
		return "", err
	}
	invocation, err := manualHelmInvocationFromFields(fields)
	if err != nil {
		return "", err
	}
	args := []string{"template", invocation.releaseName, invocation.chartRef}
	args = append(args, helmTemplateCompatibleArgs(invocation.trailingArgs)...)
	if kubeVersion != "" && !helmArgsIncludeFlag(args, "--kube-version") {
		args = append(args, "--kube-version", kubeVersion)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		return trimmed, fmt.Errorf("helm template timed out")
	}
	if err != nil {
		return trimmed, err
	}
	if trimmed == "" {
		return "helm template completed without output", nil
	}
	return firstNLines(trimmed, 8), nil
}

func helmKubeVersionFromRKE2Version(rke2Version string) string {
	version := strings.TrimSpace(rke2Version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	if plus := strings.Index(version, "+"); plus >= 0 {
		version = version[:plus]
	}
	if version == "" {
		return ""
	}
	return version
}

func helmArgsIncludeFlag(args []string, flagName string) bool {
	for _, arg := range args {
		if arg == flagName || strings.HasPrefix(arg, flagName+"=") {
			return true
		}
	}
	return false
}

func helmTemplateCompatibleArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := strings.SplitN(arg, "=", 2)[0]
		switch name {
		case "--install", "--wait", "--wait-for-jobs", "--atomic", "--cleanup-on-fail", "--dry-run", "--debug":
			continue
		case "--timeout":
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
			}
			continue
		default:
			filtered = append(filtered, arg)
		}
	}
	return filtered
}

func parseHelmCommandFields(command string) ([]string, error) {
	command = strings.ReplaceAll(command, "\\\r\n", " ")
	command = strings.ReplaceAll(command, "\\\n", " ")
	var fields []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	hadChars := false

	flush := func() {
		if hadChars {
			fields = append(fields, current.String())
			current.Reset()
			hadChars = false
		}
	}

	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			hadChars = true
			escaped = false
			continue
		}
		switch {
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			hadChars = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			hadChars = true
		case (r == ' ' || r == '\t' || r == '\n' || r == '\r') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
			hadChars = true
		}
	}
	if escaped {
		current.WriteRune('\\')
		hadChars = true
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	flush()
	return fields, nil
}

func isShellControlField(field string) bool {
	switch field {
	case ";", "&&", "||", "|", ">", ">>", "<":
		return true
	default:
		return strings.Contains(field, "\x00")
	}
}

func helmFlagConsumesValue(flag string) bool {
	if strings.Contains(flag, "=") {
		return false
	}
	switch flag {
	case "-n", "--namespace", "-f", "--values", "--version", "--set", "--set-string", "--set-file", "--set-json", "--timeout", "--kube-version", "--kubeconfig", "--registry-config", "--repository-config", "--repository-cache", "--username", "--password":
		return true
	default:
		return false
	}
}

func firstNLines(value string, maxLines int) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n... (%d more lines)", len(lines)-maxLines)
}

func rancherHelmCommandUsesExternalTLS(helmCommand string) bool {
	fields := strings.Fields(helmCommand)
	for i, field := range fields {
		field = cleanHelmCommandField(field)
		switch {
		case field == "--set" || field == "--set-string":
			if i+1 < len(fields) && isExternalTLSHelmSet(cleanHelmCommandField(fields[i+1])) {
				return true
			}
		case strings.HasPrefix(field, "--set="):
			if isExternalTLSHelmSet(strings.TrimPrefix(field, "--set=")) {
				return true
			}
		case strings.HasPrefix(field, "--set-string="):
			if isExternalTLSHelmSet(strings.TrimPrefix(field, "--set-string=")) {
				return true
			}
		}
	}
	return false
}

func cleanHelmCommandField(field string) string {
	field = strings.TrimSpace(field)
	field = strings.TrimSuffix(field, `\`)
	return strings.Trim(strings.TrimSpace(field), `"'`)
}

func isExternalTLSHelmSet(value string) bool {
	value = cleanHelmCommandField(value)
	return value == "tls=external" || strings.HasPrefix(value, "tls=external,")
}

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

func validateWebhookImagePreflight() error {
	webhookImage := strings.TrimSpace(os.Getenv("RANCHER_WEBHOOK_IMAGE"))
	if webhookImage == "" {
		log.Printf("[preflight] No RANCHER_WEBHOOK_IMAGE set; skipping explicit webhook image manifest check")
		return nil
	}

	log.Printf("[preflight] Validating webhook image manifest before provisioning: %s", webhookImage)
	registry, repository, tag, err := parseRegistryImage(webhookImage)
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

func getRKE2InstallScriptURL(rke2Version, expectedInstallerSHA256 string) (string, string, error) {
	if rke2Version == "" {
		return "", "", fmt.Errorf("k8s.version must be set")
	}
	if expectedInstallerSHA256 == "" {
		return "", "", fmt.Errorf("rke2.install_script_sha256 must be set")
	}

	installScriptURL := fmt.Sprintf("https://raw.githubusercontent.com/rancher/rke2/%s/install.sh", rke2Version)
	return installScriptURL, expectedInstallerSHA256, nil
}

func validatePinnedRKE2InstallerChecksum(plans []*RancherResolvedPlan) error {
	log.Printf("[preflight] Validating pinned RKE2 installer checksum before provisioning...")

	if len(plans) == 0 {
		installScriptURL, expectedInstallerSHA256, err := getRKE2InstallScriptURL(
			viper.GetString("k8s.version"),
			viper.GetString("rke2.install_script_sha256"),
		)
		if err != nil {
			return err
		}
		if err := validateSinglePinnedRKE2InstallerChecksum(installScriptURL, expectedInstallerSHA256); err != nil {
			return err
		}
		log.Printf("[preflight] RKE2 installer checksum validated successfully")
		return nil
	}

	seen := map[string]bool{}
	for _, plan := range plans {
		if plan == nil {
			continue
		}

		installScriptURL, expectedInstallerSHA256, err := getRKE2InstallScriptURL(plan.RecommendedRKE2Version, plan.InstallerSHA256)
		if err != nil {
			return err
		}

		dedupKey := installScriptURL + "|" + strings.ToLower(expectedInstallerSHA256)
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		if err := validateSinglePinnedRKE2InstallerChecksum(installScriptURL, expectedInstallerSHA256); err != nil {
			return err
		}
	}

	log.Printf("[preflight] RKE2 installer checksum validated successfully")
	return nil
}

func validateSinglePinnedRKE2InstallerChecksum(installScriptURL, expectedInstallerSHA256 string) error {
	resp, err := http.Get(installScriptURL)
	if err != nil {
		return fmt.Errorf("failed to download installer from %s: %w", installScriptURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d downloading %s", resp.StatusCode, installScriptURL)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, resp.Body); err != nil {
		return fmt.Errorf("failed to hash installer from %s: %w", installScriptURL, err)
	}

	actualInstallerSHA256 := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualInstallerSHA256, expectedInstallerSHA256) {
		return fmt.Errorf("installer checksum mismatch for %s: expected %s, got %s", installScriptURL, expectedInstallerSHA256, actualInstallerSHA256)
	}

	return nil
}

func isRKE2InstallerChecksumFailure(stdout, stderr string) bool {
	combinedOutput := stdout + "\n" + stderr
	return strings.Contains(combinedOutput, "SECURITY ERROR: RKE2 installer checksum validation failed")
}

func buildRKE2ImagesDownloadCommand(rke2Version string) string {
	imagesURL := fmt.Sprintf("https://github.com/rancher/rke2/releases/download/%s/rke2-images.linux-amd64.tar.zst", rke2Version)
	checksumURL := fmt.Sprintf("https://github.com/rancher/rke2/releases/download/%s/sha256sum-amd64.txt", rke2Version)

	return fmt.Sprintf(`curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 600 -o /tmp/rke2-images.linux-amd64.tar.zst %s
curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 120 -o /tmp/rke2-sha256sum-amd64.txt %s

if ! (cd /tmp && grep 'rke2-images.linux-amd64.tar.zst' /tmp/rke2-sha256sum-amd64.txt | sha256sum -c -); then
  echo "############################################################" >&2
  echo "# SECURITY ERROR: RKE2 images checksum validation failed   #" >&2
  echo "# Refusing to use the downloaded images tarball.           #" >&2
  echo "############################################################" >&2
  rm -f /tmp/rke2-images.linux-amd64.tar.zst /tmp/rke2-sha256sum-amd64.txt
  exit 1
fi

rm -f /tmp/rke2-sha256sum-amd64.txt`,
		shellSingleQuote(imagesURL),
		shellSingleQuote(checksumURL),
	)
}

func buildRKE2InstallCommand(nodeType string, rke2Version string, expectedInstallerSHA256 string) (string, error) {
	installScriptURL, expectedInstallerSHA256, err := getRKE2InstallScriptURL(rke2Version, expectedInstallerSHA256)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`tmp_script="$(mktemp /tmp/rke2-install.XXXXXX.sh)"
trap 'rm -f "$tmp_script"' EXIT

# Download the exact installer script for the requested RKE2 version.
curl -fsSL -o "$tmp_script" %s

# Refuse to execute the script unless it matches the pinned checksum.
if ! echo %s"  $tmp_script" | sha256sum -c -; then
  echo "############################################################" >&2
  echo "# SECURITY ERROR: RKE2 installer checksum validation failed #" >&2
  echo "# Refusing to run the downloaded installer.                #" >&2
  echo "# Check the resolved RKE2 version and installer checksum.  #" >&2
  echo "############################################################" >&2
  exit 1
fi

sudo INSTALL_RKE2_VERSION=%s INSTALL_RKE2_TYPE=%s sh "$tmp_script"`,
		shellSingleQuote(installScriptURL),
		shellSingleQuote(expectedInstallerSHA256),
		shellSingleQuote(rke2Version),
		shellSingleQuote(nodeType),
	), nil
}
