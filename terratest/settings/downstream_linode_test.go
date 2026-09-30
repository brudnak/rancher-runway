package settings

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestReadLinodeDownstreamPlansRejectsMalformedAndMisalignedYAML(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		wantError  bool
	}{
		{"missing", "total_has: 2", false},
		{"aligned", "downstream:\n  linode:\n    plans:\n      - enabled: true\n        distribution: rke2\n      - enabled: false", false},
		{"too few", "downstream:\n  linode:\n    plans:\n      - enabled: true", true},
		{"too many", "downstream:\n  linode:\n    plans:\n      - enabled: false\n      - enabled: false\n      - enabled: true", true},
		{"malformed", "downstream:\n  linode:\n    plans: broken", true},
		{"bad boolean", "downstream:\n  linode:\n    plans:\n      - enabled: definitely\n      - enabled: false", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			viper.SetConfigType("yaml")
			if err := viper.ReadConfig(strings.NewReader(tc.yaml)); err != nil {
				t.Fatal(err)
			}
			plans, err := ReadLinodeDownstreamPlans(2)
			if (err != nil) != tc.wantError {
				t.Fatalf("plans=%+v error=%v, wantError=%v", plans, err, tc.wantError)
			}
			if !tc.wantError && (len(plans) != 2 || plans[0].Region != DefaultDownstreamLinodeRegion) {
				t.Fatalf("defaults not applied: %+v", plans)
			}
		})
	}
}

func TestNormalizeLinodeDownstreamPlansDefaultsAndValidatesAlignment(t *testing.T) {
	plans, err := NormalizeLinodeDownstreamPlans(nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[0].Enabled || plans[0].Distribution != "k3s" || plans[0].Region != "us-ord" || plans[0].InstanceType != "g6-standard-2" || plans[0].Image != "linode/ubuntu22.04" {
		t.Fatalf("unexpected defaults: %#v", plans)
	}
	if _, err := NormalizeLinodeDownstreamPlans(plans[:1], 2); err == nil {
		t.Fatal("expected row alignment error")
	}
}

func TestNormalizeLinodeDownstreamPlansRejectsMismatchedVersion(t *testing.T) {
	plan := DefaultLinodeDownstreamPlan()
	plan.Enabled = true
	plan.Distribution = "rke2"
	plan.KubernetesVersion = "v1.36.3+k3s1"
	if _, err := NormalizeLinodeDownstreamPlans([]LinodeDownstreamPlan{plan}, 1); err == nil {
		t.Fatal("expected distribution/version mismatch")
	}
}

func TestNormalizeLinodeDownstreamPlansDoesNotBlockOnDisabledStaleChoice(t *testing.T) {
	plan := DefaultLinodeDownstreamPlan()
	plan.Region = "not a valid region"
	if _, err := NormalizeLinodeDownstreamPlans([]LinodeDownstreamPlan{plan}, 1); err != nil {
		t.Fatalf("disabled plan should not add a Linode setup requirement: %v", err)
	}
}
