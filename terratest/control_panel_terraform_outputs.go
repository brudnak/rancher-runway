package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/spf13/viper"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func readTerraformFlatOutputs(repoRoot string) (map[string]string, error) {
	return readTerraformFlatOutputsWithState(repoRoot, "", "")
}

func (p *localControlPanel) readTerraformFlatOutputs() (map[string]string, error) {
	record, ok := p.readCurrentRunRecord()
	if !ok {
		return readTerraformFlatOutputs(p.repoRoot)
	}
	return readTerraformFlatOutputsWithModule(p.repoRoot, record.TerraformStatePath, record.TerraformDataDir, record.TerraformModuleDir)
}

func readTerraformFlatOutputsWithState(repoRoot string, statePath string, dataDir string) (map[string]string, error) {
	return readTerraformFlatOutputsWithModule(repoRoot, statePath, dataDir, "")
}

func readTerraformFlatOutputsWithModule(repoRoot string, statePath string, dataDir string, moduleDir string) (map[string]string, error) {
	args := []string{"output", "-no-color", "-json"}
	if strings.TrimSpace(statePath) != "" && pathExists(statePath) {
		args = append(args, "-state="+statePath)
	}
	args = append(args, "flat_outputs")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.WaitDelay = time.Second
	cmd.Dir = strings.TrimSpace(moduleDir)
	if cmd.Dir == "" {
		cmd.Dir = filepath.Join(repoRoot, "modules", "aws")
	}
	if strings.TrimSpace(dataDir) != "" {
		cmd.Env = append(os.Environ(), "TF_DATA_DIR="+dataDir)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("terraform output stopped: %w", ctxErr)
		}
		return nil, fmt.Errorf("terraform output failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	var outputs map[string]string
	if err := json.Unmarshal(output, &outputs); err != nil {
		return nil, fmt.Errorf("failed to parse terraform outputs: %w", err)
	}
	return outputs, nil
}

func readRequestedRancherVersionsForPanel(totalHAs int) []string {
	versions := viper.GetStringSlice("rancher.versions")
	if len(versions) == totalHAs {
		out := make([]string, 0, len(versions))
		for _, version := range versions {
			out = append(out, imagelookup.NormalizeVersionInput(version))
		}
		return out
	}

	version := imagelookup.NormalizeVersionInput(viper.GetString("rancher.version"))
	if version == "" {
		return nil
	}
	if totalHAs == 1 {
		return []string{version}
	}

	out := make([]string, totalHAs)
	for i := range out {
		out[i] = version
	}
	return out
}
