package test

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
)

const configuredDownstreamLinodePlansEnv = "RANCHER_RUNWAY_DOWNSTREAM_LINODE_PLANS"

type downstreamProvisioningConfig struct {
	ClusterName       string
	MachineName       string
	SecretName        string
	Namespace         string
	Distribution      string
	KubernetesVersion string
	Region            string
	InstanceType      string
	Image             string
	LinodeToken       string
}

func configuredLinodeDownstreamPlans(totalHAs int) ([]settings.LinodeDownstreamPlan, error) {
	raw, frozen := os.LookupEnv(configuredDownstreamLinodePlansEnv)
	if frozen {
		if strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("%s was provided but empty", configuredDownstreamLinodePlansEnv)
		}
		var plans []settings.LinodeDownstreamPlan
		if err := json.Unmarshal([]byte(raw), &plans); err != nil {
			return nil, fmt.Errorf("failed to parse frozen downstream Linode plans from %s: %w", configuredDownstreamLinodePlansEnv, err)
		}
		normalized, err := settings.NormalizeLinodeDownstreamPlans(plans, totalHAs)
		if err != nil {
			return nil, fmt.Errorf("invalid frozen downstream Linode plans: %w", err)
		}
		return normalized, nil
	}

	plans := settings.CurrentLinodeDownstreamPlans(totalHAs)
	normalized, err := settings.NormalizeLinodeDownstreamPlans(plans, totalHAs)
	if err != nil {
		return nil, fmt.Errorf("invalid configured downstream Linode plans: %w", err)
	}
	return normalized, nil
}

func legacyLinodeDownstreamPlans(totalHAs int) ([]settings.LinodeDownstreamPlan, error) {
	if totalHAs < 1 {
		return nil, fmt.Errorf("total_has must be at least 1")
	}
	plans := make([]settings.LinodeDownstreamPlan, totalHAs)
	for index := range plans {
		plan := settings.DefaultLinodeDownstreamPlan()
		plan.Enabled = true
		plan.KubernetesVersion = normalizeDownstreamKubernetesVersion(strings.TrimSpace(os.Getenv("K3S_VERSION")))
		plan.Region = envOrDefaultTrimmed("LINODE_REGION", settings.DefaultDownstreamLinodeRegion)
		plan.InstanceType = envOrDefaultTrimmed("LINODE_INSTANCE_TYPE", settings.DefaultDownstreamLinodeInstanceType)
		plan.Image = envOrDefaultTrimmed("LINODE_IMAGE", settings.DefaultDownstreamLinodeImage)
		if err := settings.ValidateLinodeDownstreamPlan(plan); err != nil {
			return nil, fmt.Errorf("legacy downstream Linode plan for HA %d: %w", index+1, err)
		}
		plans[index] = plan
	}
	return plans, nil
}

func validateLinodeDownstreamPlansAgainstCatalog(plans []settings.LinodeDownstreamPlan, catalog linodeCatalogResponse) error {
	regions := make(map[string]struct{}, len(catalog.Regions))
	for _, region := range catalog.Regions {
		regions[strings.TrimSpace(region.ID)] = struct{}{}
	}
	instanceTypes := make(map[string]struct{}, len(catalog.Types))
	for _, instanceType := range catalog.Types {
		instanceTypes[strings.TrimSpace(instanceType.ID)] = struct{}{}
	}
	images := make(map[string]struct{}, len(catalog.Images))
	for _, image := range catalog.Images {
		images[strings.TrimSpace(image.ID)] = struct{}{}
	}

	for index, plan := range plans {
		if !plan.Enabled {
			continue
		}
		if _, ok := regions[strings.TrimSpace(plan.Region)]; !ok {
			return fmt.Errorf("downstream Linode plan for HA %d references unavailable region %q", index+1, plan.Region)
		}
		if _, ok := instanceTypes[strings.TrimSpace(plan.InstanceType)]; !ok {
			return fmt.Errorf("downstream Linode plan for HA %d references unavailable instance type %q", index+1, plan.InstanceType)
		}
		if _, ok := images[strings.TrimSpace(plan.Image)]; !ok {
			return fmt.Errorf("downstream Linode plan for HA %d references unavailable image %q", index+1, plan.Image)
		}
	}
	return nil
}

func linodeMachineConfigPayload(cfg downstreamProvisioningConfig) map[string]interface{} {
	metadata := map[string]interface{}{
		"annotations": map[string]string{},
		"labels":      map[string]string{},
		"namespace":   cfg.Namespace,
	}
	if strings.TrimSpace(cfg.MachineName) != "" {
		metadata["name"] = strings.TrimSpace(cfg.MachineName)
	} else {
		metadata["generateName"] = fmt.Sprintf("nc-%s-pool1-", cfg.ClusterName)
	}
	return map[string]interface{}{
		"image":        cfg.Image,
		"instanceType": cfg.InstanceType,
		"interfaces":   []interface{}{},
		"metadata":     metadata,
		"region":       cfg.Region,
		"type":         "rke-machine-config.cattle.io.linodeconfig",
	}
}

func renderLinodeCredentialSecretManifest(cfg downstreamProvisioningConfig) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: cattle-global-data
  annotations:
    field.cattle.io/name: %s
type: Opaque
stringData:
  linodecredentialConfig-token: %s
`,
		yamlScalar(cfg.SecretName),
		yamlScalar(cfg.SecretName),
		yamlScalar(cfg.LinodeToken),
	)
}

func renderLinodeDownstreamClusterManifest(cfg downstreamProvisioningConfig) string {
	machineGlobalConfig := "    machineGlobalConfig: {}\n"
	if strings.EqualFold(strings.TrimSpace(cfg.Distribution), "k3s") {
		machineGlobalConfig = `    machineGlobalConfig:
      disable-apiserver: false
      disable-cloud-controller: false
      disable-controller-manager: false
      disable-etcd: false
      disable-kube-proxy: false
      disable-network-policy: false
      disable-scheduler: false
      etcd-expose-metrics: false
      etcd-s3-bucket-lookup-type: auto
      ingress-controller: traefik
      secrets-encryption: false
      secrets-encryption-provider: aescbc
`
	}
	return fmt.Sprintf(`apiVersion: provisioning.cattle.io/v1
kind: Cluster
metadata:
  name: %s
  namespace: %s
spec:
  cloudCredentialSecretName: %s
  kubernetesVersion: %s
  defaultPodSecurityAdmissionConfigurationTemplateName: ""
  localClusterAuthEndpoint:
    enabled: false
  rkeConfig:
    chartValues: {}
    dataDirectories:
      systemAgent: ""
      provisioning: ""
      k8sDistro: ""
    etcd:
      disableSnapshots: false
      s3: null
      snapshotRetention: 5
      snapshotScheduleCron: "0 */5 * * *"
%s
    machineSelectorConfig:
    - config:
        docker: false
        protect-kernel-defaults: false
        selinux: false
    networking: {}
    registries:
      configs: {}
      mirrors: {}
    machinePools:
    - name: pool1
      controlPlaneRole: true
      etcdRole: true
      workerRole: true
      quantity: 1
      drainBeforeDelete: true
      labels: {}
      unhealthyNodeTimeout: "0m"
      machineConfigRef:
        kind: LinodeConfig
        name: %s
    upgradeStrategy:
      controlPlaneConcurrency: "1"
      controlPlaneDrainOptions:
        deleteEmptyDirData: true
        disableEviction: false
        enabled: false
        force: false
        gracePeriod: -1
        ignoreDaemonSets: true
        skipWaitForDeleteTimeoutSeconds: 0
        timeout: 120
      workerConcurrency: "1"
      workerDrainOptions:
        deleteEmptyDirData: true
        disableEviction: false
        enabled: false
        force: false
        gracePeriod: -1
        ignoreDaemonSets: true
        skipWaitForDeleteTimeoutSeconds: 0
        timeout: 120
`,
		yamlScalar(cfg.ClusterName),
		yamlScalar(cfg.Namespace),
		yamlScalar("cattle-global-data:"+cfg.SecretName),
		yamlScalar(cfg.KubernetesVersion),
		machineGlobalConfig,
		yamlScalar(cfg.MachineName),
	)
}

func yamlScalar(value string) string {
	return strconv.Quote(value)
}

func envOrDefaultTrimmed(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
