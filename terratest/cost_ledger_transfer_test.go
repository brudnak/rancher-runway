package test

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func costFixture(id string, finished time.Time) costLedgerRecord {
	return costLedgerRecord{panelCostEntryView: panelCostEntryView{RunID: id, Owner: "Fixture owner", Region: "us-east-2", FinishedAt: finished, TotalRuntimeHours: 2.5, EC2CostUSD: 1, EBSCostUSD: 2, RDSCostUSD: 3, LoadBalancerCostUSD: 4, TotalCostUSD: 10, Currency: "USD", Source: "cleanup-estimate-v1"}, StartedAt: finished.Add(-time.Hour).Format(time.RFC3339Nano), RecordedAt: finished.Format(time.RFC3339Nano), RuntimeSeconds: 9000, InstanceCount: 2, InstanceType: "t3a.large", VolumeCount: 2, VolumeType: "gp3", VolumeSizeGiB: 200}
}
func seedCostRecords(t *testing.T, records []costLedgerRecord) {
	t.Helper()
	db, err := openCostLedger()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = insertCostImport(tx, records); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
func TestCostLedgerFullHistoryBeyondRecentLimit(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())
	now := time.Now()
	records := make([]costLedgerRecord, 245)
	for i := range records {
		records[i] = costFixture(fmt.Sprintf("run-%03d", i), now.AddDate(0, 0, -i))
		if i%2 == 0 {
			records[i].Region = "us-west-2"
		}
	}
	records[0].Warnings = []string{"RDS pricing unavailable"}
	seedCostRecords(t, records)
	history := discoverCostHistory()
	if history.Error != "" || history.RecordCount != 245 || len(history.Entries) != 200 || history.Totals.Lifetime != 2450 {
		t.Fatalf("full history mismatch: count=%d entries=%d total=%v error=%s", history.RecordCount, len(history.Entries), history.Totals.Lifetime, history.Error)
	}
	if history.Entries[0].RunID != "run-000" || history.Entries[199].RunID != "run-199" {
		t.Fatal("recent records not newest-first")
	}
	var sum float64
	var count, partial, imported int
	for _, day := range history.Daily {
		sum += day.Total
		count += day.Records
		partial += day.Partial
		imported += day.Imported
		if day.Total != day.EC2+day.EBS+day.RDS+day.LB {
			t.Fatal("daily service totals mismatch")
		}
	}
	if sum != 2450 || count != 245 || partial != 1 || imported != 245 || history.Totals.Today != 10 {
		t.Fatalf("bad daily aggregation: %v %d %d %d today=%v", sum, count, partial, imported, history.Totals.Today)
	}
	var month, week float64
	y, w := now.ISOWeek()
	for _, r := range records {
		at := r.FinishedAt.Local()
		if at.Year() == now.Year() && at.Month() == now.Month() {
			month += r.TotalCostUSD
		}
		ry, rw := at.ISOWeek()
		if ry == y && rw == w {
			week += r.TotalCostUSD
		}
	}
	if history.Totals.Month != month || history.Totals.Week != week {
		t.Fatal("calendar totals mismatch")
	}
}
func TestCostLedgerImportRoundTripAndConflictPreservation(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())
	r := costFixture("fixture", time.Now().UTC())
	r.Warnings = []string{"Load balancer price unavailable"}
	bundle := costBundle{Format: costBundleFormat, Version: 1, ExportedAt: time.Now(), Records: []costLedgerRecord{r}}
	raw, _ := json.Marshal(bundle)
	var decoded costBundle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	preview, add, err := planCostImport(nil, &decoded)
	if err != nil || preview.Added != 1 || preview.AddedTotal != 10 {
		t.Fatal(preview, err)
	}
	seedCostRecords(t, add)
	db, err := openCostLedger()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	records, err := readCostRecords(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ImportedAt == "" || !bytes.Equal(costCanonicalRecord(records[0]), costCanonicalRecord(r)) {
		t.Fatal("round trip lost metadata")
	}
	repeat, add, err := planCostImport(records, &decoded)
	if err != nil || repeat.Duplicates != 1 || repeat.Added != 0 || len(add) != 0 {
		t.Fatal(repeat, err)
	}
	decoded.Records[0].EC2CostUSD = 11
	decoded.Records[0].TotalCostUSD = 20
	conflict, add, err := planCostImport(records, &decoded)
	if err != nil || conflict.Conflicts != 1 || len(add) != 0 {
		t.Fatal(conflict, err)
	}
	if preview.Revision == repeat.Revision || conflict.Revision == repeat.Revision {
		t.Fatal("preview did not bind input and current history")
	}
}
func TestCostLedgerImportRejectsInvalidBundleBeforeWriting(t *testing.T) {
	r := costFixture("fixture", time.Now().UTC())
	cases := map[string]func(*costBundle){
		"format": func(b *costBundle) { b.Format = "other" }, "version": func(b *costBundle) { b.Version = 2 },
		"currency": func(b *costBundle) { b.Records[0].Currency = "EUR" }, "date": func(b *costBundle) { b.Records[0].FinishedAt = time.Time{} },
		"timestamp": func(b *costBundle) { b.Records[0].StartedAt = "today" }, "negative": func(b *costBundle) { b.Records[0].TotalRuntimeHours = -1 },
		"nan": func(b *costBundle) { b.Records[0].TotalCostUSD = math.NaN() }, "total": func(b *costBundle) { b.Records[0].TotalCostUSD = 999 },
		"count": func(b *costBundle) { b.Records[0].VolumeCount = -1 }, "duplicate": func(b *costBundle) { b.Records = append(b.Records, b.Records[0]) },
		"notes": func(b *costBundle) { b.Records[0].Warnings = []string{strings.Repeat("x", 16385)} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bundle := costBundle{Format: costBundleFormat, Version: 1, Records: []costLedgerRecord{r}}
			mutate(&bundle)
			if _, add, err := planCostImport(nil, &bundle); err == nil || len(add) != 0 {
				t.Fatal("accepted invalid bundle", err)
			}
		})
	}
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())
	db, err := openCostLedger()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = insertCostImport(tx, []costLedgerRecord{r, r}); err == nil {
		t.Fatal("expected duplicate insert failure")
	}
	tx.Rollback()
	rows, err := readCostRecords(db)
	if err != nil || len(rows) != 0 {
		t.Fatal("failed transaction changed history", err)
	}
}
func TestCostLedgerTransferAPIRechecksPreview(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())
	panel := &localControlPanel{token: "fixture-token", operations: newPanelOperations()}
	r := costFixture("fixture", time.Now().UTC())
	bundle := costBundle{Format: costBundleFormat, Version: 1, Records: []costLedgerRecord{r}}
	req := map[string]any{"action": "import-preview", "bundle": bundle}
	call := func(token string) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(req)
		request := httptest.NewRequest(http.MethodPost, "/api/costs/transfer", bytes.NewReader(raw))
		request.Header.Set("X-Control-Panel-Token", token)
		response := httptest.NewRecorder()
		panel.handleCostTransfer(response, request)
		return response
	}
	if got := call(""); got.Code != 403 {
		t.Fatal("unauthorized transfer", got.Code)
	}
	result := call("fixture-token")
	if result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	var preview costImportPreview
	json.Unmarshal(result.Body.Bytes(), &preview)
	if discoverCostHistory().RecordCount != 0 {
		t.Fatal("preview inserted a record")
	}
	req["action"] = "import"
	req["revision"] = preview.Revision
	if got := call("fixture-token"); got.Code != 409 {
		t.Fatal("missing confirmation accepted")
	}
	req["confirm"] = "IMPORT COST HISTORY"
	panel.operationLocked(panelOperationSetup).Running = true
	if got := call("fixture-token"); got.Code != 409 {
		t.Fatal("import allowed during lifecycle")
	}
	panel.operationLocked(panelOperationSetup).Running = false
	seedCostRecords(t, []costLedgerRecord{costFixture("other", time.Now().UTC())})
	if got := call("fixture-token"); got.Code != 409 {
		t.Fatal("stale preview accepted")
	}
	req["action"] = "import-preview"
	result = call("fixture-token")
	json.Unmarshal(result.Body.Bytes(), &preview)
	req["action"] = "import"
	req["revision"] = preview.Revision
	if got := call("fixture-token"); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	if discoverCostHistory().RecordCount != 2 {
		t.Fatal("import count mismatch")
	}
	if got := call("fixture-token"); got.Code != 409 {
		t.Fatal("replay accepted")
	}
}
func TestCostLedgerCSVAndPrivateDownload(t *testing.T) {
	r := costFixture("=HYPERLINK(\"https://example.test\")", time.Now())
	r.Owner = "@SUM(1,1)"
	r.Warnings = []string{"line one\nline two, note"}
	raw, err := costRecordsCSV([]costLedgerRecord{r})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatal(err)
	}
	if rows[1][0] != "'"+r.RunID || rows[1][2] != "'"+r.Owner || rows[1][12] != r.Warnings[0] {
		t.Fatal("CSV escaping", rows)
	}
	root := t.TempDir()
	out, err := writeCostDownload(root, raw, "runway-aws-costs.csv")
	if err != nil {
		t.Fatal(err)
	}
	path := out["path"].(string)
	info, err := os.Stat(path)
	if err != nil || filepath.Dir(path) != root || info.Mode().Perm() != 0600 {
		t.Fatal("download path or permissions", err)
	}
	saved, _ := os.ReadFile(path)
	if !bytes.Equal(saved, raw) {
		t.Fatal("download truncated")
	}
}
func TestCostLedgerDoesNotBorrowUnrelatedRunMetadata(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())
	path := filepath.Join(automationOutputDir(), "control-panel", "current-run.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	raw, _ := json.Marshal(panelRunRecord{RunID: "unrelated", Owner: "Someone else"})
	os.WriteFile(path, raw, 0600)
	if record := readRunRecordForLedger("desired-run"); record.Owner != "" || record.RunID != "" {
		t.Fatal("borrowed unrelated run metadata")
	}
}
