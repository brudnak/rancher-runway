package test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const costBundleFormat = "rancher-runway/aws-cost-history"
const costBundleLimit = 64 << 20
const costBundleRecordLimit = 50000

type panelCostDay struct {
	Date     string  `json:"date"`
	Region   string  `json:"region"`
	Records  int     `json:"records"`
	Partial  int     `json:"partial"`
	Imported int     `json:"imported"`
	EC2      float64 `json:"ec2"`
	EBS      float64 `json:"ebs"`
	RDS      float64 `json:"rds"`
	LB       float64 `json:"lb"`
	Total    float64 `json:"total"`
}
type costLedgerRecord struct {
	panelCostEntryView
	StartedAt         string `json:"startedAt,omitempty"`
	RecordedAt        string `json:"recordedAt,omitempty"`
	RuntimeSeconds    int64  `json:"runtimeSeconds"`
	InstanceCount     int    `json:"instanceCount"`
	InstanceType      string `json:"instanceType,omitempty"`
	VolumeCount       int    `json:"volumeCount"`
	VolumeType        string `json:"volumeType,omitempty"`
	VolumeSizeGiB     int    `json:"volumeSizeGiB"`
	DBInstanceCount   int    `json:"dbInstanceCount"`
	DBInstanceClass   string `json:"dbInstanceClass,omitempty"`
	LoadBalancerCount int    `json:"loadBalancerCount"`
	LoadBalancerType  string `json:"loadBalancerType,omitempty"`
}
type costBundle struct {
	Format     string             `json:"format"`
	Version    int                `json:"version"`
	ExportedAt time.Time          `json:"exportedAt"`
	Records    []costLedgerRecord `json:"records"`
}
type costImportPreview struct {
	Added      int     `json:"added"`
	Duplicates int     `json:"duplicates"`
	Conflicts  int     `json:"conflicts"`
	AddedTotal float64 `json:"addedTotal"`
	Revision   string  `json:"revision"`
}

const costRecordColumns = `run_id, COALESCE(slot_id,''), COALESCE(owner,''), COALESCE(aws_prefix,''), region, finished_at, total_runtime_hours, ec2_cost_usd, ebs_cost_usd, rds_cost_usd, load_balancer_cost_usd, total_cost_usd, currency, source, COALESCE(started_at,''), created_at, runtime_seconds, instance_count, COALESCE(instance_type,''), volume_count, COALESCE(volume_type,''), volume_size_gib, db_instance_count, COALESCE(db_instance_class,''), load_balancer_count, COALESCE(load_balancer_type,''), imported_at, warnings`

type costQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

func readCostRecords(db costQuerier) ([]costLedgerRecord, error) {
	rows, err := db.Query("SELECT " + costRecordColumns + " FROM cost_estimates ORDER BY finished_at DESC, run_id, source")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []costLedgerRecord{}
	for rows.Next() {
		var r costLedgerRecord
		var finished, warnings string
		if err = rows.Scan(&r.RunID, &r.SlotID, &r.Owner, &r.AWSPrefix, &r.Region, &finished, &r.TotalRuntimeHours, &r.EC2CostUSD, &r.EBSCostUSD, &r.RDSCostUSD, &r.LoadBalancerCostUSD, &r.TotalCostUSD, &r.Currency, &r.Source, &r.StartedAt, &r.RecordedAt, &r.RuntimeSeconds, &r.InstanceCount, &r.InstanceType, &r.VolumeCount, &r.VolumeType, &r.VolumeSizeGiB, &r.DBInstanceCount, &r.DBInstanceClass, &r.LoadBalancerCount, &r.LoadBalancerType, &r.ImportedAt, &warnings); err != nil {
			return nil, err
		}
		r.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished)
		_ = json.Unmarshal([]byte(warnings), &r.Warnings)
		records = append(records, r)
	}
	return records, rows.Err()
}
func costWarningsJSON(warnings []string) string {
	if warnings == nil {
		return "[]"
	}
	b, _ := json.Marshal(warnings)
	return string(b)
}
func costRecordKey(r costLedgerRecord) string {
	b, _ := json.Marshal([]string{r.RunID, r.FinishedAt.UTC().Format(time.RFC3339Nano), r.Source})
	return string(b)
}
func costCanonicalRecord(r costLedgerRecord) []byte {
	r.ImportedAt = ""
	r.FinishedAt = r.FinishedAt.UTC()
	if len(r.Warnings) == 0 {
		r.Warnings = nil
	}
	b, _ := json.Marshal(r)
	return b
}
func validateCostRecord(r costLedgerRecord) error {
	if strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.Region) == "" || strings.TrimSpace(r.Source) == "" || r.Currency != "USD" || r.FinishedAt.IsZero() {
		return fmt.Errorf("each record needs a run ID, region, source, recorded cleanup date, and USD currency")
	}
	for _, v := range []string{r.RunID, r.SlotID, r.Owner, r.AWSPrefix, r.Region, r.Source, r.InstanceType, r.VolumeType, r.DBInstanceClass, r.LoadBalancerType} {
		if len(v) > 2048 || strings.ContainsRune(v, '\x00') {
			return fmt.Errorf("record metadata is too long or contains invalid characters")
		}
	}
	for _, v := range []string{r.StartedAt, r.RecordedAt, r.ImportedAt} {
		if v != "" {
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
				return fmt.Errorf("record contains an invalid timestamp")
			}
		}
	}
	for _, v := range []float64{r.TotalRuntimeHours, r.EC2CostUSD, r.EBSCostUSD, r.RDSCostUSD, r.LoadBalancerCostUSD, r.TotalCostUSD} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1e12 {
			return fmt.Errorf("record costs and instance-hours must be finite nonnegative numbers")
		}
	}
	for _, v := range []int64{r.RuntimeSeconds, int64(r.InstanceCount), int64(r.VolumeCount), int64(r.VolumeSizeGiB), int64(r.DBInstanceCount), int64(r.LoadBalancerCount)} {
		if v < 0 || v > 1e12 {
			return fmt.Errorf("record resource counts must be nonnegative")
		}
	}
	if math.Abs(r.EC2CostUSD+r.EBSCostUSD+r.RDSCostUSD+r.LoadBalancerCostUSD-r.TotalCostUSD) > 0.000001 {
		return fmt.Errorf("record total must equal its four service estimates")
	}
	if len(r.Warnings) > 20 {
		return fmt.Errorf("too many estimate notes")
	}
	for _, v := range r.Warnings {
		if len(v) > 16384 {
			return fmt.Errorf("estimate note is too long")
		}
	}
	return nil
}
func planCostImport(existing []costLedgerRecord, bundle *costBundle) (costImportPreview, []costLedgerRecord, error) {
	preview := costImportPreview{}
	var additions []costLedgerRecord
	if bundle == nil || bundle.Format != costBundleFormat || bundle.Version != 1 || len(bundle.Records) > costBundleRecordLimit {
		return preview, nil, fmt.Errorf("choose a Runway cost-history JSON bundle, version 1, with at most 50,000 records")
	}
	input, err := json.Marshal(bundle)
	if err != nil || len(input) > costBundleLimit {
		return preview, nil, fmt.Errorf("cost-history bundle exceeds 64 MiB or could not be encoded")
	}
	raw, err := json.Marshal(struct {
		Existing []costLedgerRecord
		Bundle   *costBundle
	}{existing, bundle})
	if err != nil || len(raw) > costBundleLimit*2 {
		return preview, nil, fmt.Errorf("cost history is too large to merge")
	}
	hash := sha256.Sum256(raw)
	preview.Revision = hex.EncodeToString(hash[:])
	known := map[string]costLedgerRecord{}
	for _, r := range existing {
		known[costRecordKey(r)] = r
	}
	seen := map[string]bool{}
	for i, r := range bundle.Records {
		if err = validateCostRecord(r); err != nil {
			return preview, nil, fmt.Errorf("record %d: %w", i+1, err)
		}
		key := costRecordKey(r)
		if seen[key] {
			return preview, nil, fmt.Errorf("bundle contains duplicate record identities")
		}
		seen[key] = true
		if old, ok := known[key]; ok {
			if bytes.Equal(costCanonicalRecord(old), costCanonicalRecord(r)) {
				preview.Duplicates++
			} else {
				preview.Conflicts++
			}
			continue
		}
		preview.Added++
		preview.AddedTotal += r.TotalCostUSD
		additions = append(additions, r)
	}
	return preview, additions, nil
}
func insertCostImport(tx *sql.Tx, records []costLedgerRecord) error {
	for _, r := range records {
		_, err := tx.Exec(`INSERT INTO cost_estimates (run_id,slot_id,owner,aws_prefix,region,finished_at,total_runtime_hours,ec2_cost_usd,ebs_cost_usd,rds_cost_usd,load_balancer_cost_usd,total_cost_usd,currency,source,started_at,created_at,runtime_seconds,instance_count,instance_type,volume_count,volume_type,volume_size_gib,db_instance_count,db_instance_class,load_balancer_count,load_balancer_type,imported_at,warnings) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.RunID, r.SlotID, r.Owner, r.AWSPrefix, r.Region, r.FinishedAt.UTC().Format(time.RFC3339Nano), r.TotalRuntimeHours, r.EC2CostUSD, r.EBSCostUSD, r.RDSCostUSD, r.LoadBalancerCostUSD, r.TotalCostUSD, r.Currency, r.Source, r.StartedAt, r.RecordedAt, r.RuntimeSeconds, r.InstanceCount, r.InstanceType, r.VolumeCount, r.VolumeType, r.VolumeSizeGiB, r.DBInstanceCount, r.DBInstanceClass, r.LoadBalancerCount, r.LoadBalancerType, time.Now().UTC().Format(time.RFC3339Nano), costWarningsJSON(r.Warnings))
		if err != nil {
			return err
		}
	}
	return nil
}
func costRecordsCSV(records []costLedgerRecord) ([]byte, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Write([]string{"Run", "Recorded cleanup (UTC)", "Owner", "Region", "EC2 instance-hours", "EC2 estimate USD", "EBS estimate USD", "RDS estimate USD", "Load balancer estimate USD", "Total estimate USD", "Source", "Imported at", "Estimate notes"})
	safe := func(s string) string {
		if s != "" && strings.ContainsAny(s[:1], "=+-@\t\r\n") {
			return "'" + s
		}
		return s
	}
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }
	for _, r := range records {
		w.Write([]string{safe(r.RunID), r.FinishedAt.UTC().Format(time.RFC3339Nano), safe(r.Owner), safe(r.Region), num(r.TotalRuntimeHours), num(r.EC2CostUSD), num(r.EBSCostUSD), num(r.RDSCostUSD), num(r.LoadBalancerCostUSD), num(r.TotalCostUSD), safe(r.Source), safe(r.ImportedAt), safe(strings.Join(r.Warnings, "; "))})
	}
	w.Flush()
	return b.Bytes(), w.Error()
}
func writeCostDownload(directory string, raw []byte, name string) (map[string]any, error) {
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		directory = filepath.Join(home, "Downloads")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(directory, cacheLabID()[:8]+"-"+name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(f.Name())
		return nil, err
	}
	return map[string]any{"path": f.Name(), "recordsBytes": len(raw)}, nil
}
func (p *localControlPanel) handleCostTransfer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", 403)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Action   string      `json:"action"`
		Revision string      `json:"revision"`
		Confirm  string      `json:"confirm"`
		Bundle   *costBundle `json:"bundle"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 80<<20)).Decode(&req) != nil {
		http.Error(w, "invalid cost history request", 400)
		return
	}
	if req.Action == "import" && p.anyOperationRunning() {
		http.Error(w, "wait for the active operation to finish before importing cost history", 409)
		return
	}
	db, err := openCostLedger()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer tx.Rollback()
	records, err := readCostRecords(tx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	switch req.Action {
	case "export-json", "export-csv":
		if req.Action == "export-json" && len(records) > costBundleRecordLimit {
			http.Error(w, "JSON backups support at most 50,000 records; use CSV to export this larger history for analysis", 400)
			return
		}
		var raw []byte
		name := "runway-aws-costs.json"
		if req.Action == "export-csv" {
			raw, err = costRecordsCSV(records)
			name = "runway-aws-costs.csv"
		} else {
			raw, err = json.MarshalIndent(costBundle{Format: costBundleFormat, Version: 1, ExportedAt: time.Now().UTC(), Records: records}, "", "  ")
		}
		if err != nil || len(raw) > costBundleLimit {
			http.Error(w, "cost export exceeds 64 MiB or could not be encoded", 400)
			return
		}
		result, e := writeCostDownload("", raw, name)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, result)
	case "import-preview", "import":
		preview, additions, e := planCostImport(records, req.Bundle)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if req.Action == "import-preview" {
			writeJSON(w, preview)
			return
		}
		if req.Confirm != "IMPORT COST HISTORY" || req.Revision != preview.Revision {
			http.Error(w, "history or input changed; preview the import again", 409)
			return
		}
		if e = insertCostImport(tx, additions); e == nil {
			e = tx.Commit()
		}
		if e != nil {
			http.Error(w, "cost import could not commit; no records were imported", 409)
			return
		}
		writeJSON(w, preview)
	default:
		http.Error(w, "unknown cost transfer action", 400)
	}
}
