package test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/linodeinventory"
)

func (p *localControlPanel) handleLinodeInventory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action           string              `json:"action"`
		Token            string              `json:"token"`
		Resource         linodeinventory.Ref `json:"resource"`
		Review           string              `json:"review"`
		NameConfirmation string              `json:"nameConfirmation"`
		IDConfirmation   string              `json:"idConfirmation"`
	}
	if !decodeAWSCleanupRequest(w, r, p, &in) {
		return
	}
	token := in.Token
	if token == "" {
		token = linodeAccessToken()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var result any
	var err error
	switch in.Action {
	case "list":
		result, err = p.linodeInventory.Client.List(ctx, token)
	case "preview":
		result, err = p.linodeInventory.Preview(ctx, token, in.Resource)
	case "delete":
		// Use the same lifecycle exclusion as AWS cleanup to avoid racing Destroy.
		p.mu.Lock()
		running := false
		for _, op := range p.operations {
			if op != nil && op.Running {
				running = true
			}
		}
		p.rancherOps.mu.Lock()
		running = running || len(p.rancherOps.active) > 0
		if !running {
			if p.rancherOps.active == nil {
				p.rancherOps.active = map[string]string{}
			}
			p.rancherOps.active["linode-inventory"] = operationID()
		}
		p.rancherOps.mu.Unlock()
		p.mu.Unlock()
		if running {
			http.Error(w, "Wait for the active Runway operation before deleting a Linode resource.", 409)
			return
		}
		defer p.releaseRancherOperation("linode-inventory")
		var item linodeinventory.Resource
		item, err = p.linodeInventory.Delete(ctx, token, in.Review, in.NameConfirmation, in.IDConfirmation)
		if err == nil {
			result = map[string]any{"resource": item, "message": fmt.Sprintf("Linode accepted deletion of %s %d. Refresh to verify its removal.", item.Type, item.ID)}
		}
	default:
		err = fmt.Errorf("unknown Linode inventory action")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, result)
}
