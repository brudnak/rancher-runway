package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (p *localControlPanel) handleRancherOperations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", 403)
		return
	}
	if r.Method == http.MethodGet {
		records := []rancherOperationRecord{}
		entries, err := os.ReadDir(p.rancherOperationsDir())
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "cannot read operation history", 500)
			return
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(p.rancherOperationsDir(), entry.Name()))
			if err != nil {
				continue
			}
			var record rancherOperationRecord
			if json.Unmarshal(data, &record) != nil || record.ClusterID != r.URL.Query().Get("clusterId") {
				continue
			}
			p.rancherOps.mu.Lock()
			active := p.rancherOps.active[record.ClusterID] == record.ID
			p.rancherOps.mu.Unlock()
			if record.Status == "running" && !active {
				record.Status = "interrupted"
				record.Events = append(record.Events, rancherOperationEvent{time.Now().UTC(), "Runway restarted; outcome is unknown. Inspect the live server before retrying."})
			}
			records = append(records, record)
		}
		sort.Slice(records, func(i, j int) bool { return records[i].Started.After(records[j].Started) })
		writeJSON(w, map[string]any{"records": records})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var in rancherOperationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&in); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	cluster, err := p.clusterByID(in.ClusterID)
	if err == nil && (cluster.Type == "downstream" || cluster.Role == "downstream" || cluster.RancherURL == "") {
		err = fmt.Errorf("select a deployed management Rancher")
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var result any
	switch in.Action {
	case "environment":
		_, credentialErr := deployedEnvironmentCredentials(in.Provider)
		message := "Runway credentials are available. Secret values remain on the backend."
		if credentialErr != nil {
			message = credentialErr.Error()
		}
		result = map[string]any{"available": credentialErr == nil, "message": message}
	case "image-details":
		result, err = deployedUpgradeImageDetails(ctx, in.Image, inspectRancherImageReference)
	case "image-sources":
		result = map[string]any{"sources": deployedImageSources}
	case "heads":
		result, err = deployedHeadTargets(ctx, in.Distribution, in.Registry, listRancherImageTags)
	case "inspect":
		result, err = inspectInstalledRancher(ctx, cluster.KubeconfigPath)
	case "plan":
		result, err = p.planRancherUpgrade(ctx, cluster, in)
	case "upgrade":
		result, err = p.startRancherUpgrade(ctx, cluster, in)
	case "options":
		var catalog deployedDownstreamCatalog
		catalog, err = deployedDownstreamOptions(ctx, cluster, in)
		if err == nil {
			p.downstreamCatalogDefaults(ctx, cluster, in, &catalog)
		}
		result = catalog
	case "provider-options":
		result, err = deployedLinodeChoices(ctx, cluster, in)
	case "driver-status":
		result, err = deployedDriverStatus(ctx, cluster, in)
	case "enable-driver":
		result, err = enableDeployedDriver(ctx, cluster, in)
	case "downstream":
		result, err = p.startDeployedDownstream(ctx, cluster, in)
	default:
		err = fmt.Errorf("unknown Rancher workflow action")
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, result)
}
