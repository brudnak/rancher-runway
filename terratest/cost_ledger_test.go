package test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCostLedgerRecordsAndSummarizesCleanupEstimate(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv(runIDEnv, "abc12345")

	panel := &localControlPanel{}
	record := panelRunRecord{
		RunID:     "abc12345",
		SlotID:    "slot-abc12345",
		Owner:     "Ada Lovelace",
		AWSPrefix: "alb-rabc12345",
	}
	if err := os.MkdirAll(panel.runRecordsDir(), 0o755); err != nil {
		t.Fatalf("failed to create run records dir: %v", err)
	}
	panel.writeRunRecord(record)

	estimate := &cleanupCostEstimate{
		Region:              "us-east-2",
		TotalRuntimeHours:   2.5,
		InstanceCount:       3,
		InstanceType:        "t3a.large",
		VolumeCount:         3,
		VolumeType:          "gp2",
		VolumeSizeGiB:       200,
		EstimatedEC2CostUSD: 0.44,
		EstimatedEBSCostUSD: 0.31,
	}

	if err := recordCleanupCostEstimate(estimate); err != nil {
		t.Fatalf("recordCleanupCostEstimate failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspace, "automation-output", "control-panel", "cost-ledger.sqlite")); err != nil {
		t.Fatalf("expected cost ledger db: %v", err)
	}

	history := discoverCostHistory()
	if history.Error != "" {
		t.Fatalf("unexpected history error: %s", history.Error)
	}
	if len(history.Entries) != 1 {
		t.Fatalf("expected one cost history entry, got %#v", history.Entries)
	}
	entry := history.Entries[0]
	if entry.RunID != "abc12345" {
		t.Fatalf("expected run id to persist, got %q", entry.RunID)
	}
	if entry.Owner != "Ada Lovelace" {
		t.Fatalf("expected owner metadata, got %q", entry.Owner)
	}
	if got := entry.TotalCostUSD; got != 0.75 {
		t.Fatalf("expected total cost 0.75, got %.2f", got)
	}
	if got := history.Totals.Lifetime; got != 0.75 {
		t.Fatalf("expected lifetime total 0.75, got %.2f", got)
	}
}

func TestResetCostLedgerRemovesDatabaseAndSidecars(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv(runIDEnv, "abc12345")

	estimate := &cleanupCostEstimate{
		Region:              "us-east-2",
		TotalRuntimeHours:   1,
		EstimatedEC2CostUSD: 0.10,
		EstimatedEBSCostUSD: 0.05,
	}
	if err := recordCleanupCostEstimate(estimate); err != nil {
		t.Fatalf("recordCleanupCostEstimate failed: %v", err)
	}

	dbPath := costLedgerPath()
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.WriteFile(sidecar, []byte("sidecar"), 0o644); err != nil {
			t.Fatalf("failed to create sidecar %s: %v", sidecar, err)
		}
	}

	if err := resetCostLedger(); err != nil {
		t.Fatalf("resetCostLedger failed: %v", err)
	}

	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, got %v", path, err)
		}
	}

	history := discoverCostHistory()
	if history.Error != "" {
		t.Fatalf("unexpected history error after reset: %s", history.Error)
	}
	if len(history.Entries) != 0 {
		t.Fatalf("expected reset history to be empty, got %#v", history.Entries)
	}
}
