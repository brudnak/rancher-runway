package settings

import "testing"

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
