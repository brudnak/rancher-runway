package test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRancherUpgradeSystemRegistry(t *testing.T) {
	for _, tt := range []struct {
		target, old, want string
		managed           bool
	}{
		{"stgregistry.suse.com/rancher/rancher", "registry.rancher.com", "stgregistry.suse.com", true},
		{"docker.io/rancher/rancher", "stgregistry.suse.com", "docker.io", true},
		{"registry.rancher.com/rancher/rancher", "docker.io", "registry.rancher.com", true},
		{"stgregistry.suse.com/rancher/rancher", "mirror.example/rancher", "mirror.example/rancher", false},
		{"example.org/custom/rancher", "registry.rancher.com", "registry.rancher.com", false},
	} {
		t.Run(tt.target+tt.old, func(t *testing.T) {
			got, managed := upgradeSystemRegistry(tt.target, tt.old)
			if got != tt.want || managed != tt.managed {
				t.Fatalf("%s %v", got, managed)
			}
		})
	}
	plan := &rancherUpgradePlan{Image: "stgregistry.suse.com/rancher/rancher", ImageTag: "v2.16-head", AgentImage: "stgregistry.suse.com/rancher/rancher-agent:v2.16-head", imageFields: true}
	command := func(context.Context, []byte, string, ...string) ([]byte, error) {
		return []byte(`{"systemDefaultRegistry":"registry.rancher.com","extraEnv":[{"name":"CATTLE_SYSTEM_DEFAULT_REGISTRY","value":"registry.rancher.com"},{"name":"CATTLE_AGENT_IMAGE","value":"old"},{"name":"KEEP","value":"yes"}]}`), nil
	}
	args, cleanup, err := prepareRancherUpgrade(context.Background(), plan, command)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for i, arg := range args {
		if strings.HasPrefix(arg, "systemDefaultRegistry=") {
			t.Fatal("changed chart hook registry")
		}
		if arg != "-f" {
			continue
		}
		raw, err := os.ReadFile(args[i+1])
		if err != nil {
			t.Fatal(err)
		}
		var all map[string]any
		if err = json.Unmarshal(raw, &all); err != nil {
			t.Fatal(err)
		}
		if all["systemDefaultRegistry"] != "" {
			t.Fatal("chart registry would duplicate runtime env")
		}
		for _, key := range []string{"preUpgrade", "postDelete", "auditLog"} {
			if all[key].(map[string]any)["image"].(map[string]any)["registry"] != "registry.rancher.com" {
				t.Fatal("changed hook/audit registry", all)
			}
		}
		var values struct {
			Env []struct{ Name, Value string } `json:"extraEnv"`
		}
		if err = json.Unmarshal(raw, &values); err != nil {
			t.Fatal(err)
		}
		found := map[string]string{}
		for _, e := range values.Env {
			if _, ok := found[e.Name]; ok {
				t.Fatal("duplicate env", e.Name)
			}
			found[e.Name] = e.Value
		}
		if found["CATTLE_SYSTEM_DEFAULT_REGISTRY"] != "stgregistry.suse.com" || found["CATTLE_AGENT_IMAGE"] != "rancher/rancher-agent:v2.16-head" || found["KEEP"] != "yes" {
			t.Fatal(found)
		}
	}
	if plan.SystemDefaultRegistry == nil || *plan.SystemDefaultRegistry != "stgregistry.suse.com" {
		t.Fatal("registry missing from review/history")
	}
	if _, _, err = prepareUpgradeRegistry(plan, map[string]any{"systemDefaultRegistry": "mirror.example"}); err == nil {
		t.Fatal("accepted registry policy change after review")
	}
}

func TestRancherUpgradeSystemRegistryReadback(t *testing.T) {
	target := "stgregistry.suse.com"
	plan := &rancherUpgradePlan{SystemDefaultRegistry: &target}
	for _, actual := range []string{"stgregistry.suse.com", "registry.rancher.com"} {
		err := verifyUpgradeRegistry(context.Background(), plan, func(context.Context, []byte, string, ...string) ([]byte, error) {
			return json.Marshal(map[string]string{"value": actual})
		})
		if (err == nil) != (actual == target) {
			t.Fatal(actual, err)
		}
	}
}
