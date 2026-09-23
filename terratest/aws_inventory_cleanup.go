package test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// A candidate is an owned Runway resource missing from this workspace, not proof
// that the resource is unused. Only an explicit, reviewed selection may be deleted.
type awsCleanupRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type awsCleanupInspection struct {
	Resource awsResourceView `json:"resource"`
	Effects  []string        `json:"effects"`
	// Dependencies must also be selected; never detach a disk from a live instance.
	Dependencies []awsCleanupRef `json:"-"`
}
type awsCleanupResult struct {
	Resource awsResourceView `json:"resource"`
	Status   string          `json:"status"`
	Message  string          `json:"message,omitempty"`
}
type awsCleanupPlan struct {
	Token            string                 `json:"token"`
	ExpiresAt        time.Time              `json:"expiresAt"`
	Region           string                 `json:"region"`
	Owner            string                 `json:"owner"`
	Items            []awsCleanupInspection `json:"items"`
	Blocked          []awsCleanupResult     `json:"blocked"`
	InventoryWarning string                 `json:"inventoryWarning,omitempty"`
	Confirmation     string                 `json:"confirmation"`
	scope            [32]byte
	client           awsCleanupClient
}
type awsCleanupSnapshot struct {
	panelOperationSnapshot
	Results []awsCleanupResult `json:"results"`
}
type awsCleanupClient interface {
	Inspect(context.Context, awsResourceView) (awsCleanupInspection, error)
	Delete(context.Context, awsCleanupInspection) error
}

var errAWSResourceGone = errors.New("resource is already absent")

func awsCleanupKey(t, id string) string { return t + "\x00" + id }
func awsCleanupScope() (string, string, [32]byte) {
	region := strings.TrimSpace(viper.GetString("tf_vars.aws_region"))
	if region == "" {
		region = "us-east-2"
	}
	owner := normalizeAWSOwner(viper.GetString("user.first_name") + " " + viper.GetString("user.last_name"))
	// Keep credentials out of responses, and invalidate a preview if the AWS scope changes.
	scope := sha256.Sum256([]byte(strings.Join([]string{region, owner,
		configuredAWSValue("aws.access_key_id", "AWS_ACCESS_KEY_ID"),
		getenvFallback("AWS_SECRET_ACCESS_KEY"),
		getenvFallback("AWS_SESSION_TOKEN"),
	}, "\x00")))
	return region, owner, scope
}
func awsCleanupBlockedReason(item awsResourceView, owner, region string, records []panelRunRecord) string {
	for _, record := range records {
		if (item.RunID != "" && safeRunPathSegment(item.RunID) == safeRunPathSegment(record.RunID)) ||
			(record.AWSPrefix != "" && (strings.HasPrefix(item.Name, record.AWSPrefix) || item.Tags["NamePrefix"] == record.AWSPrefix)) {
			return "Recorded run: use the Destroy tab to preserve Terraform cleanup."
		}
	}
	switch item.Type {
	case "EC2 instance", "EBS volume", "ALB", "ALB listener", "Target group", "ACM certificate":
	default:
		return "Protected: IAM and DNS dependencies cannot be verified by this regional inventory."
	}
	if region == "" || item.Region != region {
		return "Resource is outside the configured AWS region."
	}
	if owner == "" || normalizeAWSOwner(item.Tags["Owner"]) != owner {
		return "A matching Owner tag is required for cleanup."
	}
	if !awsManagedByTagValues[item.Tags["ManagedBy"]] {
		return "A recognized Runway ManagedBy tag is required for cleanup."
	}
	if strings.TrimSpace(item.Tags["HA_Rancher_RKE2_Run_ID"]) == "" {
		return "A Runway run ID tag is required for cleanup."
	}
	if item.ID == "" {
		return "Resource ID is missing."
	}
	return ""
}

// Inventory display can tolerate an unreadable run record, but cleanup must not
// interpret it as evidence that the resource has no local Terraform owner.
func (p *localControlPanel) awsCleanupRunRecords() ([]panelRunRecord, error) {
	paths := []string{p.currentRunRecordPath()}
	entries, err := os.ReadDir(p.runRecordsDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("Cannot verify local run records. Resolve the workspace access error before cleanup.")
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			paths = append(paths, filepath.Join(p.runRecordsDir(), entry.Name()))
		}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		var record panelRunRecord
		if err != nil || json.Unmarshal(data, &record) != nil || strings.TrimSpace(record.RunID) == "" {
			return nil, fmt.Errorf("Cannot verify a local run record (%s). Repair the record before inventory cleanup.", filepath.Base(path))
		}
	}
	return p.listRunRecords(), nil
}

func decodeAWSCleanupRequest(w http.ResponseWriter, r *http.Request, p *localControlPanel, dest any) bool {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return false
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}
func (p *localControlPanel) handleAWSCleanupPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resources []awsCleanupRef `json:"resources"`
	}
	if !decodeAWSCleanupRequest(w, r, p, &req) {
		return
	}
	if len(req.Resources) == 0 || len(req.Resources) > 200 {
		http.Error(w, "Select between 1 and 200 resources.", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	plan, err := p.prepareAWSCleanup(ctx, req.Resources)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, plan)
}
func (p *localControlPanel) prepareAWSCleanup(ctx context.Context, refs []awsCleanupRef) (*awsCleanupPlan, error) {
	p.mu.Lock()
	if p.anyOperationRunningLocked() {
		p.mu.Unlock()
		return nil, fmt.Errorf("Wait for the running operation to finish before reviewing AWS cleanup.")
	}
	region, owner, scope := awsCleanupScope()
	records, recordsErr := p.awsCleanupRunRecords()
	p.mu.Unlock()
	if recordsErr != nil {
		return nil, recordsErr
	}
	p.awsMu.Lock()
	inventory := p.awsCache
	p.awsMu.Unlock()
	if inventory.Region != region || inventory.Owner != owner || time.Since(inventory.UpdatedAt) > 2*time.Minute {
		return nil, fmt.Errorf("Refresh the AWS inventory before reviewing cleanup.")
	}
	available := map[string]awsResourceView{}
	for _, item := range inventory.Items {
		available[awsCleanupKey(item.Type, item.ID)] = item
	}
	selected := []awsResourceView{}
	seen := map[string]bool{}
	for _, ref := range refs {
		key := awsCleanupKey(ref.Type, ref.ID)
		if seen[key] {
			return nil, fmt.Errorf("Duplicate resource selection.")
		}
		item, ok := available[key]
		if !ok {
			return nil, fmt.Errorf("A selected resource is no longer in this inventory. Refresh and review again.")
		}
		seen[key] = true
		selected = append(selected, item)
	}
	factory := p.awsCleanupClientFactory
	if factory == nil {
		factory = newAWSCleanupClient
	}
	client, err := factory(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("Cannot initialize AWS cleanup: %w", err)
	}
	token, err := randomConfirmationToken()
	if err != nil {
		return nil, err
	}
	plan := &awsCleanupPlan{Token: token, Region: region, Owner: owner, Items: []awsCleanupInspection{}, Blocked: []awsCleanupResult{}, InventoryWarning: inventory.Error, scope: scope, client: client}
	for _, item := range selected {
		reason := awsCleanupBlockedReason(item, owner, region, records)
		var inspected awsCleanupInspection
		if reason == "" {
			inspected, err = client.Inspect(ctx, item)
			if err != nil {
				reason = err.Error()
			} else {
				reason = awsCleanupBlockedReason(inspected.Resource, owner, region, records)
			}
		}
		if reason != "" {
			plan.Blocked = append(plan.Blocked, awsCleanupResult{Resource: item, Status: "blocked", Message: reason})
			continue
		}
		plan.Items = append(plan.Items, inspected)
	}
	// Prune dependents transitively if their prerequisite could not be verified.
	for changed := true; changed; {
		changed = false
		included := map[string]bool{}
		for _, item := range plan.Items {
			included[awsCleanupKey(item.Resource.Type, item.Resource.ID)] = true
		}
		remaining := []awsCleanupInspection{}
		for _, item := range plan.Items {
			reason := ""
			for _, dependency := range item.Dependencies {
				if !included[awsCleanupKey(dependency.Type, dependency.ID)] {
					reason = "Also select the attached " + dependency.Type + " " + dependency.ID + ", or clean it up first."
					break
				}
			}
			if reason != "" {
				plan.Blocked = append(plan.Blocked, awsCleanupResult{Resource: item.Resource, Status: "blocked", Message: reason})
				changed = true
			} else {
				remaining = append(remaining, item)
			}
		}
		plan.Items = remaining
	}
	sort.SliceStable(plan.Items, func(i, j int) bool {
		return awsCleanupOrder(plan.Items[i].Resource.Type) < awsCleanupOrder(plan.Items[j].Resource.Type)
	})
	noun := "resources"
	if len(plan.Items) == 1 {
		noun = "resource"
	}
	plan.Confirmation = fmt.Sprintf("delete %d %s", len(plan.Items), noun)
	plan.ExpiresAt = time.Now().Add(5 * time.Minute)
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _, currentScope := awsCleanupScope()
	if p.anyOperationRunningLocked() || currentScope != scope {
		return nil, fmt.Errorf("The workspace changed during review. Review cleanup again.")
	}
	p.awsCleanupPlan = plan
	return plan, nil
}
func awsCleanupOrder(resourceType string) int {
	switch resourceType {
	case "ALB listener":
		return 0
	case "ALB":
		return 1
	case "Target group":
		return 2
	case "EC2 instance":
		return 3
	case "EBS volume":
		return 4
	default:
		return 5
	}
}
func (p *localControlPanel) handleAWSCleanup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token   string `json:"token"`
		Confirm string `json:"confirm"`
	}
	if !decodeAWSCleanupRequest(w, r, p, &req) {
		return
	}
	if err := p.startAWSCleanup(req.Token, req.Confirm); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]string{"status": "AWS cleanup started"})
}
func (p *localControlPanel) startAWSCleanup(token, confirm string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	plan := p.awsCleanupPlan
	if plan == nil || token != plan.Token || time.Now().After(plan.ExpiresAt) {
		return fmt.Errorf("Cleanup review expired or was replaced. Review the resources again.")
	}
	if len(plan.Items) == 0 || confirm != plan.Confirmation {
		return fmt.Errorf("Type the exact confirmation from the cleanup review.")
	}
	if p.anyOperationRunningLocked() {
		return fmt.Errorf("Another operation is running. Wait for it to finish.")
	}
	_, _, scope := awsCleanupScope()
	if scope != plan.scope {
		return fmt.Errorf("AWS configuration changed. Review cleanup again.")
	}
	records, err := p.awsCleanupRunRecords()
	if err != nil {
		return err
	}
	for _, item := range plan.Items {
		if reason := awsCleanupBlockedReason(item.Resource, plan.Owner, plan.Region, records); reason != "" {
			return fmt.Errorf("Cleanup selection changed: %s", reason)
		}
	}
	now := time.Now()
	*p.operationLocked(panelOperationAWSCleanup) = panelOperationState{Running: true, StartedAt: &now, UpdatedAt: &now, Output: []string{"[AWS cleanup] Revalidating the reviewed resources before deletion."}}
	p.awsCleanupPlan = nil // single-use, including duplicate clicks / network retries
	p.awsCleanupResults = make([]awsCleanupResult, len(plan.Items))
	for i, item := range plan.Items {
		p.awsCleanupResults[i] = awsCleanupResult{Resource: item.Resource, Status: "queued"}
	}
	p.persistOperationsLocked()
	go p.runAWSCleanup(plan)
	return nil
}
func (p *localControlPanel) runAWSCleanup(plan *awsCleanupPlan) {
	// Verify the entire plan before the first mutation; a changed scope or new
	// attachment must produce a new review instead of broadening the approved work.
	for _, item := range plan.Items {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		current, err := plan.client.Inspect(ctx, item.Resource)
		cancel()
		if errors.Is(err, errAWSResourceGone) {
			continue
		}
		if err == nil {
			var records []panelRunRecord
			records, err = p.awsCleanupRunRecords()
			if err == nil {
				err = validateAWSCleanupInspection(item, current, plan, records)
			}
		}
		if err != nil {
			for i := range plan.Items {
				p.setAWSCleanupResult(i, "blocked", "Nothing deleted: resource validation changed. "+err.Error())
			}
			p.finishAWSCleanup()
			return
		}
	}
	completed := map[string]bool{}
	for i, item := range plan.Items {
		dependencyFailed := false
		for _, dependency := range item.Dependencies {
			if !completed[awsCleanupKey(dependency.Type, dependency.ID)] {
				dependencyFailed = true
				break
			}
		}
		if dependencyFailed {
			p.setAWSCleanupResult(i, "blocked", "A selected dependency did not finish deleting. Refresh inventory before retrying.")
			continue
		}
		p.setAWSCleanupResult(i, "deleting", "Checking ownership and dependencies…")
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		current, err := plan.client.Inspect(ctx, item.Resource)
		if err == nil {
			var records []panelRunRecord
			records, err = p.awsCleanupRunRecords()
			if err == nil {
				err = validateAWSCleanupInspection(item, current, plan, records)
			}
		}
		if err == nil {
			err = plan.client.Delete(ctx, current)
		}
		cancel()
		switch {
		case errors.Is(err, errAWSResourceGone):
			completed[awsCleanupKey(item.Resource.Type, item.Resource.ID)] = true
			p.setAWSCleanupResult(i, "deleted", "Already absent (possibly removed with a selected parent).")
		case err != nil:
			p.setAWSCleanupResult(i, "failed", err.Error())
		default:
			completed[awsCleanupKey(item.Resource.Type, item.Resource.ID)] = true
			p.setAWSCleanupResult(i, "deleted", "Deletion accepted by AWS.")
		}
	}
	p.finishAWSCleanup()
}
func validateAWSCleanupInspection(reviewed, current awsCleanupInspection, plan *awsCleanupPlan, records []panelRunRecord) error {
	if current.Resource.Type != reviewed.Resource.Type || current.Resource.ID != reviewed.Resource.ID {
		return fmt.Errorf("Resource identity changed.")
	}
	if reason := awsCleanupBlockedReason(current.Resource, plan.Owner, plan.Region, records); reason != "" {
		return errors.New(reason)
	}
	for _, key := range []string{"Owner", "ManagedBy", "HA_Rancher_RKE2_Run_ID", "NamePrefix"} {
		if current.Resource.Tags[key] != reviewed.Resource.Tags[key] {
			return fmt.Errorf("Ownership or run tags changed for %s.", current.Resource.ID)
		}
	}
	// Effects may disappear when an earlier selected parent was deleted, but no
	// new side effect or dependency may be accepted without another review.
	effects := map[string]bool{}
	for _, effect := range reviewed.Effects {
		effects[effect] = true
	}
	for _, effect := range current.Effects {
		if !effects[effect] {
			return fmt.Errorf("Deletion effects changed for %s.", current.Resource.ID)
		}
	}
	deps := map[string]bool{}
	for _, dep := range reviewed.Dependencies {
		deps[awsCleanupKey(dep.Type, dep.ID)] = true
	}
	for _, dep := range current.Dependencies {
		if !deps[awsCleanupKey(dep.Type, dep.ID)] {
			return fmt.Errorf("Resource dependencies changed for %s.", current.Resource.ID)
		}
	}
	return nil
}
func (p *localControlPanel) setAWSCleanupResult(index int, status, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := &p.awsCleanupResults[index]
	result.Status, result.Message = status, message
	op := p.operationLocked(panelOperationAWSCleanup)
	now := time.Now()
	op.UpdatedAt = &now
	op.Output = appendBatchOutput(op.Output, fmt.Sprintf("[AWS cleanup] %s %s: %s — %s", result.Resource.Type, result.Resource.ID, status, message))
	p.persistOperationsLocked()
}
func (p *localControlPanel) finishAWSCleanup() {
	p.awsMu.Lock()
	p.awsCacheKey = ""
	p.awsMu.Unlock()
	p.mu.Lock()
	op := p.operationLocked(panelOperationAWSCleanup)
	now := time.Now()
	op.Running = false
	op.FinishedAt = &now
	op.UpdatedAt = &now
	failures := 0
	for _, result := range p.awsCleanupResults {
		if result.Status != "deleted" {
			failures++
		}
	}
	if failures > 0 {
		op.Error = fmt.Sprintf("%d resource(s) were not deleted. Review the results, refresh inventory, and retry if appropriate.", failures)
	}
	p.persistOperationsLocked()
	p.mu.Unlock()
}
func (p *localControlPanel) snapshotAWSCleanup() awsCleanupSnapshot {
	snapshot := p.snapshotOperation(panelOperationAWSCleanup)
	p.mu.Lock()
	defer p.mu.Unlock()
	return awsCleanupSnapshot{panelOperationSnapshot: snapshot, Results: append([]awsCleanupResult{}, p.awsCleanupResults...)}
}
