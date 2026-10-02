package test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestRancherUpgradeReviewPreflightNeverApplies(t *testing.T) {
	for _, fail := range []string{"", "values", "render"} {
		t.Run("failure_"+fail, func(t *testing.T) {
			plan := &rancherUpgradePlan{Image: "rancher/rancher", ImageTag: "head", AgentImage: "rancher/rancher-agent:head", kubeconfig: "fixture", archive: []byte("chart")}
			var calls []string
			var temporary string
			command := func(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				calls = append(calls, joined)
				if strings.Contains(joined, "get values") {
					if fail == "values" {
						return nil, fmt.Errorf("read failed")
					}
					return []byte(`{"extraEnv":[{"name":"KEEP","value":"yes"},{"name":"CATTLE_AGENT_IMAGE","value":"old"}]}`), nil
				}
				if !strings.Contains(joined, "--dry-run=server") || strings.Contains(joined, "--wait-for-jobs") {
					t.Fatal("review attempted a real upgrade")
				}
				temporary = args[4]
				if data, err := os.ReadFile(temporary); err != nil || string(data) != "chart" {
					t.Fatalf("chart: %s %v", data, err)
				}
				if !strings.Contains(joined, "--reuse-values") || !strings.Contains(joined, "rancherImageTag=head") {
					t.Fatal("review and execution must use the same overrides")
				}
				for i, arg := range args {
					if arg == "-f" {
						data, err := os.ReadFile(args[i+1])
						if err != nil || !strings.Contains(string(data), "KEEP") || strings.Contains(string(data), `"old"`) || !strings.Contains(string(data), plan.AgentImage) {
							t.Fatalf("overrides: %s %v", data, err)
						}
					}
				}
				if fail == "render" {
					return nil, fmt.Errorf("render failed")
				}
				return nil, nil
			}
			err := preflightRancherUpgrade(context.Background(), plan, command)
			if (err != nil) != (fail != "") {
				t.Fatalf("got %v", err)
			}
			if fail == "values" && len(calls) != 1 {
				t.Fatal("render followed failed values read")
			}
			if temporary != "" {
				if _, err := os.Stat(temporary); !os.IsNotExist(err) {
					t.Fatal("temporary chart retained")
				}
			}
		})
	}
}
