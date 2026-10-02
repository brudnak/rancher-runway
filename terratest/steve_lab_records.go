package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (p *localControlPanel) finishSteveLabOperation(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	op := p.operationLocked(panelOperationSteveLab)
	op.Running = false
	op.InProcess = false
	op.PID = 0
	finished := time.Now()
	op.FinishedAt = &finished
	op.UpdatedAt = &finished
	if err != nil {
		op.Error = err.Error()
		op.Output = append(op.Output, "[steve-lab] Finished with error: "+err.Error())
	} else {
		op.Error = ""
		op.Output = append(op.Output, "[steve-lab] Steve Lab completed successfully")
	}
	p.persistOperationsLocked()
}

func (p *localControlPanel) steveLabRunsDir() string {
	path := filepath.Join(automationOutputDir(), "control-panel", "steve-lab", "runs")
	if abs, err := absoluteFromWorkingDir(path); err == nil {
		return abs
	}
	return path
}

func (p *localControlPanel) steveLabRunRecordPath(runID string) string {
	return filepath.Join(p.steveLabRunsDir(), safeRunPathSegment(runID)+".json")
}

func (p *localControlPanel) writeSteveLabRunRecord(record steveLabRunRecord) error {
	if err := os.MkdirAll(p.steveLabRunsDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.steveLabRunRecordPath(record.RunID), data, 0o600)
}

func (p *localControlPanel) deleteSteveLabRunRecord(runID string) error {
	path := p.steveLabRunRecordPath(runID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (p *localControlPanel) readSteveLabRunRecord(runID string) (steveLabRunRecord, bool) {
	data, err := os.ReadFile(p.steveLabRunRecordPath(runID))
	if err != nil {
		return steveLabRunRecord{}, false
	}
	var record steveLabRunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return steveLabRunRecord{}, false
	}
	return record, true
}

func (p *localControlPanel) listSteveLabRunRecords() []steveLabRunRecord {
	entries, err := os.ReadDir(p.steveLabRunsDir())
	if err != nil {
		return nil
	}
	var records []steveLabRunRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.steveLabRunsDir(), entry.Name()))
		if err != nil {
			continue
		}
		var record steveLabRunRecord
		if err := json.Unmarshal(data, &record); err == nil {
			if record.Status == "cleaned" || record.Status == "deleted" {
				continue
			}
			if record.StevePID > 0 && !processAlive(record.StevePID) {
				record.StevePID = 0
				if record.Status == "serving" || record.Status == "starting" {
					record.Status = "stopped"
				}
				record.UpdatedAt = time.Now()
				_ = p.writeSteveLabRunRecord(record)
			}
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records
}

func k3dDeleteMissingClusterOK(output string) bool {
	output = strings.ToLower(output)
	return strings.Contains(output, "not found") ||
		strings.Contains(output, "no nodes found") ||
		strings.Contains(output, "no cluster") ||
		strings.Contains(output, "does not exist")
}

func ensureSQLCachePrereqs(kubeconfigPath string) error {
	kubectlPath, err := resolveLocalToolPath("kubectl")
	if err != nil {
		return err
	}
	cmd := exec.Command(kubectlPath, "--kubeconfig", kubeconfigPath, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(projectCRD)
	cmd.Env = localToolEnv(nil)
	return cmd.Run()
}

const projectCRD = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: projects.management.cattle.io
spec:
  group: management.cattle.io
  names:
    kind: Project
    listKind: ProjectList
    plural: projects
    singular: project
  scope: Namespaced
  versions:
  - name: v3
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        x-kubernetes-preserve-unknown-fields: true
`
