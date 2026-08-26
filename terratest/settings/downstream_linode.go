package settings

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

const DownstreamLinodeConfigKey = "downstream.linode.plans"

const (
	DefaultDownstreamLinodeDistribution = "k3s"
	DefaultDownstreamLinodeRegion       = "us-ord"
	DefaultDownstreamLinodeInstanceType = "g6-standard-2"
	DefaultDownstreamLinodeImage        = "linode/ubuntu22.04"
)

var downstreamLinodeIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/+:-]{0,159}$`)

// LinodeDownstreamPlan is intentionally credential-free. The Linode token is
// read at runtime and is never serialized into the setup page or tool config.
type LinodeDownstreamPlan struct {
	Enabled           bool   `json:"enabled" mapstructure:"enabled"`
	Distribution      string `json:"distribution" mapstructure:"distribution"`
	KubernetesVersion string `json:"kubernetesVersion,omitempty" mapstructure:"kubernetes_version"`
	Region            string `json:"region" mapstructure:"region"`
	InstanceType      string `json:"instanceType" mapstructure:"instance_type"`
	Image             string `json:"image" mapstructure:"image"`
}

func DefaultLinodeDownstreamPlan() LinodeDownstreamPlan {
	return LinodeDownstreamPlan{
		Distribution: DefaultDownstreamLinodeDistribution,
		Region:       DefaultDownstreamLinodeRegion,
		InstanceType: DefaultDownstreamLinodeInstanceType,
		Image:        DefaultDownstreamLinodeImage,
	}
}

func CurrentLinodeDownstreamPlans(total int) []LinodeDownstreamPlan {
	if total < 0 {
		total = 0
	}
	var configured []LinodeDownstreamPlan
	_ = viper.UnmarshalKey(DownstreamLinodeConfigKey, &configured)

	plans := make([]LinodeDownstreamPlan, total)
	for i := range plans {
		plans[i] = DefaultLinodeDownstreamPlan()
		if i < len(configured) {
			plans[i] = configured[i]
		}
		normalizeLinodeDownstreamPlan(&plans[i])
	}
	return plans
}

func NormalizeLinodeDownstreamPlans(plans []LinodeDownstreamPlan, total int) ([]LinodeDownstreamPlan, error) {
	if total < 0 {
		return nil, fmt.Errorf("downstream Linode plan count cannot be negative")
	}
	if len(plans) == 0 {
		normalized := make([]LinodeDownstreamPlan, total)
		for i := range normalized {
			normalized[i] = DefaultLinodeDownstreamPlan()
		}
		return normalized, nil
	}
	if len(plans) != total {
		return nil, fmt.Errorf("downstream Linode plans must match Rancher rows: got %d plans for %d Ranchers", len(plans), total)
	}

	normalized := append([]LinodeDownstreamPlan(nil), plans...)
	for i := range normalized {
		normalizeLinodeDownstreamPlan(&normalized[i])
		if normalized[i].Enabled {
			if err := ValidateLinodeDownstreamPlan(normalized[i]); err != nil {
				return nil, fmt.Errorf("downstream Linode plan for HA %d: %w", i+1, err)
			}
		}
	}
	return normalized, nil
}

func ValidateLinodeDownstreamPlan(plan LinodeDownstreamPlan) error {
	distribution := strings.ToLower(strings.TrimSpace(plan.Distribution))
	if distribution != "k3s" && distribution != "rke2" {
		return fmt.Errorf("distribution must be k3s or rke2")
	}
	if version := strings.TrimSpace(plan.KubernetesVersion); version != "" {
		lower := strings.ToLower(version)
		if !strings.HasPrefix(lower, "v1.") || !strings.Contains(lower, "+"+distribution) {
			return fmt.Errorf("Kubernetes version %q does not match %s", version, strings.ToUpper(distribution))
		}
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "region", value: plan.Region},
		{label: "instance type", value: plan.InstanceType},
		{label: "image", value: plan.Image},
	} {
		if !downstreamLinodeIdentifierPattern.MatchString(strings.TrimSpace(field.value)) {
			return fmt.Errorf("%s %q is not a valid Linode identifier", field.label, field.value)
		}
	}
	return nil
}

func normalizeLinodeDownstreamPlan(plan *LinodeDownstreamPlan) {
	defaults := DefaultLinodeDownstreamPlan()
	plan.Distribution = strings.ToLower(strings.TrimSpace(plan.Distribution))
	if plan.Distribution == "" {
		plan.Distribution = defaults.Distribution
	}
	plan.KubernetesVersion = strings.TrimSpace(plan.KubernetesVersion)
	plan.Region = strings.TrimSpace(plan.Region)
	if plan.Region == "" {
		plan.Region = defaults.Region
	}
	plan.InstanceType = strings.TrimSpace(plan.InstanceType)
	if plan.InstanceType == "" {
		plan.InstanceType = defaults.InstanceType
	}
	plan.Image = strings.TrimSpace(plan.Image)
	if plan.Image == "" {
		plan.Image = defaults.Image
	}
}

func AnyLinodeDownstreamPlanEnabled(plans []LinodeDownstreamPlan) bool {
	for _, plan := range plans {
		if plan.Enabled {
			return true
		}
	}
	return false
}
