package test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type panelCostHistoryState struct {
	DBPath    string                 `json:"dbPath"`
	UpdatedAt time.Time              `json:"updatedAt"`
	Totals    panelCostHistoryTotals `json:"totals"`
	Entries   []panelCostEntryView   `json:"entries"`
	Error     string                 `json:"error,omitempty"`
}

type panelCostHistoryTotals struct {
	Lifetime float64 `json:"lifetime"`
	Month    float64 `json:"month"`
	Week     float64 `json:"week"`
	Today    float64 `json:"today"`
}

type panelCostEntryView struct {
	RunID               string    `json:"runId"`
	SlotID              string    `json:"slotId,omitempty"`
	Owner               string    `json:"owner,omitempty"`
	AWSPrefix           string    `json:"awsPrefix,omitempty"`
	Region              string    `json:"region"`
	FinishedAt          time.Time `json:"finishedAt"`
	TotalRuntimeHours   float64   `json:"totalRuntimeHours"`
	EC2CostUSD          float64   `json:"ec2CostUsd"`
	EBSCostUSD          float64   `json:"ebsCostUsd"`
	RDSCostUSD          float64   `json:"rdsCostUsd"`
	LoadBalancerCostUSD float64   `json:"loadBalancerCostUsd"`
	TotalCostUSD        float64   `json:"totalCostUsd"`
	Currency            string    `json:"currency"`
	Source              string    `json:"source"`
}

func costLedgerPath() string {
	return filepath.Join(automationOutputDir(), "control-panel", "cost-ledger.sqlite")
}

func openCostLedger() (*sql.DB, error) {
	path := costLedgerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create cost ledger directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open cost ledger: %w", err)
	}
	if err := initCostLedger(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func initCostLedger(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cost_estimates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			slot_id TEXT,
			owner TEXT,
			aws_prefix TEXT,
			region TEXT NOT NULL,
			started_at TEXT,
			finished_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			runtime_seconds INTEGER NOT NULL DEFAULT 0,
			total_runtime_hours REAL NOT NULL DEFAULT 0,
			ec2_cost_usd REAL NOT NULL DEFAULT 0,
			ebs_cost_usd REAL NOT NULL DEFAULT 0,
			total_cost_usd REAL NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT 'USD',
			source TEXT NOT NULL,
			instance_count INTEGER NOT NULL DEFAULT 0,
			instance_type TEXT,
			volume_count INTEGER NOT NULL DEFAULT 0,
			volume_type TEXT,
			volume_size_gib INTEGER NOT NULL DEFAULT 0,
			rds_cost_usd REAL NOT NULL DEFAULT 0,
			db_instance_count INTEGER NOT NULL DEFAULT 0,
			db_instance_class TEXT,
			load_balancer_cost_usd REAL NOT NULL DEFAULT 0,
			load_balancer_count INTEGER NOT NULL DEFAULT 0,
			load_balancer_type TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS cost_estimates_run_finished_source_idx ON cost_estimates(run_id, finished_at, source)`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("failed to initialize cost ledger: %w", err)
		}
	}
	migrations := []string{
		`ALTER TABLE cost_estimates ADD COLUMN rds_cost_usd REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE cost_estimates ADD COLUMN db_instance_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE cost_estimates ADD COLUMN db_instance_class TEXT`,
		`ALTER TABLE cost_estimates ADD COLUMN load_balancer_cost_usd REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE cost_estimates ADD COLUMN load_balancer_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE cost_estimates ADD COLUMN load_balancer_type TEXT`,
	}
	for _, statement := range migrations {
		if _, err := db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("failed to migrate cost ledger: %w", err)
		}
	}
	return nil
}

func recordCleanupCostEstimate(estimate *cleanupCostEstimate) error {
	if estimate == nil {
		return nil
	}

	finishedAt := time.Now().UTC()
	runID := safeRunPathSegment(os.Getenv(runIDEnv))
	if runID == "" || runID == "unknown" {
		runID = "manual-" + finishedAt.Format("20060102150405")
	}

	record := readRunRecordForLedger(runID)
	startedAt := finishedAt.Add(-time.Duration(estimate.TotalRuntimeHours * float64(time.Hour)))
	if !record.CreatedAt.IsZero() {
		startedAt = record.CreatedAt.UTC()
	}

	totalCost := estimate.EstimatedEC2CostUSD + estimate.EstimatedEBSCostUSD + estimate.EstimatedRDSCostUSD + estimate.EstimatedLBCostUSD
	db, err := openCostLedger()
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(
		`INSERT OR IGNORE INTO cost_estimates (
			run_id, slot_id, owner, aws_prefix, region, started_at, finished_at, created_at,
			runtime_seconds, total_runtime_hours, ec2_cost_usd, ebs_cost_usd, rds_cost_usd, load_balancer_cost_usd, total_cost_usd,
			currency, source, instance_count, instance_type, volume_count, volume_type, volume_size_gib,
			db_instance_count, db_instance_class, load_balancer_count, load_balancer_type
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID,
		record.SlotID,
		record.Owner,
		record.AWSPrefix,
		estimate.Region,
		startedAt.Format(time.RFC3339Nano),
		finishedAt.Format(time.RFC3339Nano),
		finishedAt.Format(time.RFC3339Nano),
		int64(estimate.TotalRuntimeHours*3600),
		estimate.TotalRuntimeHours,
		estimate.EstimatedEC2CostUSD,
		estimate.EstimatedEBSCostUSD,
		estimate.EstimatedRDSCostUSD,
		estimate.EstimatedLBCostUSD,
		totalCost,
		"USD",
		"cleanup-estimate-v1",
		estimate.InstanceCount,
		estimate.InstanceType,
		estimate.VolumeCount,
		estimate.VolumeType,
		estimate.VolumeSizeGiB,
		estimate.DBInstanceCount,
		estimate.DBInstanceClass,
		estimate.LoadBalancerCount,
		estimate.LoadBalancerType,
	)
	if err != nil {
		return fmt.Errorf("failed to record cleanup cost estimate: %w", err)
	}
	return nil
}

func readRunRecordForLedger(runID string) panelRunRecord {
	var record panelRunRecord
	safeRunID := safeRunPathSegment(runID)
	if safeRunID == "" || safeRunID == "unknown" {
		return record
	}

	paths := []string{
		filepath.Join(automationOutputDir(), "control-panel", "runs", safeRunID+".json"),
		filepath.Join(automationOutputDir(), "control-panel", "current-run.json"),
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var candidate panelRunRecord
		if err := json.Unmarshal(data, &candidate); err != nil {
			continue
		}
		if sameRunID(candidate.RunID, safeRunID) || record.RunID == "" {
			record = candidate
		}
		if sameRunID(candidate.RunID, safeRunID) {
			return record
		}
	}
	return record
}

func discoverCostHistory() panelCostHistoryState {
	state := panelCostHistoryState{
		DBPath:    costLedgerPath(),
		UpdatedAt: time.Now(),
	}

	db, err := openCostLedger()
	if err != nil {
		state.Error = err.Error()
		return state
	}
	defer db.Close()

	rows, err := db.Query(`SELECT run_id, slot_id, owner, aws_prefix, region, finished_at, total_runtime_hours, ec2_cost_usd, ebs_cost_usd, rds_cost_usd, load_balancer_cost_usd, total_cost_usd, currency, source FROM cost_estimates ORDER BY finished_at DESC LIMIT 200`)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	defer rows.Close()

	now := time.Now()
	year, week := now.ISOWeek()
	for rows.Next() {
		var entry panelCostEntryView
		var finishedAt string
		if err := rows.Scan(
			&entry.RunID,
			&entry.SlotID,
			&entry.Owner,
			&entry.AWSPrefix,
			&entry.Region,
			&finishedAt,
			&entry.TotalRuntimeHours,
			&entry.EC2CostUSD,
			&entry.EBSCostUSD,
			&entry.RDSCostUSD,
			&entry.LoadBalancerCostUSD,
			&entry.TotalCostUSD,
			&entry.Currency,
			&entry.Source,
		); err != nil {
			state.Error = err.Error()
			return state
		}
		parsed, err := time.Parse(time.RFC3339Nano, finishedAt)
		if err == nil {
			entry.FinishedAt = parsed
		}
		state.Entries = append(state.Entries, entry)

		state.Totals.Lifetime += entry.TotalCostUSD
		finishedLocal := entry.FinishedAt.Local()
		if sameDay(finishedLocal, now) {
			state.Totals.Today += entry.TotalCostUSD
		}
		if finishedLocal.Year() == now.Year() && finishedLocal.Month() == now.Month() {
			state.Totals.Month += entry.TotalCostUSD
		}
		entryYear, entryWeek := finishedLocal.ISOWeek()
		if entryYear == year && entryWeek == week {
			state.Totals.Week += entry.TotalCostUSD
		}
	}
	if err := rows.Err(); err != nil {
		state.Error = err.Error()
	}
	return state
}

func resetCostLedger() error {
	path := costLedgerPath()
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove cost ledger file %s: %w", candidate, err)
		}
	}
	return nil
}

func sameDay(left, right time.Time) bool {
	ly, lm, ld := left.Date()
	ry, rm, rd := right.Date()
	return ly == ry && lm == rm && ld == rd
}

func logPersistCleanupCostEstimate(estimate *cleanupCostEstimate) {
	if estimate == nil {
		return
	}
	if err := recordCleanupCostEstimate(estimate); err != nil {
		log.Printf("[cleanup] Failed to persist AWS cost estimate ledger: %v", err)
		return
	}
	log.Printf("[cleanup] Persisted AWS cost estimate to %s", costLedgerPath())
}

func compactCurrency(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".")
}
